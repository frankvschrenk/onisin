//! Application state and the main event loop.

use std::path::PathBuf;
use std::time::Duration;

use anyhow::Result;
use crossterm::event::{self, Event, KeyCode, KeyEvent, KeyEventKind, KeyModifiers};
use ratatui::DefaultTerminal;

use crate::config::ProcSpec;
use crate::proc::Proc;
use crate::ui;

pub struct App {
    pub procs: Vec<Proc>,
    pub selected: usize,
    pub sidebar_width: u16,
    should_quit: bool,
}

impl App {
    pub fn new(specs: Vec<ProcSpec>, root: PathBuf) -> Self {
        // Sidebar wide enough for the longest name plus glyph and exit suffix,
        // clamped to a sensible band.
        let widest = specs.iter().map(|s| s.name.len()).max().unwrap_or(8) as u16;
        let sidebar_width = (widest + 14).clamp(18, 32);

        // Provisional pty size; the first loop iteration resizes to the real
        // pane before anything is drawn.
        let size = (24, 80);
        let procs = specs
            .into_iter()
            .map(|spec| Proc::new(spec, root.clone(), size))
            .collect();

        App {
            procs,
            selected: 0,
            sidebar_width,
            should_quit: false,
        }
    }

    pub fn run(mut self, terminal: &mut DefaultTerminal) -> Result<()> {
        let mut redraw = true;
        while !self.should_quit {
            // Size every pty to the visible pane so output is laid out correctly
            // and a proc is ready the moment it is selected.
            let area = terminal.size()?;
            let (rows, cols) = ui::pane_dims(area, self.sidebar_width);
            for proc in &mut self.procs {
                proc.resize(rows, cols);
            }

            // Reap any procs that exited on their own.
            for proc in &mut self.procs {
                proc.poll();
            }
            if self.procs.iter().any(Proc::take_dirty) {
                redraw = true;
            }

            if redraw {
                terminal.draw(|f| ui::draw(f, &self))?;
                redraw = false;
            }

            // Block briefly for input; the timeout bounds the redraw latency for
            // background output.
            if event::poll(Duration::from_millis(50))? {
                match event::read()? {
                    Event::Key(key) => {
                        self.on_key(key);
                        redraw = true;
                    }
                    Event::Resize(_, _) => redraw = true,
                    _ => {}
                }
            }
        }

        self.shutdown();
        Ok(())
    }

    fn on_key(&mut self, key: KeyEvent) {
        // Ignore key-release events on platforms that emit them.
        if key.kind == KeyEventKind::Release {
            return;
        }
        let ctrl = key.modifiers.contains(KeyModifiers::CONTROL);
        match key.code {
            KeyCode::Char('q') | KeyCode::Esc => self.should_quit = true,
            KeyCode::Char('c') if ctrl => self.should_quit = true,
            KeyCode::Down | KeyCode::Char('j') | KeyCode::Tab => self.select_next(),
            KeyCode::Up | KeyCode::Char('k') | KeyCode::BackTab => self.select_prev(),
            KeyCode::Char(d @ '1'..='9') => {
                let idx = d as usize - '1' as usize;
                if idx < self.procs.len() {
                    self.selected = idx;
                }
            }
            KeyCode::Char('r') => {
                if let Some(proc) = self.procs.get_mut(self.selected) {
                    proc.restart();
                }
            }
            KeyCode::Char('s') => {
                if let Some(proc) = self.procs.get_mut(self.selected) {
                    proc.stop();
                }
            }
            KeyCode::Char('a') => {
                if let Some(proc) = self.procs.get_mut(self.selected) {
                    if !proc.is_running() {
                        proc.start();
                    }
                }
            }
            _ => {}
        }
    }

    fn select_next(&mut self) {
        if !self.procs.is_empty() {
            self.selected = (self.selected + 1) % self.procs.len();
        }
    }

    fn select_prev(&mut self) {
        if !self.procs.is_empty() {
            self.selected = (self.selected + self.procs.len() - 1) % self.procs.len();
        }
    }

    /// Tear every child down on exit so nothing is orphaned.
    fn shutdown(&mut self) {
        for proc in &mut self.procs {
            proc.stop();
        }
    }
}
