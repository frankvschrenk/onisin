# oosfs

An MCP filesystem server tailored for LLM-driven work on the **onisin OS**
monorepo. It replaces `@modelcontextprotocol/server-filesystem` but is
designed for a trusted, single-user context: no arbitrary size caps, no
refusal to read "too large" files, no `[FILE]` text formatting.

Instead, oosfs focuses on the things that actually slow the LLM down:

- **Structured JSON output** everywhere — no hand-rolled text formats to parse.
- **`search`** — glob + regex content search in one call, respecting
  `.gitignore`. Replaces the `list` → `read` → `read` → `read` pattern.
- **`read` with line ranges** and **`read_many`** with per-file ranges.
- **Reliable `edit`** — atomic find/replace with uniqueness check and
  `dry_run` preview. Fixes the common "edit_file silently does nothing"
  pain.
- **`project_info`** — detects git root, Go module, Node / Python /
  Rust / Make projects at a glance.
- **`git_status`** and **`git_diff`** — see what the user sees.

## Install

```bash
cd /Users/frank/repro/onisin/oosfs
go build -o oosfs .
```

## Run

Stdio mode (for Claude Desktop):

```bash
./oosfs /Users/frank/repro/onisin
```

HTTP mode (for debugging with curl or for reuse through `oosb`):

```bash
./oosfs --http :8765 /Users/frank/repro/onisin
```

Pass multiple allowed roots by listing them:

```bash
./oosfs /Users/frank/repro/onisin /Users/frank/repro/xium
```

## Claude Desktop config

Edit `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "oosfs": {
      "command": "/Users/frank/repro/onisin/oosfs/oosfs",
      "args": ["/Users/frank/repro/onisin"]
    }
  }
}
```

Restart Claude Desktop after editing.

## Tools

| Tool            | Purpose                                              |
|-----------------|------------------------------------------------------|
| `list`          | Flat directory listing (JSON)                        |
| `tree`          | Recursive directory tree with depth control          |
| `read`          | Single-file read with line ranges                    |
| `read_many`     | Batch read with per-file ranges                      |
| `search`        | Glob + regex content search, honors `.gitignore`     |
| `write`         | Atomic write (temp-file + rename)                    |
| `append`        | Append to a file                                     |
| `edit`          | Find/replace with uniqueness check and dry-run       |
| `mkdir`         | Create directory with parents                        |
| `move`          | Rename / move                                        |
| `copy`          | Copy single file                                     |
| `remove`        | Delete file or (optionally recursive) directory      |
| `stat`          | File metadata                                        |
| `allowed_roots` | Which directories this server can access             |
| `project_info`  | Detect project structure (git / go / node / ...)    |
| `git_status`    | Parse `git status --porcelain=v1 -b`                 |
| `git_diff`      | Unstaged, staged, or revision diff                   |
| `git_commit`    | Stage, commit, optionally push — single-call workflow |
| `exec`          | Run a command in an allowed dir; returns exit/stdout/stderr/duration |
| `exec_start`    | Start a long-running command; returns a session ID   |
| `exec_read`     | Read incremental output from a streaming session     |
| `exec_stop`     | Terminate a streaming session (SIGTERM → SIGKILL)    |
| `which`         | Resolve an executable name against `$PATH`           |
| `git_push`      | Push committed history to a remote (no commit step)  |
| `apply_patch`   | Apply a unified diff (uses `git apply` under the hood) |
| `find_symbol`   | Go-AST search for symbol definitions by name or regex |
| `list_symbols`  | List all top-level symbols in a Go file or package   |
| `go_hover`      | gopls hover: type + godoc at a position              |
| `go_definition` | gopls go-to-definition                               |
| `go_references` | gopls find-all-references across the workspace       |
| `go_diagnostics`| gopls diagnostics for a file (errors, hints, unused) |
| `go_symbols`    | gopls document-symbol tree (type-checker view)       |
| `go_symbol_refs`| Find a Go symbol by name, return all usages via gopls |
| `go_package_diagnostics` | gopls diagnostics across a package directory |
| `go_workspace_diagnostics` | gopls diagnostics across an entire subtree |
| `pg_query`      | Run a row-returning SELECT against the demo database |
| `pg_exec`       | Run INSERT/UPDATE/DELETE/DDL against the demo database |
| `pg_reset`      | Drop and recreate the demo database from `demo.toml` |
| `browser_open`  | Navigate the shared headless browser to a URL        |
| `browser_click` | Click an element matching a CSS selector             |
| `browser_fill`  | Fill a form field identified by CSS selector         |
| `browser_text`  | Read visible text from an element or the page body   |
| `browser_wait`  | Wait until a selector becomes visible                |
| `browser_screenshot` | Capture a full-page PNG                         |
| `browser_close` | Close the browser session                            |

## Trusted mode

Set `OOSFS_TRUSTED=1` in the Claude Desktop config to advertise all tools
as read-only. This suppresses the per-call confirmation dialog. The
actual behaviour is unchanged; it's a UX hint to the client.

```json
{
  "mcpServers": {
    "oosfs": {
      "command": "/Users/frank/repro/onisin/oosfs/oosfs",
      "args": ["/Users/frank/repro/onisin"],
      "env": { "OOSFS_TRUSTED": "1" }
    }
  }
}
```

## Design notes

- **Trusted context.** Within allowed roots, no further gating applies.
  Frank granted access; oosfs acts.
- **Modular layout.** `internal/tools/*.go` — one tool family per file.
  New tool? New file + one line in `RegisterAll`.
- **`.gitignore` awareness** is on by default for `search` and for `tree`
  excludes (`.git`, `node_modules`, `dist`). Toggle off per call when
  needed.
- **Atomic writes** via temp-file + rename. No half-written files on
  crash.
- **Git integration** via the `git` binary, not a Go library. The CLI is
  always present on a dev machine and produces canonical output.

## License

Matches the onisin OS project — BSL 1.1.
Copyright © Frank von Schrenk.
