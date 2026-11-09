// Label commands: list, create, update, delete.
// 标签命令：list、create、update、delete。
//
// Uses Client directly for label endpoints since there is no
// dedicated resource module for labels in kuayle-sdk.
// 直接使用 Client 访问标签端点，因为 kuayle-sdk 没有专门的标签资源模块。

use kuayle_sdk::client::Client;
use kuayle_sdk::types::label::{CreateLabelRequest, LabelResponse, UpdateLabelRequest};

use crate::cli::{Cli, LabelAction};
use crate::output::{self, is_json_output};

/// Handle label subcommand dispatch.
/// 处理 label 子命令分发。
pub async fn handle(action: &LabelAction, cli: &Cli) {
    match action {
        LabelAction::List => cmd_list(cli).await,
        LabelAction::Create { name, color } => cmd_create(cli, name, color.as_deref()).await,
        LabelAction::Update { id, name, color } => {
            cmd_update(cli, id, name.as_deref(), color.as_deref()).await
        }
        LabelAction::Delete { id } => cmd_delete(cli, id).await,
    }
}

// ── resolve helper ──────────────────────────────────────────────────

/// Resolve client, workspace slug, and is_json flag from CLI context.
/// 从 CLI 上下文解析 client、工作区 slug 和 is_json 标志。
async fn resolve(cli: &Cli) -> (Client, String, bool) {
    let is_json = is_json_output(cli);
    let (client, _url) = match crate::commands::resolve_client(cli).await {
        Ok(c) => c,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };
    let ws = cli.workspace.as_deref().unwrap_or("acme").to_string();
    (client, ws, is_json)
}

// ── list ────────────────────────────────────────────────────────────

/// List all labels in the workspace.
/// 列出工作区中的所有标签。
async fn cmd_list(cli: &Cli) {
    let (client, ws, is_json) = resolve(cli).await;
    let path = format!("/api/workspaces/{ws}/labels");

    match client.get::<Vec<LabelResponse>>(&path).await {
        Ok(labels) => {
            if is_json {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&labels).unwrap_or_default()
                );
            } else {
                if labels.is_empty() {
                    println!("No labels found.");
                    return;
                }
                println!("{:<40}  {:<25}  {:<15}", "NAME", "COLOR", "ID");
                println!("{:-<40}  {:-<25}  {:-<15}", "", "", "");
                for l in &labels {
                    let color = l.color.as_deref().unwrap_or("-");
                    println!("{:<40}  {:<25}  {:<15}", l.name, color, l.id);
                }
                println!("\n{} label(s)", labels.len());
            }
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

// ── create ──────────────────────────────────────────────────────────

/// Create a new label in the workspace.
/// 在工作区中创建新标签。
async fn cmd_create(cli: &Cli, name: &str, color: Option<&str>) {
    let (client, ws, is_json) = resolve(cli).await;
    let path = format!("/api/workspaces/{ws}/labels");

    let req = CreateLabelRequest {
        name: name.to_string(),
        color: color.map(|s| s.to_string()),
        description: None,
        parent_id: None,
    };

    match client.post::<_, LabelResponse>(&path, &req).await {
        Ok(label) => {
            if is_json {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&label).unwrap_or_default()
                );
            } else {
                let color_display = label.color.as_deref().unwrap_or("-");
                println!("✓ Created label \"{}\" ({color_display})", label.name);
                println!("  id: {}", label.id);
            }
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

// ── update ──────────────────────────────────────────────────────────

/// Update an existing label.
/// 更新已有标签。
async fn cmd_update(cli: &Cli, id: &str, name: Option<&str>, color: Option<&str>) {
    let (client, ws, is_json) = resolve(cli).await;
    let path = format!("/api/workspaces/{ws}/labels/{id}");

    let req = UpdateLabelRequest {
        name: name.map(|s| s.to_string()),
        color: color.map(|s| s.to_string()),
        description: None,
        parent_id: None,
    };

    match client.patch::<_, LabelResponse>(&path, &req).await {
        Ok(label) => {
            if is_json {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&label).unwrap_or_default()
                );
            } else {
                println!("✓ Updated label \"{}\"", label.name);
            }
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

// ── delete ──────────────────────────────────────────────────────────

/// Delete a label by ID.
/// 通过 ID 删除标签。
async fn cmd_delete(cli: &Cli, id: &str) {
    let (client, ws, is_json) = resolve(cli).await;
    let path = format!("/api/workspaces/{ws}/labels/{id}");

    match client.delete::<serde_json::Value>(&path).await {
        Ok(_) => {
            if is_json {
                println!(r#"{{"deleted":"{id}"}}"#);
            } else {
                println!("✓ Deleted label {id}");
            }
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}
