// Tool: search (glob + grep)
//
// search combines the "find this file by name" and "find this text in some
// files" use cases that normally take 4+ separate tool calls into one
// round trip. It is the single most impactful tool for reducing LLM
// latency when navigating an unfamiliar codebase.
//
// Two modes:
//   - name-only: pass 'glob' without 'pattern' → returns matching paths.
//   - content:   pass 'pattern' (regex) → returns matching lines with
//                surrounding context, optionally filtered by 'glob'.
//
// By default the search honors .gitignore via a walking GitignoreStack. Pass
// no_ignore=true to disable that (useful for sniffing around inside
// vendored or generated code).

package tools

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	gitignore "github.com/sabhiram/go-gitignore"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// searchHit describes one matched line within a file.
type searchHit struct {
	Line    int      `json:"line"`
	Text    string   `json:"text"`
	Context []string `json:"context,omitempty"` // surrounding lines when requested
}

// fileSearchResult groups hits for a single file plus top-level metadata.
type fileSearchResult struct {
	Path  string      `json:"path"`
	Hits  []searchHit `json:"hits,omitempty"`
	Error string      `json:"error,omitempty"`
}

func registerSearch(s *server.MCPServer, ctx *handlerCtx) {
	tool := mcp.NewTool("search",
		mcp.WithDescription(
			"Search files by name (glob) and/or by content (regex) in one call. "+
				"Glob uses doublestar syntax (** = any number of directories). "+
				"Pattern is a Go RE2 regex. Respects .gitignore unless no_ignore=true. "+
				"Skips .git/node_modules/dist by default.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Search files")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Root directory to search from")),
		mcp.WithString("glob", mcp.Description("Glob for filenames (e.g. '**/*.go' or 'oosp/**/*.go')")),
		mcp.WithString("pattern", mcp.Description("Regex to search inside file contents (Go RE2 syntax)")),
		mcp.WithBoolean("case_insensitive", mcp.Description("Match pattern case-insensitively (default: false)")),
		mcp.WithNumber("context", mcp.Description("Include N lines of context around each match (default: 0)")),
		mcp.WithNumber("max_hits_per_file", mcp.Description("Cap hits per file (default: 100, 0 = unlimited)")),
		mcp.WithNumber("max_files", mcp.Description("Cap number of files returned (default: 500, 0 = unlimited)")),
		mcp.WithBoolean("no_ignore", mcp.Description("Disregard .gitignore (default: false)")),
		mcp.WithBoolean("include_heavy", mcp.Description("Also descend into .git/node_modules/dist (default: false)")),
		mcp.WithBoolean("hidden", mcp.Description("Include dotfiles (default: false)")),
	)
	s.AddTool(tool, ctx.handleSearch)
}

func (c *handlerCtx) handleSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rootArg, err := req.RequireString("path")
	if err != nil {
		return c.errResult("search", err), nil
	}
	glob := optionalString(req, "glob", "")
	pattern := optionalString(req, "pattern", "")
	caseInsens := optionalBool(req, "case_insensitive", false)
	contextN := optionalInt(req, "context", 0)
	maxHitsPerFile := optionalInt(req, "max_hits_per_file", 100)
	maxFiles := optionalInt(req, "max_files", 500)
	noIgnore := optionalBool(req, "no_ignore", false)
	includeHeavy := optionalBool(req, "include_heavy", false)
	hidden := optionalBool(req, "hidden", false)

	if glob == "" && pattern == "" {
		return c.errResult("search", fmt.Errorf("at least one of 'glob' or 'pattern' must be set")), nil
	}

	root, err := c.reg.Resolve(rootArg)
	if err != nil {
		return c.errResult("search", err), nil
	}

	var re *regexp.Regexp
	if pattern != "" {
		compiled, err := compileRegex(pattern, caseInsens)
		if err != nil {
			return c.errResult("search", err), nil
		}
		re = compiled
	}

	gs := newGitignoreStack(root, noIgnore)

	results := make([]fileSearchResult, 0, 64)
	filesSeen := 0

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Permission errors on individual entries shouldn't kill the walk.
			return nil
		}
		name := d.Name()

		// Skip hidden entries unless asked.
		if !hidden && name != "." && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// Skip heavy directories unless asked.
		if d.IsDir() && !includeHeavy && heavyDirs[name] {
			return fs.SkipDir
		}
		// Respect .gitignore.
		if !noIgnore && gs.Ignored(path, d.IsDir()) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// Apply glob filter, if any.
		if glob != "" {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			matched, _ := doublestar.PathMatch(glob, filepath.ToSlash(rel))
			if !matched {
				return nil
			}
		}
		// Name-only search.
		if re == nil {
			results = append(results, fileSearchResult{Path: path})
			filesSeen++
			if maxFiles > 0 && filesSeen >= maxFiles {
				return filepath.SkipAll
			}
			return nil
		}
		// Content search.
		hits, err := grepFile(path, re, contextN, maxHitsPerFile)
		if err != nil {
			results = append(results, fileSearchResult{Path: path, Error: err.Error()})
			return nil
		}
		if len(hits) > 0 {
			results = append(results, fileSearchResult{Path: path, Hits: hits})
			filesSeen++
			if maxFiles > 0 && filesSeen >= maxFiles {
				return filepath.SkipAll
			}
		}
		return nil
	})

	if walkErr != nil && walkErr != filepath.SkipAll {
		return c.errResult("search", walkErr), nil
	}

	return jsonResult(map[string]any{
		"root":    root,
		"glob":    glob,
		"pattern": pattern,
		"files":   len(results),
		"results": results,
	}), nil
}

// compileRegex builds the effective regex, prepending (?i) for
// case-insensitive matching.
func compileRegex(pattern string, caseInsens bool) (*regexp.Regexp, error) {
	if caseInsens {
		pattern = "(?i)" + pattern
	}
	return regexp.Compile(pattern)
}

// grepFile scans the file line-by-line and records matches.
// Context lines are included only when contextN > 0.
func grepFile(path string, re *regexp.Regexp, contextN, maxHits int) ([]searchHit, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Quick binary screen: peek at a small buffer. Skipping binaries here
	// mirrors ripgrep's default behavior and keeps result noise down.
	if isBinary(f) {
		return nil, nil
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	// Keep a ring of recent lines for "before" context.
	var (
		ring    = make([]string, contextN)
		hits    []searchHit
		lineNo  int
		tailNeed int // lines still owed as "after" context to the previous hit
		tailHit  *searchHit
	)
	for scanner.Scan() {
		lineNo++
		text := scanner.Text()

		// Append to the outstanding "after" window first.
		if tailNeed > 0 && tailHit != nil {
			tailHit.Context = append(tailHit.Context, fmt.Sprintf("%d  %s", lineNo, text))
			tailNeed--
			if tailNeed == 0 {
				tailHit = nil
			}
		}

		if re.MatchString(text) {
			hit := searchHit{Line: lineNo, Text: text}
			if contextN > 0 {
				// "Before" window from ring buffer.
				start := lineNo - contextN
				if start < 1 {
					start = 1
				}
				for i := start; i < lineNo; i++ {
					// The ring may not yet hold contextN items early in the file.
					idx := (i - 1) % contextN
					if ring[idx] != "" {
						hit.Context = append(hit.Context, fmt.Sprintf("%d  %s", i, ring[idx]))
					}
				}
			}
			hits = append(hits, hit)
			if maxHits > 0 && len(hits) >= maxHits {
				break
			}
			if contextN > 0 {
				tailHit = &hits[len(hits)-1]
				tailNeed = contextN
			}
		}

		if contextN > 0 {
			ring[(lineNo-1)%contextN] = text
		}
	}
	if err := scanner.Err(); err != nil {
		return hits, err
	}
	return hits, nil
}

// isBinary peeks at the first 512 bytes and rewinds. A NUL byte is a strong
// indicator that we are looking at a non-text file.
func isBinary(f *os.File) bool {
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	defer f.Seek(0, 0) //nolint:errcheck
	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true
		}
	}
	return false
}

// gitignoreStack accumulates .gitignore files as we descend into
// directories. Each entry applies to paths below its directory, the way
// Git itself evaluates them.
type gitignoreStack struct {
	disabled bool
	root     string
	// byDir caches the compiled matcher per directory so we don't reparse
	// the same .gitignore on every file in a big folder.
	byDir map[string]*gitignore.GitIgnore
}

func newGitignoreStack(root string, disabled bool) *gitignoreStack {
	return &gitignoreStack{
		disabled: disabled,
		root:     root,
		byDir:    map[string]*gitignore.GitIgnore{},
	}
}

// Ignored reports whether a path is ignored by any .gitignore in its
// ancestor chain within the search root.
func (g *gitignoreStack) Ignored(path string, isDir bool) bool {
	if g.disabled {
		return false
	}
	// Walk up from path's directory to root, applying each .gitignore.
	dir := path
	if !isDir {
		dir = filepath.Dir(path)
	}
	for {
		if !strings.HasPrefix(dir, g.root) {
			break
		}
		gi := g.load(dir)
		if gi != nil {
			rel, err := filepath.Rel(dir, path)
			if err == nil && gi.MatchesPath(rel) {
				return true
			}
		}
		if dir == g.root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return false
}

// load lazily reads and compiles the .gitignore in dir, returning nil if
// none exists.
func (g *gitignoreStack) load(dir string) *gitignore.GitIgnore {
	if gi, ok := g.byDir[dir]; ok {
		return gi
	}
	p := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(p); err != nil {
		g.byDir[dir] = nil
		return nil
	}
	gi, err := gitignore.CompileIgnoreFile(p)
	if err != nil {
		g.byDir[dir] = nil
		return nil
	}
	g.byDir[dir] = gi
	return gi
}
