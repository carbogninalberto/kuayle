// Output formatting: human / JSON with tty auto-detection.
// 输出格式化：human / JSON，带 tty 自动检测。
#![allow(dead_code)] // Wired in item #8
                     //
                     // In human mode, output is designed for terminal display.
                     // In JSON mode, output is machine-readable.
                     // Auto mode detects whether stdout is a terminal.
                     // human 模式下输出专为终端显示设计。
                     // JSON 模式下输出为机器可读。
                     // auto 模式检测 stdout 是否为终端。

use serde::Serialize;

/// Output format for CLI commands.
/// CLI 命令的输出格式。
#[derive(Debug, Clone, Copy, PartialEq)]
#[allow(dead_code)]
pub enum Format {
    /// Human-readable terminal output.
    /// 人类可读的终端输出。
    Human,
    /// Machine-readable JSON output.
    /// 机器可读的 JSON 输出。
    Json,
}

impl Format {
    /// Determine the output format from a CLI flag and tty status.
    /// 根据 CLI flag 和 tty 状态确定输出格式。
    pub fn from_flag(flag: &str) -> Self {
        match flag {
            "human" => Format::Human,
            "json" => Format::Json,
            _ => {
                if std::io::IsTerminal::is_terminal(&std::io::stdout()) {
                    Format::Human
                } else {
                    Format::Json
                }
            }
        }
    }

    pub fn is_json(self) -> bool {
        matches!(self, Format::Json)
    }
}

/// Print a single value in the specified format.
/// 以指定格式打印单个值。
pub fn print_one<T: Serialize>(_format: Format, value: &T) {
    println!(
        "{}",
        serde_json::to_string_pretty(value).unwrap_or_default()
    );
}

/// Print a list of values as a table (human) or JSON array (json).
/// 以表格（human）或 JSON 数组（json）打印值列表。
pub fn print_table<T: Serialize>(
    format: Format,
    items: &[T],
    headers: &[&str],
    row_fn: impl Fn(&T) -> Vec<String>,
) {
    match format {
        Format::Json => {
            println!(
                "{}",
                serde_json::to_string_pretty(items).unwrap_or_default()
            );
        }
        Format::Human => {
            if items.is_empty() {
                println!("No items found.");
                return;
            }

            let mut widths: Vec<usize> = headers.iter().map(|h| h.len()).collect();
            for item in items {
                let row = row_fn(item);
                for (i, cell) in row.iter().enumerate() {
                    if i < widths.len() {
                        widths[i] = widths[i].max(cell.len());
                    }
                }
            }

            let header_line: Vec<String> = headers
                .iter()
                .enumerate()
                .map(|(i, h)| format!("{:<width$}", h, width = widths[i]))
                .collect();
            println!("{}", header_line.join("  "));

            let sep: Vec<String> = widths.iter().map(|w| "-".repeat(*w)).collect();
            println!("{}", sep.join("  "));

            for item in items {
                let row = row_fn(item);
                let line: Vec<String> = row
                    .iter()
                    .enumerate()
                    .map(|(i, cell)| format!("{:<width$}", cell, width = widths[i]))
                    .collect();
                println!("{}", line.join("  "));
            }

            println!("\n{} item(s)", items.len());
        }
    }
}

/// Print a `KuayleError` respecting the output format.
/// 按输出格式打印 `KuayleError`。
pub fn print_error(err: &kuayle_sdk::error::KuayleError, is_json: bool) {
    if is_json {
        println!("{}", err.to_json_error());
    } else {
        eprintln!("Error: {err}");
    }
}

/// Print an error string to stdout (JSON) or stderr (human), then exit.
/// 将错误字符串打印到 stdout（JSON）或 stderr（human），然后退出。
pub fn print_string_error(msg: &str, exit_code: i32, is_json: bool) -> ! {
    if is_json {
        println!(
            r#"{{"error":{{"kind":"cli_error","message":"{}"}}}}"#,
            msg.replace('"', r#"\""#)
        );
    } else {
        eprintln!("Error: {msg}");
    }
    std::process::exit(exit_code);
}

/// Determine whether JSON output is requested from the CLI flags.
/// 根据 CLI flags 确定是否请求 JSON 输出。
pub fn is_json_output(cli: &crate::cli::Cli) -> bool {
    match cli.format.as_str() {
        "json" => true,
        "human" => false,
        _ => !std::io::IsTerminal::is_terminal(&std::io::stdout()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn format_from_flag_human() {
        assert_eq!(Format::from_flag("human"), Format::Human);
    }

    #[test]
    fn format_from_flag_json() {
        assert_eq!(Format::from_flag("json"), Format::Json);
    }

    #[test]
    fn format_is_json() {
        assert!(Format::Json.is_json());
        assert!(!Format::Human.is_json());
    }
}
