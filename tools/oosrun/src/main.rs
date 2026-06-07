//! oosrun — a small process supervisor with a TUI for the compiled onisin stack.
//!
//! Why we build this ourselves instead of depending on mprocs: upstream mprocs
//! ships no library API (it is a binary), and the only lib-includable fork is an
//! unmaintained copy of the whole TUI app — not a dependency worth carrying.
//! The pieces that matter (a pty per proc, a vt100 screen, a sidebar to switch
//! between them) are small enough to own outright, so oosrun reads the profiles
//! from oosrun.yaml and supervises them directly.
//!
//! Usage: `oosrun [all|api|ui|dev]` (defaults to the full stack).

mod app;
mod config;
mod proc;
mod ui;

use anyhow::Result;

fn main() -> Result<()> {
    let profile = std::env::args().nth(1).unwrap_or_else(|| "all".to_string());
    if matches!(profile.as_str(), "-h" | "--help") {
        usage();
        return Ok(());
    }

    let (root, specs) = match config::load(&profile) {
        Ok(loaded) => loaded,
        Err(e) => {
            eprintln!("oosrun: {e:#}");
            usage();
            std::process::exit(2);
        }
    };

    // ratatui::init enters the alternate screen, turns on raw mode and installs
    // a panic hook that restores the terminal, so a panic mid-run never leaves a
    // wrecked terminal behind.
    let mut terminal = ratatui::init();
    let result = app::App::new(specs, root).run(&mut terminal);
    ratatui::restore();
    result
}

fn usage() {
    eprintln!("usage: oosrun [all|api|ui|dev]");
    eprintln!("  all  full compiled stack (default)");
    eprintln!("  api  headless Rust services");
    eprintln!("  ui   Tauri desktop apps");
    eprintln!("  dev  services via cargo run (live rebuild)");
}
