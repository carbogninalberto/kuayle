// Relation commands: list, create, delete.
// 关系命令：list、create、delete。
//
// Uses Client directly for relation endpoints since there is no
// dedicated resource module for relations in kuayle-sdk.
// 直接使用 Client 访问关系端点，因为 kuayle-sdk 没有专门的关系资源模块。

use kuayle_sdk::client::Client;
use kuayle_sdk::types::relation::{CreateRelationRequest, RelationResponse};

use crate::cli::{Cli, RelationAction};
use crate::output::{self, is_json_output};

/// Handle relation subcommand dispatch.
/// 处理 relation 子命令分发。
pub async fn handle(action: &RelationAction, cli: &Cli) {
    match action {
        RelationAction::List { issue } => cmd_list(cli, issue).await,
        RelationAction::Create {
            issue,
            related,
            r#type,
        } => cmd_create(cli, issue, related, r#type).await,
        RelationAction::Delete { issue, id } => cmd_delete(cli, issue, id).await,
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

/// List relations for an issue.
/// 列出 issue 的关系。
async fn cmd_list(cli: &Cli, issue: &str) {
    let (client, ws, is_json) = resolve(cli).await;
    let path = format!("/api/workspaces/{ws}/issues/{issue}/relations");

    match client.get::<Vec<RelationResponse>>(&path).await {
        Ok(relations) => {
            if is_json {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&relations).unwrap_or_default()
                );
            } else {
                if relations.is_empty() {
                    println!("No relations for issue {issue}.");
                    return;
                }
                println!(
                    "{:<12}  {:<40}  {:<40}  {:<15}",
                    "TYPE", "RELATED ISSUE", "RELATED ID", "ID"
                );
                println!("{:-<12}  {:-<40}  {:-<40}  {:-<15}", "", "", "", "");
                for r in &relations {
                    println!(
                        "{:<12}  {:<40}  {:<40}  {:<15}",
                        r.relation_type,
                        truncate(&r.related_issue_id, 40),
                        r.related_issue_id,
                        r.id,
                    );
                }
                println!("\n{} relation(s)", relations.len());
            }
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

// ── create ──────────────────────────────────────────────────────────

/// Create a relation between two issues.
/// 在两个 issue 之间创建关系。
async fn cmd_create(cli: &Cli, issue: &str, related: &str, relation_type: &str) {
    let (client, ws, is_json) = resolve(cli).await;
    let path = format!("/api/workspaces/{ws}/issues/{issue}/relations");

    let req = CreateRelationRequest {
        related_identifier: related.to_string(),
        relation_type: relation_type.to_string(),
    };

    match client.post::<_, RelationResponse>(&path, &req).await {
        Ok(relation) => {
            if is_json {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&relation).unwrap_or_default()
                );
            } else {
                println!("✓ Created relation on issue {issue}");
                println!(
                    "  {} → {} ({})",
                    issue, relation.related_issue_id, relation.relation_type
                );
            }
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

// ── delete ──────────────────────────────────────────────────────────

/// Delete a relation by ID.
/// DELETE /api/workspaces/{ws}/issues/{issue}/relations/{id}
/// 通过 ID 删除关系。
async fn cmd_delete(cli: &Cli, issue: &str, id: &str) {
    let (client, ws, is_json) = resolve(cli).await;
    let path = format!("/api/workspaces/{ws}/issues/{issue}/relations/{id}");

    match client.delete::<serde_json::Value>(&path).await {
        Ok(_) => {
            if is_json {
                println!(r#"{{"deleted":"{id}"}}"#);
            } else {
                println!("✓ Deleted relation {id}");
            }
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

// ── helpers ─────────────────────────────────────────────────────────

/// Truncate a string to `max` chars, appending "…" if truncated.
/// 将字符串截断到 `max` 个字符，超出则追加 "…"。
fn truncate(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        s.to_string()
    } else {
        // Truncate by chars, not bytes — byte slicing panics inside multi-byte UTF-8.
        // 按字符数截断而非字节数——按字节切片会在多字节 UTF-8 字符内部 panic。
        let kept: String = s.chars().take(max.saturating_sub(1)).collect();
        format!("{kept}…")
    }
}

#[cfg(test)]
mod tests {
    use super::truncate;

    #[test]
    fn truncate_handles_multibyte_utf8() {
        assert_eq!(truncate("Wave 4: 通知通道抽象", 12), "Wave 4: 通知通…");
        assert_eq!(truncate("短", 5), "短");
    }
}
