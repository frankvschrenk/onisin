// Package roots manages the set of allowed root directories.
//
// oosfs is built for a trusted, single-user context but still scopes access
// to explicitly listed roots. This avoids accidentally wandering into /etc
// or $HOME/.ssh because the LLM confused two similar paths.
//
// Within the allowed roots no further restrictions apply: read, write, move,
// delete are all permitted.
package roots

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Registry holds the canonicalized set of allowed root directories.
type Registry struct {
	roots  []string
	logger *slog.Logger
}

// New resolves each user-supplied path to its absolute, symlink-free form
// and returns a ready-to-use Registry. Tilde expansion is applied so that
// "~/repro" works out of the box.
func New(paths []string, logger *slog.Logger) (*Registry, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no allowed directories provided")
	}

	resolved := make([]string, 0, len(paths))
	seen := make(map[string]bool)

	for _, raw := range paths {
		abs, err := canonicalize(raw)
		if err != nil {
			return nil, fmt.Errorf("resolve %q: %w", raw, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("stat %q: %w", abs, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%q is not a directory", abs)
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		resolved = append(resolved, abs)
	}

	return &Registry{roots: resolved, logger: logger}, nil
}

// All returns a copy of all allowed roots. Handy for the list_allowed_roots
// tool and for diagnostics.
func (r *Registry) All() []string {
	out := make([]string, len(r.roots))
	copy(out, r.roots)
	return out
}

// Resolve turns an incoming path (relative, absolute, or with ~) into an
// absolute, symlink-free path and verifies it sits inside an allowed root.
//
// Resolve accepts paths that do not yet exist (useful for write operations),
// but requires the parent directory to exist and to live inside an allowed
// root. This prevents creating a file that would technically be reachable
// only via a symlink outside the sandbox.
func (r *Registry) Resolve(path string) (string, error) {
	abs, err := canonicalize(path)
	if err != nil {
		// Path may not exist yet — fall back to a lexical absolute form and
		// verify via the parent directory.
		expanded, expErr := expandHome(path)
		if expErr != nil {
			return "", expErr
		}
		if !filepath.IsAbs(expanded) {
			cwd, cwdErr := os.Getwd()
			if cwdErr != nil {
				return "", cwdErr
			}
			expanded = filepath.Join(cwd, expanded)
		}
		expanded = filepath.Clean(expanded)

		parent := filepath.Dir(expanded)
		parentAbs, parentErr := canonicalize(parent)
		if parentErr != nil {
			return "", fmt.Errorf("resolve %q: %w", path, err)
		}
		if err := r.checkContained(parentAbs); err != nil {
			return "", err
		}
		return filepath.Join(parentAbs, filepath.Base(expanded)), nil
	}

	if err := r.checkContained(abs); err != nil {
		return "", err
	}
	return abs, nil
}

// checkContained verifies that abs lies within any of the allowed roots.
func (r *Registry) checkContained(abs string) error {
	for _, root := range r.roots {
		if abs == root || strings.HasPrefix(abs, root+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("path %q is outside allowed roots", abs)
}

// canonicalize expands ~, makes the path absolute, and resolves symlinks.
func canonicalize(path string) (string, error) {
	expanded, err := expandHome(path)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	// EvalSymlinks also cleans the path.
	return filepath.EvalSymlinks(abs)
}

// expandHome replaces a leading "~" or "~/" with the current user's home
// directory. Other uses of ~ (e.g. "~user") are left untouched — keep it
// simple, Go's os.UserHomeDir covers the common case.
func expandHome(path string) (string, error) {
	if path == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
