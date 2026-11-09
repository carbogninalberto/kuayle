// CRUD engine: generic list/read/create/update/delete from ResourceSpec.
// CRUD 引擎：从 ResourceSpec 生成通用 list/read/create/update/delete。
//
// The engine eliminates hand-written match arms for homogeneous CRUD.
// Each resource gets list/read for free from its spec; only special
// actions (cycles burndown, etc.) remain hand-written.
// 引擎消除了同构 CRUD 的手写 match。每个资源从 spec 自动获得
// list/read；只有特殊动作（cycles burndown 等）保留手写。

use kuayle_sdk::client::Client;
use serde::Serialize;
use serde_json::Value;

use crate::registry::ResourceSpec;

/// Execute a generic list command from a ResourceSpec.
/// 从 ResourceSpec 执行通用 list 命令。
///
/// Handles pagination (--all), truncation hints, and dual-format output.
/// 处理分页（--all）、截断提示和双格式输出。
pub async fn execute_list(
    spec: &ResourceSpec,
    path: &str,
    client: &Client,
    is_json: bool,
    all: bool,
) {
    // Fetch first page of items.
    // 抓取第一页项目。
    let page_path = if path.contains('?') {
        format!("{path}&page=1&per_page=100")
    } else {
        format!("{path}?page=1&per_page=100")
    };

    let items: Vec<Value> = match client.get(&page_path).await {
        Ok(v) => v,
        Err(e) => {
            crate::output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    };

    if is_json {
        println!(
            "{}",
            serde_json::to_string_pretty(&items).unwrap_or_default()
        );
        return;
    }

    if items.is_empty() {
        println!("No {} found.", spec.name);
        return;
    }

    // Human table output.
    // 人类表格输出。
    let headers = (spec.headers_fn)();
    let row_fn = spec.row_fn;

    // Calculate column widths.
    let mut widths: Vec<usize> = headers.iter().map(|h| h.len()).collect();
    for item in &items {
        let row = row_fn(item);
        for (i, cell) in row.iter().enumerate() {
            if i < widths.len() {
                widths[i] = widths[i].max(cell.len());
            }
        }
    }

    // Print header + separator.
    let header_line: Vec<String> = headers
        .iter()
        .enumerate()
        .map(|(i, h)| format!("{:<width$}", h, width = widths[i]))
        .collect();
    println!("{}", header_line.join("  "));
    let sep: Vec<String> = widths.iter().map(|w| "-".repeat(*w)).collect();
    println!("{}", sep.join("  "));

    // Print rows.
    for item in &items {
        let row = row_fn(item);
        let line: Vec<String> = row
            .iter()
            .enumerate()
            .map(|(i, cell)| format!("{:<width$}", cell, width = widths[i]))
            .collect();
        println!("{}", line.join("  "));
    }

    // Truncation hint.
    let has_more = items.len() >= 100 && !all;
    if has_more {
        println!("… and more (use --all)");
    }
    println!("\n{} {} found", items.len(), spec.name);
}

/// Execute a generic read command from a ResourceSpec.
/// 从 ResourceSpec 执行通用 read 命令。
pub async fn execute_read(spec: &ResourceSpec, item_path: &str, client: &Client, is_json: bool) {
    let item: Value = match client.get(item_path).await {
        Ok(v) => v,
        Err(e) => {
            crate::output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    };

    if is_json {
        println!(
            "{}",
            serde_json::to_string_pretty(&item).unwrap_or_default()
        );
        return;
    }

    // Use the spec's detail function for human output.
    (spec.detail_fn)(&item);
}

/// Execute a generic create command.
/// 执行通用 create 命令。
pub async fn execute_create<T: Serialize>(
    spec: &ResourceSpec,
    path: &str,
    client: &Client,
    body: &T,
    is_json: bool,
) {
    let result: Value = match client.post(path, body).await {
        Ok(v) => v,
        Err(e) => {
            crate::output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    };

    if is_json {
        println!(
            "{}",
            serde_json::to_string_pretty(&result).unwrap_or_default()
        );
    } else {
        println!("✓ Created {}", spec.name);
    }
}

/// Execute a generic update command.
/// 执行通用 update 命令。
pub async fn execute_update<T: Serialize>(
    spec: &ResourceSpec,
    item_path: &str,
    client: &Client,
    body: &T,
    is_json: bool,
) {
    let result: Value = match client.patch(item_path, body).await {
        Ok(v) => v,
        Err(e) => {
            crate::output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    };

    if is_json {
        println!(
            "{}",
            serde_json::to_string_pretty(&result).unwrap_or_default()
        );
    } else {
        println!("✓ Updated {}", spec.name);
    }
}

/// Execute a generic delete command.
/// 执行通用 delete 命令。
pub async fn execute_delete(spec: &ResourceSpec, item_path: &str, client: &Client, is_json: bool) {
    match client.delete::<Value>(item_path).await {
        Ok(_) => {
            if is_json {
                println!(r#"{{"deleted":true}}"#);
            } else {
                println!("✓ Deleted {}", spec.name);
            }
        }
        Err(e) => {
            crate::output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}
