//! TUI rendering: a process sidebar plus the selected proc's live terminal.
//!
//! The pty/vt100 layer already lays output out to a fixed grid, so rendering is
//! a straight cell-by-cell translation of the vt100 screen into ratatui spans —
//! no wrapping or reflow here. Pane dimensions are derived arithmetically (see
//! pane_dims) so app.rs can size the ptys to exactly what we draw.

use ratatui::layout::{Constraint, Layout, Size};
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, List, ListItem, Paragraph};
use ratatui::Frame;

use crate::app::App;
use crate::proc::Status;

/// Inner size (rows, cols) of the output pane for a given terminal size. Must
/// stay in sync with the layout in `draw`: one footer row, a bordered pane.
pub fn pane_dims(area: Size, sidebar_width: u16) -> (u16, u16) {
    let cols = area.width.saturating_sub(sidebar_width + 2).max(1);
    let rows = area.height.saturating_sub(3).max(1);
    (rows, cols)
}

pub fn draw(f: &mut Frame, app: &App) {
    let [body, footer] =
        Layout::vertical([Constraint::Min(0), Constraint::Length(1)]).areas(f.area());
    let [side, pane] =
        Layout::horizontal([Constraint::Length(app.sidebar_width), Constraint::Min(0)]).areas(body);

    draw_sidebar(f, side, app);
    draw_pane(f, pane, app);
    draw_footer(f, footer);
}

fn draw_sidebar(f: &mut Frame, area: ratatui::layout::Rect, app: &App) {
    let items: Vec<ListItem> = app
        .procs
        .iter()
        .enumerate()
        .map(|(i, p)| {
            let (glyph, color) = status_glyph(&p.status);
            let label = match &p.status {
                Status::Exited(code) => format!("{} (exit {code})", p.name),
                Status::Failed(_) => format!("{} (failed)", p.name),
                _ => p.name.clone(),
            };
            let line = Line::from(vec![
                Span::styled(format!(" {glyph} "), Style::default().fg(color)),
                Span::raw(label),
            ]);
            let mut item = ListItem::new(line);
            if i == app.selected {
                item = item.style(Style::default().add_modifier(Modifier::REVERSED));
            }
            item
        })
        .collect();

    f.render_widget(List::new(items).block(Block::bordered().title(" procs ")), area);
}

fn draw_pane(f: &mut Frame, area: ratatui::layout::Rect, app: &App) {
    let Some(proc) = app.procs.get(app.selected) else {
        return;
    };
    let title = format!(" {} \u{2014} {} ", proc.name, status_text(&proc.status));
    let block = Block::bordered().title(Line::from(title));
    let inner = block.inner(area);
    f.render_widget(block, area);

    let lines = {
        let parser = proc.parser().lock().expect("parser mutex poisoned");
        screen_lines(parser.screen())
    };
    f.render_widget(Paragraph::new(lines), inner);
}

fn draw_footer(f: &mut Frame, area: ratatui::layout::Rect) {
    let hint = " \u{2191}/\u{2193} select   1-9 jump   r restart   s stop   a start   q quit ";
    f.render_widget(
        Paragraph::new(Line::from(hint)).style(Style::default().add_modifier(Modifier::DIM)),
        area,
    );
}

/// Translates one vt100 screen into ratatui lines, cell by cell.
fn screen_lines(screen: &vt100::Screen) -> Vec<Line<'static>> {
    let (rows, cols) = screen.size();
    let mut lines = Vec::with_capacity(rows as usize);
    for row in 0..rows {
        let mut spans = Vec::with_capacity(cols as usize);
        for col in 0..cols {
            match screen.cell(row, col) {
                Some(cell) => {
                    let mut style = Style::default();
                    if let Some(c) = convert_color(cell.fgcolor()) {
                        style = style.fg(c);
                    }
                    if let Some(c) = convert_color(cell.bgcolor()) {
                        style = style.bg(c);
                    }
                    if cell.bold() {
                        style = style.add_modifier(Modifier::BOLD);
                    }
                    if cell.italic() {
                        style = style.add_modifier(Modifier::ITALIC);
                    }
                    if cell.underline() {
                        style = style.add_modifier(Modifier::UNDERLINED);
                    }
                    if cell.inverse() {
                        style = style.add_modifier(Modifier::REVERSED);
                    }
                    // vt100 returns "" for blank cells; render a space so the
                    // grid keeps its shape.
                    let contents = cell.contents();
                    let text = if contents.is_empty() {
                        " ".to_string()
                    } else {
                        contents.to_string()
                    };
                    spans.push(Span::styled(text, style));
                }
                None => spans.push(Span::raw(" ")),
            }
        }
        lines.push(Line::from(spans));
    }
    lines
}

/// Maps a vt100 colour to a ratatui colour; Default means "leave unset".
fn convert_color(c: vt100::Color) -> Option<Color> {
    match c {
        vt100::Color::Default => None,
        vt100::Color::Idx(i) => Some(Color::Indexed(i)),
        vt100::Color::Rgb(r, g, b) => Some(Color::Rgb(r, g, b)),
    }
}

/// Sidebar status glyph and its colour.
fn status_glyph(status: &Status) -> (char, Color) {
    match status {
        Status::Running => ('\u{25CF}', Color::Green), // ●
        Status::Exited(0) => ('\u{2713}', Color::DarkGray), // ✓
        Status::Exited(_) => ('\u{2717}', Color::Red), // ✗
        Status::Stopped => ('\u{25A0}', Color::Yellow), // ■
        Status::Failed(_) => ('!', Color::Red),
    }
}

/// Human-readable status for the pane title.
fn status_text(status: &Status) -> String {
    match status {
        Status::Running => "running".to_string(),
        Status::Exited(code) => format!("exited {code}"),
        Status::Stopped => "stopped".to_string(),
        Status::Failed(reason) => format!("failed: {reason}"),
    }
}
