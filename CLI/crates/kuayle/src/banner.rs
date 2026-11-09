// Purple ASCII banner shown on interactive help.
//
// The banner is decoration for people at a terminal. It is never printed when
// stdout is piped (agents, scripts, golden snapshots), when NO_COLOR is set or
// when TERM is "dumb", so machine-readable output never changes.

use std::io::IsTerminal;

const LETTERS: [[&str; 6]; 6] = [
    // K
    [
        "██╗  ██╗",
        "██║ ██╔╝",
        "█████╔╝ ",
        "██╔═██╗ ",
        "██║  ██╗",
        "╚═╝  ╚═╝",
    ],
    // U
    [
        "██╗   ██╗",
        "██║   ██║",
        "██║   ██║",
        "██║   ██║",
        "╚██████╔╝",
        " ╚═════╝ ",
    ],
    // A
    [
        " █████╗ ",
        "██╔══██╗",
        "███████║",
        "██╔══██║",
        "██║  ██║",
        "╚═╝  ╚═╝",
    ],
    // Y
    [
        "██╗   ██╗",
        "╚██╗ ██╔╝",
        " ╚████╔╝ ",
        "  ╚██╔╝  ",
        "   ██║   ",
        "   ╚═╝   ",
    ],
    // L
    [
        "██╗     ",
        "██║     ",
        "██║     ",
        "██║     ",
        "███████╗",
        "╚══════╝",
    ],
    // E
    [
        "███████╗",
        "██╔════╝",
        "█████╗  ",
        "██╔══╝  ",
        "███████╗",
        "╚══════╝",
    ],
];

/// Kuayle's primary palette (UI/src/app.css --primary-200 … --primary-500),
/// light to deep violet, one shade per banner row.
const ROW_RGB: [(u8, u8, u8); 6] = [
    (0xb0, 0xa6, 0xf5),
    (0x9d, 0x91, 0xf3),
    (0x8b, 0x7b, 0xf0),
    (0x78, 0x66, 0xee),
    (0x66, 0x50, 0xeb),
    (0x58, 0x44, 0xe2),
];

/// Nearest xterm-256 colours for terminals without truecolor.
const ROW_256: [u8; 6] = [147, 147, 141, 105, 99, 62];

/// How the banner may be coloured.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ColorMode {
    TrueColor,
    Ansi256,
}

/// The six banner lines without any colour.
pub fn plain_lines() -> Vec<String> {
    (0..6)
        .map(|row| LETTERS.iter().map(|letter| letter[row]).collect())
        .collect()
}

/// The banner with each row coloured, followed by a tagline.
pub fn render(mode: ColorMode) -> String {
    let mut out = String::new();
    for (row, line) in plain_lines().iter().enumerate() {
        let color = match mode {
            ColorMode::TrueColor => {
                let (r, g, b) = ROW_RGB[row];
                format!("\x1b[1;38;2;{r};{g};{b}m")
            }
            ColorMode::Ansi256 => format!("\x1b[1;38;5;{}m", ROW_256[row]),
        };
        out.push_str(&format!("{color}{line}\x1b[0m\n"));
    }
    out.push_str("\x1b[2mThe official Kuayle CLI · run `kuayle usage` for every command\x1b[0m\n");
    out
}

/// Decide whether and how to colour, from the environment and stdout.
/// Returns None when the banner must not be printed.
pub fn color_mode(
    stdout_is_terminal: bool,
    no_color: Option<&str>,
    term: Option<&str>,
    colorterm: Option<&str>,
) -> Option<ColorMode> {
    // https://no-color.org: any non-empty value disables colour.
    if !stdout_is_terminal || no_color.is_some_and(|v| !v.is_empty()) || term == Some("dumb") {
        return None;
    }
    match colorterm {
        Some("truecolor") | Some("24bit") => Some(ColorMode::TrueColor),
        _ => Some(ColorMode::Ansi256),
    }
}

/// The banner for this process, or None when output is not interactive.
pub fn for_terminal() -> Option<String> {
    let no_color = std::env::var("NO_COLOR").ok();
    let term = std::env::var("TERM").ok();
    let colorterm = std::env::var("COLORTERM").ok();
    color_mode(
        std::io::stdout().is_terminal(),
        no_color.as_deref(),
        term.as_deref(),
        colorterm.as_deref(),
    )
    .map(render)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn lines_are_aligned() {
        let lines = plain_lines();
        assert_eq!(lines.len(), 6);
        let width = lines[0].chars().count();
        assert!(lines.iter().all(|l| l.chars().count() == width));
    }

    #[test]
    fn never_shown_when_piped_or_colour_disabled() {
        assert_eq!(color_mode(false, None, None, Some("truecolor")), None);
        assert_eq!(color_mode(true, Some("1"), None, None), None);
        assert_eq!(color_mode(true, None, Some("dumb"), None), None);
    }

    #[test]
    fn picks_truecolor_only_when_advertised() {
        assert_eq!(
            color_mode(true, None, Some("xterm-256color"), Some("truecolor")),
            Some(ColorMode::TrueColor)
        );
        assert_eq!(
            color_mode(true, Some(""), Some("xterm"), None),
            Some(ColorMode::Ansi256)
        );
    }

    #[test]
    fn rendered_rows_are_purple_and_reset() {
        let out = render(ColorMode::TrueColor);
        assert!(out.contains("\x1b[1;38;2;176;166;245m"));
        assert_eq!(out.matches("\x1b[0m").count(), 7);
    }
}
