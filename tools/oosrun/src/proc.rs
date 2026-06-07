//! A supervised process.
//!
//! Each proc runs on its own pty so the child sees a real tty (colours,
//! unbuffered output, correct `isatty`). A reader thread feeds the pty output
//! into a vt100 parser, which the UI renders. We keep the master (for resize)
//! and the child handle (for signals and liveness) for as long as it runs.

use std::io::Read;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

use anyhow::{Context, Result};
use portable_pty::{native_pty_system, Child, CommandBuilder, MasterPty, PtySize};

use crate::config::ProcSpec;

/// Lifecycle state, surfaced in the sidebar.
#[derive(Debug, Clone)]
pub enum Status {
    Running,
    /// Exited on its own with this code.
    Exited(i32),
    /// Stopped on purpose by the user.
    Stopped,
    /// Spawning failed; carries the reason.
    Failed(String),
}

pub struct Proc {
    pub name: String,
    pub status: Status,

    spec: ProcSpec,
    default_cwd: PathBuf,

    /// Shared with the reader thread; replaced on every (re)start so a fresh
    /// run starts from a clean screen.
    parser: Arc<Mutex<vt100::Parser>>,
    /// Set by the reader thread / lifecycle changes; drives redraws.
    dirty: Arc<AtomicBool>,

    master: Option<Box<dyn MasterPty + Send>>,
    child: Option<Box<dyn Child + Send + Sync>>,
    /// For a macOS .app launched via `open`: the inner executable to terminate
    /// on stop, since `open` reparents the real app away from us. None for
    /// directly-spawned procs.
    app_exe: Option<PathBuf>,
    /// Current pty size as (rows, cols).
    size: (u16, u16),
}

impl Proc {
    /// Creates the proc and starts it when `autostart` is set.
    pub fn new(spec: ProcSpec, default_cwd: PathBuf, size: (u16, u16)) -> Self {
        let parser = Arc::new(Mutex::new(vt100::Parser::new(size.0, size.1, 0)));
        let mut proc = Proc {
            name: spec.name.clone(),
            status: Status::Stopped,
            spec,
            default_cwd,
            parser,
            dirty: Arc::new(AtomicBool::new(true)),
            master: None,
            child: None,
            app_exe: None,
            size,
        };
        if proc.spec.autostart {
            proc.start();
        }
        proc
    }

    pub fn is_running(&self) -> bool {
        matches!(self.status, Status::Running)
    }

    /// Handle to the parser for the UI to lock and render.
    pub fn parser(&self) -> &Arc<Mutex<vt100::Parser>> {
        &self.parser
    }

    /// Returns and clears the dirty flag; true means a redraw is warranted.
    pub fn take_dirty(&self) -> bool {
        self.dirty.swap(false, Ordering::Relaxed)
    }

    /// Starts the proc, recording a Failed status if spawning errors out.
    pub fn start(&mut self) {
        match self.try_start() {
            Ok(()) => self.status = Status::Running,
            Err(e) => self.status = Status::Failed(format!("{e:#}")),
        }
        self.dirty.store(true, Ordering::Relaxed);
    }

    fn try_start(&mut self) -> Result<()> {
        let pty = native_pty_system();
        let pair = pty.openpty(PtySize {
            rows: self.size.0,
            cols: self.size.1,
            pixel_width: 0,
            pixel_height: 0,
        })?;

        // How a user launches this proc differs by platform. Services, and the
        // app binaries on Linux/Windows, are plain executables we run directly
        // — which is exactly how their launchers start them. A macOS .app is
        // different: a double-click goes through LaunchServices, and running the
        // inner Mach-O directly would NOT reproduce that (it would inherit our
        // shell environment and a tty, and skip Gatekeeper). So for a bundle we
        // shell out to `open` just like the Finder does, and remember the inner
        // executable so stop/restart can terminate the real app (killing the
        // `open` process would not).
        // Resolve a relative program/bundle path against the proc's cwd (repo
        // root) so detection and launching work no matter where oosrun itself
        // was started from.
        let base_cwd = self
            .spec
            .cwd
            .clone()
            .unwrap_or_else(|| self.default_cwd.clone());
        let target = {
            let p = Path::new(&self.spec.argv[0]);
            if p.is_absolute() {
                p.to_path_buf()
            } else {
                base_cwd.join(p)
            }
        };
        let is_app = is_macos_app_bundle(&target);
        let mut cmd = if is_app {
            self.app_exe = app_executable(&target);
            let mut c = CommandBuilder::new("open");
            c.arg("-W"); // keep our child alive for the app's lifetime
            c.arg("-n"); // always launch a fresh instance (so restart works)
            c.arg(target.as_os_str());
            if self.spec.argv.len() > 1 {
                c.arg("--args");
                for arg in &self.spec.argv[1..] {
                    c.arg(arg);
                }
            }
            c
        } else {
            self.app_exe = None;
            let mut c = CommandBuilder::new(&target);
            for arg in &self.spec.argv[1..] {
                c.arg(arg);
            }
            c
        };
        cmd.cwd(self.spec.cwd.clone().unwrap_or_else(|| self.default_cwd.clone()));
        // Inherit our environment explicitly (portable-pty does not by default),
        // then layer the config overrides on top.
        for (k, v) in std::env::vars() {
            cmd.env(k, v);
        }
        for (k, v) in &self.spec.env {
            cmd.env(k, v);
        }

        let child = pair.slave.spawn_command(cmd).context("spawn failed")?;
        drop(pair.slave); // close the parent's slave end so EOF propagates

        let reader = pair.master.try_clone_reader()?;
        // Fresh parser for this run so restart clears the screen.
        let parser = Arc::new(Mutex::new(vt100::Parser::new(self.size.0, self.size.1, 0)));
        self.parser = parser.clone();
        let dirty = self.dirty.clone();
        dirty.store(true, Ordering::Relaxed);

        // A LaunchServices-started app writes to the system log, not to this
        // pty, so the pane would otherwise look empty. Drop in a hint pointing
        // at where its output actually goes.
        if is_app {
            let proc_name = self
                .app_exe
                .as_ref()
                .and_then(|p| p.file_name())
                .map(|n| n.to_string_lossy().into_owned())
                .unwrap_or_else(|| self.name.clone());
            let banner = format!(
                "\x1b[2m[oosrun] launched via LaunchServices (open) — the real user launch.\r\n\
                 GUI output goes to the system log, not here. Tail it with:\r\n  \
                 log stream --predicate 'process == \"{proc_name}\"'\x1b[0m\r\n"
            );
            if let Ok(mut p) = parser.lock() {
                p.process(banner.as_bytes());
            }
        }

        thread::spawn(move || {
            let mut reader = reader;
            let mut buf = [0u8; 8192];
            loop {
                match reader.read(&mut buf) {
                    Ok(0) | Err(_) => break,
                    Ok(n) => {
                        if let Ok(mut p) = parser.lock() {
                            p.process(&buf[..n]);
                        }
                        dirty.store(true, Ordering::Relaxed);
                    }
                }
            }
        });

        self.master = Some(pair.master);
        self.child = Some(child);
        Ok(())
    }

    /// User-initiated stop: terminate and mark as stopped.
    pub fn stop(&mut self) {
        self.terminate();
        self.status = Status::Stopped;
        self.dirty.store(true, Ordering::Relaxed);
    }

    /// Stop then start again with the same spec and current size.
    pub fn restart(&mut self) {
        self.terminate();
        self.start();
    }

    /// Resizes the pty (and parser) to match the visible pane.
    pub fn resize(&mut self, rows: u16, cols: u16) {
        if (rows, cols) == self.size {
            return;
        }
        self.size = (rows, cols);
        if let Some(master) = &self.master {
            let _ = master.resize(PtySize {
                rows,
                cols,
                pixel_width: 0,
                pixel_height: 0,
            });
        }
        if let Ok(mut p) = self.parser.lock() {
            // vt100 exposes resizing on the Screen, not the Parser.
            p.screen_mut().set_size(rows, cols);
        }
        self.dirty.store(true, Ordering::Relaxed);
    }

    /// Reaps the child if it has exited on its own, updating the status (unless
    /// the user already stopped it).
    pub fn poll(&mut self) {
        let exited = match &mut self.child {
            Some(child) => match child.try_wait() {
                Ok(Some(status)) => Some(status.exit_code() as i32),
                _ => None,
            },
            None => None,
        };
        if let Some(code) = exited {
            self.child = None;
            self.master = None;
            if self.is_running() {
                self.status = Status::Exited(code);
            }
            self.dirty.store(true, Ordering::Relaxed);
        }
    }

    /// Sends a signal to the child process if one is running.
    fn signal(&self, sig: libc::c_int) {
        if let Some(child) = &self.child {
            if let Some(pid) = child.process_id() {
                // Safety: kill on a pid we own; harmless if the pid is already gone.
                unsafe {
                    libc::kill(pid as libc::pid_t, sig);
                }
            }
        }
    }

    /// SIGTERM, a short grace period, then SIGKILL, then reap. Keeps the UI from
    /// blocking for long because our services exit promptly on SIGTERM.
    fn terminate(&mut self) {
        // A macOS app launched via `open` is reparented to launchd, so our child
        // is the `open` process, not the app. Terminate the real app by its
        // inner executable path; `open -W` then returns and gets reaped below.
        if let Some(exe) = self.app_exe.clone() {
            let _ = std::process::Command::new("pkill")
                .arg("-f")
                .arg(exe.as_os_str())
                .status();
        }
        self.signal(libc::SIGTERM);
        if let Some(mut child) = self.child.take() {
            let mut reaped = false;
            for _ in 0..50 {
                if let Ok(Some(_)) = child.try_wait() {
                    reaped = true;
                    break;
                }
                thread::sleep(Duration::from_millis(10));
            }
            if !reaped {
                let _ = child.kill();
                let _ = child.wait();
            }
        }
        self.master = None;
    }
}

/// True when `path` is a macOS application bundle that should be launched via
/// `open`. On other platforms there is no bundle concept, so this is always
/// false and the app's plain executable is run directly — which is how users
/// launch it on Linux and Windows.
fn is_macos_app_bundle(path: &Path) -> bool {
    cfg!(target_os = "macos") && path.is_dir() && path.join("Contents/MacOS").is_dir()
}

/// Resolves a bundle to the executable inside it: Contents/MacOS/<exe>, where
/// <exe> is the Info.plist CFBundleExecutable, falling back to the single file
/// present in Contents/MacOS.
fn app_executable(app: &Path) -> Option<PathBuf> {
    let macos = app.join("Contents/MacOS");
    if let Some(name) = std::fs::read_to_string(app.join("Contents/Info.plist"))
        .ok()
        .and_then(|plist| parse_cfbundle_executable(&plist))
    {
        let exe = macos.join(&name);
        if exe.is_file() {
            return Some(exe);
        }
    }
    std::fs::read_dir(&macos)
        .ok()?
        .flatten()
        .map(|entry| entry.path())
        .find(|p| p.is_file())
}

/// Extracts CFBundleExecutable from an XML Info.plist without pulling in a full
/// plist parser: find the key, then the next <string>…</string>. Binary plists
/// fail the UTF-8 read upstream and fall back to the single-file heuristic.
fn parse_cfbundle_executable(plist: &str) -> Option<String> {
    let key = plist.find("CFBundleExecutable")?;
    let rest = &plist[key..];
    let open = rest.find("<string>")? + "<string>".len();
    let close = rest[open..].find("</string>")?;
    Some(rest[open..open + close].trim().to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn extracts_bundle_executable_from_plist() {
        let plist = r#"<?xml version="1.0"?>
            <plist><dict>
              <key>CFBundleName</key><string>Onisin OS</string>
              <key>CFBundleExecutable</key><string>oos</string>
            </dict></plist>"#;
        assert_eq!(parse_cfbundle_executable(plist).as_deref(), Some("oos"));
    }

    #[test]
    fn resolves_bundle_to_inner_executable() {
        let base = std::env::temp_dir()
            .join(format!("oosrun-bundle-test-{}.app", std::process::id()));
        let macos = base.join("Contents/MacOS");
        std::fs::create_dir_all(&macos).unwrap();
        std::fs::write(
            base.join("Contents/Info.plist"),
            "<key>CFBundleExecutable</key><string>myapp</string>",
        )
        .unwrap();
        let exe = macos.join("myapp");
        std::fs::write(&exe, b"#!/bin/sh\n").unwrap();

        assert_eq!(app_executable(&base), Some(exe));

        std::fs::remove_dir_all(&base).ok();
    }
}
