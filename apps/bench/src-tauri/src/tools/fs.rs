//! Filesystem handlers (bench.fs.*).
//
// Faithful port of the Bun tools/fs.ts. Every path argument is resolved
// through the allowed-root sandbox first. Handlers are synchronous std::fs and
// run on a blocking thread (see [`handle`]) so they never stall the async
// dispatcher loop. The returned JSON shapes match the Bun originals so callers
// (and bench-nats) see no difference.

use std::cmp::Ordering;
use std::io::Write;
use std::os::unix::fs::MetadataExt;
use std::path::Path;
use std::process::Command;
use std::time::SystemTime;

use serde::Deserialize;
use serde_json::{json, Map, Value};

use crate::ctx::Ctx;
use crate::error::ToolError;
use crate::roots::RootRegistry;

/// Async entry: fs work is blocking std::fs, so it runs on a blocking thread to
/// keep the dispatcher's async runtime free. Only the sandbox is needed.
pub async fn handle(op: &str, args: Value, ctx: &Ctx) -> Option<Result<Value, ToolError>> {
    let roots = ctx.roots.clone();
    let op = op.to_string();
    match tokio::task::spawn_blocking(move || dispatch(&op, args, &roots)).await {
        Ok(outcome) => outcome,
        Err(join) => Some(Err(ToolError::Msg(format!("fs task failed: {join}")))),
    }
}

fn dispatch(op: &str, args: Value, roots: &RootRegistry) -> Option<Result<Value, ToolError>> {
    let result = match op {
        "allowed_roots" => Ok(allowed_roots(roots)),
        "stat" => stat(args, roots),
        "list" => list(args, roots),
        "tree" => tree(args, roots),
        "read" => read(args, roots),
        "read_many" => read_many(args, roots),
        "write" => write(args, roots),
        "append" => append(args, roots),
        "edit" => edit(args, roots),
        "mkdir" => mkdir(args, roots),
        "move" => move_(args, roots),
        "copy" => copy(args, roots),
        "remove" => remove(args, roots),
        "project_info" => project_info(args, roots),
        _ => return None,
    };
    Some(result)
}

// ─── Arg shapes ──────────────────────────────────────────────────

#[derive(Deserialize)]
struct PathArg {
    path: String,
}

#[derive(Deserialize)]
struct ListArgs {
    path: String,
    #[serde(default)]
    hidden: bool,
}

#[derive(Deserialize)]
struct TreeArgs {
    path: String,
    depth: Option<i64>,
    #[serde(default)]
    hidden: bool,
    #[serde(default)]
    include_heavy: bool,
}

#[derive(Deserialize)]
struct ReadArgs {
    path: String,
    start: Option<usize>,
    end: Option<usize>,
    lines: Option<usize>,
    tail: Option<usize>,
}

#[derive(Deserialize)]
struct PathsArg {
    paths: Vec<String>,
}

#[derive(Deserialize)]
struct WriteArgs {
    path: String,
    content: String,
}

fn default_expect() -> i64 {
    1
}

#[derive(Deserialize)]
struct EditArgs {
    path: String,
    find: String,
    replace: String,
    #[serde(default = "default_expect")]
    expect_count: i64,
    #[serde(default)]
    dry_run: bool,
}

#[derive(Deserialize)]
struct MoveArgs {
    src: String,
    dst: String,
}

// ─── Handlers ────────────────────────────────────────────────────

fn allowed_roots(roots: &RootRegistry) -> Value {
    json!({ "roots": roots.all() })
}

fn stat(args: Value, roots: &RootRegistry) -> Result<Value, ToolError> {
    let a: PathArg = serde_json::from_value(args)?;
    let abs = roots.resolve(&a.path)?;
    // symlink_metadata so a symlink reports as "symlink" rather than its target.
    let meta = std::fs::symlink_metadata(&abs)?;
    let kind = if meta.is_dir() {
        "dir"
    } else if meta.file_type().is_symlink() {
        "symlink"
    } else {
        "file"
    };
    Ok(json!({
        "path": path_str(&abs),
        "kind": kind,
        "size": meta.len(),
        "mtime": iso(meta.modified().unwrap_or(SystemTime::UNIX_EPOCH)),
        "mode": octal(&meta),
    }))
}

fn list(args: Value, roots: &RootRegistry) -> Result<Value, ToolError> {
    let a: ListArgs = serde_json::from_value(args)?;
    let abs = roots.resolve(&a.path)?;
    let mut entries: Vec<Value> = Vec::new();
    for ent in std::fs::read_dir(&abs)? {
        let ent = ent?;
        let name = ent.file_name().to_string_lossy().into_owned();
        if !a.hidden && name.starts_with('.') {
            continue;
        }
        // metadata (not symlink_metadata) follows links, matching the Bun stat.
        let Ok(meta) = std::fs::metadata(ent.path()) else {
            continue;
        };
        entries.push(json!({
            "name": name,
            "type": if meta.is_dir() { "dir" } else { "file" },
            "size": meta.len(),
            "mtime": iso(meta.modified().unwrap_or(SystemTime::UNIX_EPOCH)),
            "mode": octal(&meta),
        }));
    }
    sort_dirs_first(&mut entries);
    Ok(json!({ "path": path_str(&abs), "count": entries.len(), "entries": entries }))
}

fn tree(args: Value, roots: &RootRegistry) -> Result<Value, ToolError> {
    let a: TreeArgs = serde_json::from_value(args)?;
    let abs = roots.resolve(&a.path)?;
    let depth = a.depth.unwrap_or(3);
    let root = walk(&abs, depth, a.hidden, a.include_heavy, &abs);
    Ok(json!({ "path": path_str(&abs), "root": root }))
}

// Heavy dirs are summarised (truncated:true) instead of descended into, unless
// include_heavy is set or the dir is the tree root itself.
const HEAVY: [&str; 5] = [".git", "node_modules", "dist", ".next", "build"];

fn walk(dir: &Path, depth: i64, hidden: bool, include_heavy: bool, root: &Path) -> Option<Value> {
    let meta = std::fs::metadata(dir).ok()?;
    let name = dir
        .file_name()
        .map(|n| n.to_string_lossy().into_owned())
        .unwrap_or_else(|| dir.to_string_lossy().into_owned());
    let mut node = json!({
        "name": name,
        "type": if meta.is_dir() { "dir" } else { "file" },
        "size": meta.len(),
        "mtime": iso(meta.modified().unwrap_or(SystemTime::UNIX_EPOCH)),
        "mode": octal(&meta),
    });

    if !meta.is_dir() || depth <= 0 {
        return Some(node);
    }
    if !include_heavy && HEAVY.contains(&name.as_str()) && dir != root {
        node["truncated"] = Value::Bool(true);
        return Some(node);
    }

    let mut children: Vec<Value> = Vec::new();
    if let Ok(rd) = std::fs::read_dir(dir) {
        for ent in rd.flatten() {
            let child_name = ent.file_name().to_string_lossy().into_owned();
            if !hidden && child_name.starts_with('.') {
                continue;
            }
            if let Some(child) = walk(&ent.path(), depth - 1, hidden, include_heavy, root) {
                children.push(child);
            }
        }
    }
    sort_dirs_first(&mut children);
    node["children"] = Value::Array(children);
    Some(node)
}

fn read(args: Value, roots: &RootRegistry) -> Result<Value, ToolError> {
    let a: ReadArgs = serde_json::from_value(args)?;
    let abs = roots.resolve(&a.path)?;
    let size = std::fs::metadata(&abs)?.len();
    let bytes = std::fs::read(&abs)?;

    // Text iff no NUL byte and valid UTF-8 — robust without a MIME database.
    let is_text = !bytes.contains(&0) && std::str::from_utf8(&bytes).is_ok();
    let mime = guess_mime(&abs, is_text);
    if !is_text {
        return Ok(json!({ "path": path_str(&abs), "kind": "binary", "mime": mime, "size": size }));
    }

    let text = String::from_utf8(bytes).expect("validated UTF-8 above");
    let all: Vec<&str> = text.split('\n').collect();
    let total = all.len();

    // Line window: start/end, or lines (head N), or tail (last N). 1-based.
    let mut s = a.start.unwrap_or(1).max(1);
    let mut e = a.end.unwrap_or(total);
    if let Some(l) = a.lines {
        s = 1;
        e = l.min(total);
    }
    if let Some(t) = a.tail {
        s = total.saturating_sub(t).saturating_add(1).max(1);
        e = total;
    }

    let lo = s.saturating_sub(1).min(total);
    let hi = e.min(total);
    let slice: &[&str] = if lo <= hi { &all[lo..hi] } else { &[] };

    const LIMIT: usize = 2000;
    let truncated = slice.len() > LIMIT;
    let shown = if truncated { &slice[..LIMIT] } else { slice };
    let content = shown.join("\n");
    let end_line = if shown.is_empty() {
        s.saturating_sub(1)
    } else {
        e.min(s + shown.len() - 1)
    };

    Ok(json!({
        "path": path_str(&abs),
        "kind": "text",
        "mime": mime,
        "size": size,
        "start_line": s,
        "end_line": end_line,
        "total_lines": total,
        "truncated": truncated,
        "content": content,
    }))
}

fn read_many(args: Value, roots: &RootRegistry) -> Result<Value, ToolError> {
    let a: PathsArg = serde_json::from_value(args)?;
    let results: Vec<Value> = a
        .paths
        .iter()
        .map(|p| match roots.resolve(p) {
            Ok(abs) => match std::fs::read_to_string(&abs) {
                Ok(content) => json!({ "path": path_str(&abs), "ok": true, "content": content }),
                Err(err) => json!({ "path": p, "ok": false, "error": err.to_string() }),
            },
            Err(err) => json!({ "path": p, "ok": false, "error": err.to_string() }),
        })
        .collect();
    Ok(json!({ "results": results }))
}

fn write(args: Value, roots: &RootRegistry) -> Result<Value, ToolError> {
    let a: WriteArgs = serde_json::from_value(args)?;
    let abs = roots.resolve(&a.path)?;
    if let Some(parent) = abs.parent() {
        std::fs::create_dir_all(parent)?;
    }
    std::fs::write(&abs, &a.content)?;
    let bytes = std::fs::metadata(&abs)?.len();
    Ok(json!({ "path": path_str(&abs), "bytes": bytes, "status": "ok" }))
}

fn append(args: Value, roots: &RootRegistry) -> Result<Value, ToolError> {
    let a: WriteArgs = serde_json::from_value(args)?;
    let abs = roots.resolve(&a.path)?;
    if let Some(parent) = abs.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let mut file = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(&abs)?;
    file.write_all(a.content.as_bytes())?;
    Ok(json!({ "path": path_str(&abs), "status": "ok" }))
}

fn edit(args: Value, roots: &RootRegistry) -> Result<Value, ToolError> {
    let a: EditArgs = serde_json::from_value(args)?;
    let abs = roots.resolve(&a.path)?;
    // Guard the empty-needle case the Bun version would have spun on forever.
    if a.find.is_empty() {
        return Err(ToolError::Msg("edit: find string must not be empty".into()));
    }
    let orig = std::fs::read_to_string(&abs)?;
    let count = orig.matches(&a.find).count();
    if a.expect_count != -1 && count as i64 != a.expect_count {
        return Err(ToolError::Msg(format!(
            "edit: expected {} occurrence(s), got {} in {}",
            a.expect_count, count, a.path
        )));
    }
    if count == 0 {
        return Err(ToolError::Msg(format!("edit: find string not found in {}", a.path)));
    }
    if !a.dry_run {
        std::fs::write(&abs, orig.replace(&a.find, &a.replace))?;
    }
    Ok(json!({
        "path": path_str(&abs),
        "status": "ok",
        "replacements": count,
        "dry_run": a.dry_run,
    }))
}

fn mkdir(args: Value, roots: &RootRegistry) -> Result<Value, ToolError> {
    let a: PathArg = serde_json::from_value(args)?;
    let abs = roots.resolve(&a.path)?;
    std::fs::create_dir_all(&abs)?;
    Ok(json!({ "path": path_str(&abs), "status": "ok" }))
}

fn move_(args: Value, roots: &RootRegistry) -> Result<Value, ToolError> {
    let a: MoveArgs = serde_json::from_value(args)?;
    let src = roots.resolve(&a.src)?;
    let dst = roots.resolve(&a.dst)?;
    // rename is same-filesystem; the repo lives on one volume so that holds.
    std::fs::rename(&src, &dst)?;
    Ok(json!({ "src": path_str(&src), "dst": path_str(&dst), "status": "ok" }))
}

fn copy(args: Value, roots: &RootRegistry) -> Result<Value, ToolError> {
    let a: MoveArgs = serde_json::from_value(args)?;
    let src = roots.resolve(&a.src)?;
    let dst = roots.resolve(&a.dst)?;
    std::fs::copy(&src, &dst)?;
    Ok(json!({ "src": path_str(&src), "dst": path_str(&dst), "status": "ok" }))
}

fn remove(args: Value, roots: &RootRegistry) -> Result<Value, ToolError> {
    let a: PathArg = serde_json::from_value(args)?;
    let abs = roots.resolve(&a.path)?;
    // Mirror `rm -rf`: dirs recursively, files directly, missing is a no-op.
    match std::fs::symlink_metadata(&abs) {
        Ok(meta) if meta.is_dir() => std::fs::remove_dir_all(&abs)?,
        Ok(_) => std::fs::remove_file(&abs)?,
        Err(ref err) if err.kind() == std::io::ErrorKind::NotFound => {}
        Err(err) => return Err(err.into()),
    }
    Ok(json!({ "path": path_str(&abs), "status": "ok" }))
}

fn project_info(args: Value, roots: &RootRegistry) -> Result<Value, ToolError> {
    let a: PathArg = serde_json::from_value(args)?;
    let abs = roots.resolve(&a.path)?;
    let abs_str = path_str(&abs);
    let mut info = Map::new();
    info.insert("path".into(), json!(abs_str));

    if let Ok(out) = Command::new("git")
        .args(["-C", &abs_str, "rev-parse", "--show-toplevel"])
        .output()
    {
        if out.status.success() {
            info.insert(
                "git_root".into(),
                json!(String::from_utf8_lossy(&out.stdout).trim()),
            );
            if let Ok(branch) = Command::new("git")
                .args(["-C", &abs_str, "branch", "--show-current"])
                .output()
            {
                info.insert(
                    "git_branch".into(),
                    json!(String::from_utf8_lossy(&branch.stdout).trim()),
                );
            }
        }
    }

    let markers = [
        "package.json",
        "bun.lockb",
        "tsconfig.json",
        "go.mod",
        "Cargo.toml",
        "pyproject.toml",
        "Makefile",
        "Dockerfile",
        ".env",
    ];
    let mut found: Vec<&str> = Vec::new();
    for m in markers {
        if abs.join(m).exists() {
            found.push(m);
        }
    }
    let has_pkg = found.contains(&"package.json");
    info.insert("files".into(), json!(found));

    if has_pkg {
        if let Ok(txt) = std::fs::read_to_string(abs.join("package.json")) {
            if let Ok(pkg) = serde_json::from_str::<Value>(&txt) {
                if let Some(name) = pkg.get("name") {
                    info.insert("package_name".into(), name.clone());
                }
                if let Some(version) = pkg.get("version") {
                    info.insert("package_version".into(), version.clone());
                }
            }
        }
    }

    Ok(Value::Object(info))
}

// ─── Helpers ─────────────────────────────────────────────────────

fn path_str(p: &Path) -> String {
    p.to_string_lossy().into_owned()
}

// RFC-3339 with milliseconds and Z, byte-identical to new Date().toISOString().
fn iso(t: SystemTime) -> String {
    chrono::DateTime::<chrono::Utc>::from(t).to_rfc3339_opts(chrono::SecondsFormat::Millis, true)
}

fn octal(meta: &std::fs::Metadata) -> String {
    format!("{:o}", meta.mode())
}

// Directories before files, then by name — reading the "type"/"name" fields of
// the already-built JSON entries (shared by list and tree).
fn sort_dirs_first(entries: &mut [Value]) {
    entries.sort_by(|a, b| {
        let (at, bt) = (
            a["type"].as_str().unwrap_or(""),
            b["type"].as_str().unwrap_or(""),
        );
        if at != bt {
            return if at == "dir" {
                Ordering::Less
            } else {
                Ordering::Greater
            };
        }
        a["name"].as_str().unwrap_or("").cmp(b["name"].as_str().unwrap_or(""))
    });
}

fn guess_mime(path: &Path, is_text: bool) -> &'static str {
    match path.extension().and_then(|e| e.to_str()).unwrap_or("") {
        "json" => "application/json",
        "js" | "mjs" | "cjs" => "text/javascript",
        "ts" | "tsx" => "text/typescript",
        "html" | "htm" => "text/html",
        "css" => "text/css",
        "md" => "text/markdown",
        "xml" => "application/xml",
        "txt" | "log" => "text/plain",
        "png" => "image/png",
        "jpg" | "jpeg" => "image/jpeg",
        _ => {
            if is_text {
                "text/plain"
            } else {
                "application/octet-stream"
            }
        }
    }
}
