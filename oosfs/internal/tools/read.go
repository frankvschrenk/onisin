// Tool: read / read_many
//
// read returns the content of a single file with optional line or byte
// ranges, smart handling for binary/image files, and auto-truncation with a
// clear continuation hint for large files.
//
// read_many reads several files in one call, each with its own optional
// range. This collapses the N-round-trip "read-a-bunch-of-files" pattern
// into one request.

package tools

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// defaultLineBudget caps the number of lines returned when no explicit
// range is given. The value is a pragmatic LLM-friendly size, not a
// security limit — callers that want more can pass lines=0.
const defaultLineBudget = 2000

// binarySnippetBytes is the number of leading bytes returned as a hex
// preview for binary files.
const binarySnippetBytes = 512

// fileResult is the per-file payload returned by both read and read_many.
type fileResult struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"` // "text" | "binary" | "image" | "error"
	MIME       string `json:"mime,omitempty"`
	Size       int64  `json:"size,omitempty"`
	Content    string `json:"content,omitempty"`
	Base64     string `json:"base64,omitempty"`
	HexPreview string `json:"hex_preview,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	EndLine    int    `json:"end_line,omitempty"`
	TotalLines int    `json:"total_lines,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	Error      string `json:"error,omitempty"`
}

func registerRead(s *server.MCPServer, ctx *handlerCtx) {
	readTool := mcp.NewTool("read",
		mcp.WithDescription(
			"Read a single file. Handles text, binary, and image files. "+
				"For text files, use 'start'/'end' (1-based, inclusive) to select a line range, "+
				"or 'lines' as a shorthand for 'first N lines'. Without a range, reads up to "+
				"the first 2000 lines with a truncation hint.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Read file")),
		mcp.WithString("path", mcp.Required(), mcp.Description("File to read")),
		mcp.WithNumber("start", mcp.Description("First line to return (1-based, inclusive)")),
		mcp.WithNumber("end", mcp.Description("Last line to return (1-based, inclusive)")),
		mcp.WithNumber("lines", mcp.Description("Shorthand: return only the first N lines")),
		mcp.WithNumber("tail", mcp.Description("Shorthand: return only the last N lines")),
	)
	s.AddTool(readTool, ctx.handleRead)

	readManyTool := mcp.NewTool("read_many",
		mcp.WithDescription(
			"Read several files in one call. Each entry may supply its own line range. "+
				"Returns a list of results in the same order as the request; individual "+
				"failures do not abort the batch.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Read several files")),
		mcp.WithArray("requests", mcp.Required(),
			mcp.Description("Array of read requests: [{path, start?, end?, lines?, tail?}, ...]"),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":  map[string]any{"type": "string"},
					"start": map[string]any{"type": "number"},
					"end":   map[string]any{"type": "number"},
					"lines": map[string]any{"type": "number"},
					"tail":  map[string]any{"type": "number"},
				},
				"required": []string{"path"},
			}),
		),
	)
	s.AddTool(readManyTool, ctx.handleReadMany)
}

func (c *handlerCtx) handleRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("read", err), nil
	}
	result := c.readOne(path, rangeSpec{
		start: optionalInt(req, "start", 0),
		end:   optionalInt(req, "end", 0),
		lines: optionalInt(req, "lines", 0),
		tail:  optionalInt(req, "tail", 0),
	})
	return jsonResult(result), nil
}

func (c *handlerCtx) handleReadMany(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	rawRequests, ok := args["requests"].([]any)
	if !ok {
		return c.errResult("read_many", fmt.Errorf("requests must be an array")), nil
	}

	results := make([]fileResult, 0, len(rawRequests))
	for i, rawItem := range rawRequests {
		item, ok := rawItem.(map[string]any)
		if !ok {
			results = append(results, fileResult{
				Kind:  "error",
				Error: fmt.Sprintf("request #%d is not an object", i),
			})
			continue
		}
		path, _ := item["path"].(string)
		if path == "" {
			results = append(results, fileResult{
				Kind:  "error",
				Error: fmt.Sprintf("request #%d missing 'path'", i),
			})
			continue
		}
		results = append(results, c.readOne(path, rangeSpec{
			start: coerceInt(item["start"]),
			end:   coerceInt(item["end"]),
			lines: coerceInt(item["lines"]),
			tail:  coerceInt(item["tail"]),
		}))
	}
	return jsonResult(map[string]any{
		"count":   len(results),
		"results": results,
	}), nil
}

// rangeSpec consolidates the four optional range arguments a reader may use.
type rangeSpec struct {
	start int // 1-based inclusive; 0 means "from the beginning"
	end   int // 1-based inclusive; 0 means "to the end"
	lines int // shorthand: first N lines
	tail  int // shorthand: last N lines
}

// readOne is the shared implementation for read and each read_many entry.
func (c *handlerCtx) readOne(path string, spec rangeSpec) fileResult {
	abs, err := c.reg.Resolve(path)
	if err != nil {
		return fileResult{Path: path, Kind: "error", Error: err.Error()}
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fileResult{Path: abs, Kind: "error", Error: err.Error()}
	}
	if info.IsDir() {
		return fileResult{Path: abs, Kind: "error", Error: "path is a directory, use list or tree"}
	}

	kind, mime, err := detectKind(abs)
	if err != nil {
		return fileResult{Path: abs, Kind: "error", Error: err.Error()}
	}

	switch kind {
	case "image":
		return readAsBase64(abs, info.Size(), mime, "image")
	case "binary":
		return readBinaryPreview(abs, info.Size(), mime)
	default:
		return readText(abs, info.Size(), mime, spec)
	}
}

// detectKind classifies a file as text, binary, or image based on a MIME
// sniff of its first bytes.
func detectKind(path string) (kind, mime string, err error) {
	m, err := mimetype.DetectFile(path)
	if err != nil {
		return "", "", err
	}
	mime = m.String()
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image", mime, nil
	case strings.HasPrefix(mime, "text/"),
		strings.Contains(mime, "json"),
		strings.Contains(mime, "xml"),
		strings.Contains(mime, "javascript"),
		strings.Contains(mime, "yaml"),
		strings.Contains(mime, "toml"):
		return "text", mime, nil
	}
	// mimetype returns application/octet-stream for unknowns; treat as binary.
	return "binary", mime, nil
}

// readText loads text content, honoring any range specification.
func readText(path string, size int64, mime string, spec rangeSpec) fileResult {
	f, err := os.Open(path)
	if err != nil {
		return fileResult{Path: path, Kind: "error", Error: err.Error()}
	}
	defer f.Close()

	// Two-pass for "tail": we scan once to find the line count, then skip
	// ahead. For forward-only cases we stream in a single pass.
	if spec.tail > 0 {
		return readTextTail(f, path, size, mime, spec.tail)
	}

	start, end := resolveRange(spec, defaultLineBudget)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024) // allow lines up to 16 MiB
	var (
		sb         strings.Builder
		lineNo     int
		emitted    int
		haveMore   bool
		totalLines int
	)
	for scanner.Scan() {
		lineNo++
		totalLines++
		if lineNo < start {
			continue
		}
		if end > 0 && lineNo > end {
			haveMore = true
			// Still keep counting lines for the 'total_lines' field.
			for scanner.Scan() {
				totalLines++
			}
			break
		}
		if emitted > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(scanner.Text())
		emitted++
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return fileResult{Path: path, Kind: "error", Error: err.Error()}
	}

	actualEnd := lineNo
	if end > 0 && end < actualEnd {
		actualEnd = end
	}
	return fileResult{
		Path:       path,
		Kind:       "text",
		MIME:       mime,
		Size:       size,
		Content:    sb.String(),
		StartLine:  clampLow(start, 1),
		EndLine:    actualEnd,
		TotalLines: totalLines,
		Truncated:  haveMore,
	}
}

// readTextTail returns the last N lines by doing two passes: count lines,
// then skip ahead and read.
func readTextTail(f *os.File, path string, size int64, mime string, tail int) fileResult {
	// First pass: count lines.
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	total := 0
	for scanner.Scan() {
		total++
	}
	if err := scanner.Err(); err != nil {
		return fileResult{Path: path, Kind: "error", Error: err.Error()}
	}

	start := total - tail + 1
	if start < 1 {
		start = 1
	}
	// Second pass: rewind and emit lines from start onward.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fileResult{Path: path, Kind: "error", Error: err.Error()}
	}
	scanner = bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var sb strings.Builder
	lineNo := 0
	emitted := 0
	for scanner.Scan() {
		lineNo++
		if lineNo < start {
			continue
		}
		if emitted > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(scanner.Text())
		emitted++
	}

	return fileResult{
		Path:       path,
		Kind:       "text",
		MIME:       mime,
		Size:       size,
		Content:    sb.String(),
		StartLine:  start,
		EndLine:    total,
		TotalLines: total,
	}
}

// readBinaryPreview returns a hex snippet so the caller still has something
// useful to look at without exploding the context with gibberish.
func readBinaryPreview(path string, size int64, mime string) fileResult {
	f, err := os.Open(path)
	if err != nil {
		return fileResult{Path: path, Kind: "error", Error: err.Error()}
	}
	defer f.Close()

	buf := make([]byte, binarySnippetBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return fileResult{Path: path, Kind: "error", Error: err.Error()}
	}
	return fileResult{
		Path:       path,
		Kind:       "binary",
		MIME:       mime,
		Size:       size,
		HexPreview: hexDump(buf[:n]),
		Truncated:  size > int64(n),
	}
}

// readAsBase64 reads the whole file and encodes it, which is what MCP clients
// expect for images.
func readAsBase64(path string, size int64, mime, kind string) fileResult {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileResult{Path: path, Kind: "error", Error: err.Error()}
	}
	return fileResult{
		Path:   path,
		Kind:   kind,
		MIME:   mime,
		Size:   size,
		Base64: base64.StdEncoding.EncodeToString(data),
	}
}

// resolveRange turns a rangeSpec into effective [start, end] line numbers.
// An end of 0 means "open ended" or "use default budget".
func resolveRange(spec rangeSpec, defaultBudget int) (start, end int) {
	start = spec.start
	end = spec.end
	if start < 1 {
		start = 1
	}
	switch {
	case spec.lines > 0:
		end = start + spec.lines - 1
	case end == 0 && defaultBudget > 0:
		end = start + defaultBudget - 1
	}
	return start, end
}

func clampLow(v, min int) int {
	if v < min {
		return min
	}
	return v
}

// coerceInt handles the map[string]any values we get from mcp-go, where
// JSON numbers arrive as float64.
func coerceInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

// hexDump produces a compact hex+ASCII view, 16 bytes per line.
func hexDump(b []byte) string {
	var sb strings.Builder
	for i := 0; i < len(b); i += 16 {
		end := i + 16
		if end > len(b) {
			end = len(b)
		}
		fmt.Fprintf(&sb, "%08x  ", i)
		for j := i; j < i+16; j++ {
			if j < end {
				fmt.Fprintf(&sb, "%02x ", b[j])
			} else {
				sb.WriteString("   ")
			}
		}
		sb.WriteString(" |")
		for j := i; j < end; j++ {
			c := b[j]
			if c >= 0x20 && c < 0x7f {
				sb.WriteByte(c)
			} else {
				sb.WriteByte('.')
			}
		}
		sb.WriteString("|\n")
	}
	return sb.String()
}

// ensurePathExt is a small helper used by error messages when a caller
// passes a directory to read(). Kept here so the read code stays self-
// contained and we don't pull in a separate "pretty path" package.
func ensurePathExt(p string) string {
	if ext := filepath.Ext(p); ext != "" {
		return ext
	}
	return "(no extension)"
}
