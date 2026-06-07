//! Profile resolution and config parsing.
//!
//! All profiles live in a single oosrun.yaml at the repo root, under a
//! top-level `profiles` map. A profile lists its `procs` and/or `include`s
//! other profiles, so composite profiles (e.g. `all`) reuse the parts without
//! duplication. Each proc carries a `name` plus either `shell` or `cmd`, and
//! optional `cwd`, `env` and `autostart`.

use std::collections::HashSet;
use std::path::{Path, PathBuf};

use anyhow::{anyhow, bail, Context, Result};

/// File name that both marks the repo root and holds every profile.
const CONFIG_FILE: &str = "oosrun.yaml";

/// A single process to supervise, with its command already resolved to an argv
/// so proc.rs never has to care about the shell-vs-cmd distinction.
#[derive(Debug, Clone)]
pub struct ProcSpec {
    pub name: String,
    /// Program plus args. A `shell:` entry becomes ["/bin/sh", "-c", <string>];
    /// a `cmd:` array is taken verbatim.
    pub argv: Vec<String>,
    /// Working directory; falls back to the repo root when absent.
    pub cwd: Option<PathBuf>,
    /// Env overrides applied on top of the inherited environment.
    pub env: Vec<(String, String)>,
    /// Start the proc on launch. Defaults to true.
    pub autostart: bool,
}

/// Walks up from `start` until a directory holding oosrun.yaml is found, so
/// oosrun runs from any subdirectory (and when installed on PATH) without a
/// baked-in absolute path.
fn find_repo_root(start: &Path) -> Option<PathBuf> {
    start
        .ancestors()
        .find(|dir| dir.join(CONFIG_FILE).is_file())
        .map(Path::to_path_buf)
}

/// Locates the repo root, reads oosrun.yaml and resolves the requested profile.
/// Returns the repo root (the default cwd for procs) and the resolved procs.
pub fn load(profile: &str) -> Result<(PathBuf, Vec<ProcSpec>)> {
    let cwd = std::env::current_dir().context("cannot read current directory")?;
    let root = find_repo_root(&cwd)
        .ok_or_else(|| anyhow!("no {CONFIG_FILE} found in {} or any parent", cwd.display()))?;

    let path = root.join(CONFIG_FILE);
    let text =
        std::fs::read_to_string(&path).with_context(|| format!("reading {}", path.display()))?;
    let specs =
        parse(&text, &root, profile).with_context(|| format!("parsing {}", path.display()))?;
    if specs.is_empty() {
        bail!("profile '{profile}' resolved to no procs");
    }
    Ok((root, specs))
}

/// Parses oosrun.yaml and resolves `profile` (following includes).
fn parse(text: &str, root: &Path, profile: &str) -> Result<Vec<ProcSpec>> {
    let doc: serde_yaml::Value = serde_yaml::from_str(text)?;
    let profiles = doc
        .get("profiles")
        .and_then(serde_yaml::Value::as_mapping)
        .ok_or_else(|| anyhow!("missing top-level `profiles` mapping"))?;

    if profiles.get(profile).is_none() {
        let mut names: Vec<&str> = profiles.keys().filter_map(serde_yaml::Value::as_str).collect();
        names.sort_unstable();
        bail!("unknown profile '{profile}'; available: {}", names.join(", "));
    }

    let mut specs = Vec::new();
    let mut visited = HashSet::new();
    resolve(profiles, profile, root, &mut visited, &mut specs)?;

    // Keep the first occurrence of each name so includes can overlap harmlessly
    // while preserving order.
    let mut seen = HashSet::new();
    specs.retain(|s| seen.insert(s.name.clone()));
    Ok(specs)
}

/// Recursively collects the procs for `profile`, depth-first over includes.
fn resolve(
    profiles: &serde_yaml::Mapping,
    profile: &str,
    root: &Path,
    visited: &mut HashSet<String>,
    out: &mut Vec<ProcSpec>,
) -> Result<()> {
    // Guard against include cycles.
    if !visited.insert(profile.to_string()) {
        return Ok(());
    }
    let body = profiles
        .get(profile)
        .and_then(serde_yaml::Value::as_mapping)
        .ok_or_else(|| anyhow!("profile '{profile}' must be a mapping"))?;

    if let Some(includes) = body.get("include") {
        let seq = includes
            .as_sequence()
            .ok_or_else(|| anyhow!("profile '{profile}': include must be an array"))?;
        for inc in seq {
            let child = inc
                .as_str()
                .ok_or_else(|| anyhow!("profile '{profile}': include entries must be strings"))?;
            resolve(profiles, child, root, visited, out)?;
        }
    }

    if let Some(procs) = body.get("procs") {
        let seq = procs
            .as_sequence()
            .ok_or_else(|| anyhow!("profile '{profile}': procs must be an array"))?;
        for entry in seq {
            out.push(parse_proc(entry, root)?);
        }
    }
    Ok(())
}

/// Builds one ProcSpec from a `procs` list entry.
fn parse_proc(val: &serde_yaml::Value, root: &Path) -> Result<ProcSpec> {
    let map = val
        .as_mapping()
        .ok_or_else(|| anyhow!("proc entry must be a mapping"))?;

    let name = map
        .get("name")
        .and_then(serde_yaml::Value::as_str)
        .ok_or_else(|| anyhow!("proc entry missing `name`"))?
        .to_string();

    let argv = match (map.get("shell"), map.get("cmd")) {
        (Some(s), None) => {
            let s = s
                .as_str()
                .ok_or_else(|| anyhow!("proc '{name}': shell must be a string"))?;
            shell_argv(s)
        }
        (None, Some(c)) => {
            let seq = c
                .as_sequence()
                .ok_or_else(|| anyhow!("proc '{name}': cmd must be an array"))?;
            seq.iter()
                .map(|v| {
                    v.as_str()
                        .map(str::to_string)
                        .ok_or_else(|| anyhow!("proc '{name}': cmd entries must be strings"))
                })
                .collect::<Result<Vec<_>>>()?
        }
        (Some(_), Some(_)) => bail!("proc '{name}': set exactly one of shell or cmd"),
        (None, None) => bail!("proc '{name}': missing shell or cmd"),
    };
    if argv.is_empty() {
        bail!("proc '{name}': empty command");
    }

    let cwd = map
        .get("cwd")
        .and_then(serde_yaml::Value::as_str)
        .map(|c| resolve_cwd(c, root));

    let mut env = Vec::new();
    if let Some(e) = map.get("env") {
        let m = e
            .as_mapping()
            .ok_or_else(|| anyhow!("proc '{name}': env must be a mapping"))?;
        for (k, v) in m {
            let key = k
                .as_str()
                .ok_or_else(|| anyhow!("proc '{name}': env keys must be strings"))?
                .to_string();
            if let Some(val) = v.as_str() {
                env.push((key, val.to_string()));
            }
        }
    }

    let autostart = map
        .get("autostart")
        .and_then(serde_yaml::Value::as_bool)
        .unwrap_or(true);

    Ok(ProcSpec {
        name,
        argv,
        cwd,
        env,
        autostart,
    })
}

/// Wraps a shell command string the way a login shell would run it.
fn shell_argv(shell: &str) -> Vec<String> {
    vec!["/bin/sh".to_string(), "-c".to_string(), shell.to_string()]
}

/// Resolves a configured cwd against the repo root when it is relative.
fn resolve_cwd(cwd: &str, root: &Path) -> PathBuf {
    let p = Path::new(cwd);
    if p.is_absolute() {
        p.to_path_buf()
    } else {
        root.join(p)
    }
}
