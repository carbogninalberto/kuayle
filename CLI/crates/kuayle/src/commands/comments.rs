// Comment commands: list, create, resolve, reopen.
// 评论命令：list、create、resolve、reopen。
//
// API paths confirmed against local kuayle instance (2026-07-29):
// - GET  /api/workspaces/{ws}/issues/{issue}/comments
// - POST /api/workspaces/{ws}/issues/{issue}/comments
// - POST /api/workspaces/{ws}/issues/{issue}/comments/{id}/resolve
// - POST /api/workspaces/{ws}/issues/{issue}/comments/{id}/reopen
// API 路径已对照本地 kuayle 实例确认（2026-07-29）。

use kuayle_sdk::client::Client;
use kuayle_sdk::types::comment::{CommentResponse, CreateCommentRequest};

use crate::cli::{Cli, CommentAction};
use crate::output::{self, is_json_output};

pub async fn handle(action: &CommentAction, cli: &Cli) {
    match action {
        CommentAction::List { issue } => cmd_list(cli, issue).await,
        CommentAction::Create { issue, body } => cmd_create(cli, issue, body).await,
        CommentAction::Resolve { issue, id } => cmd_resolve(cli, issue, id).await,
        CommentAction::Reopen { issue, id } => cmd_reopen(cli, issue, id).await,
    }
}

async fn resolve(cli: &Cli) -> (Client, String, bool) {
    let is_json = is_json_output(cli);
    let (client, _url) = match crate::commands::resolve_client(cli).await {
        Ok(c) => c,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };
    let ws = cli.workspace.as_deref().unwrap_or("acme").to_string();
    (client, ws, is_json)
}

async fn cmd_list(cli: &Cli, issue: &str) {
    let (client, ws, is_json) = resolve(cli).await;
    let path = format!("/api/workspaces/{ws}/issues/{issue}/comments");

    match client.get::<Vec<CommentResponse>>(&path).await {
        Ok(comments) => {
            if is_json {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&comments).unwrap_or_default()
                );
            } else {
                if comments.is_empty() {
                    println!("No comments on issue {issue}.");
                    return;
                }
                for c in &comments {
                    let user = c
                        .user
                        .as_ref()
                        .map(|u| u.display_name.as_str())
                        .unwrap_or(&c.user_id);
                    let resolved = if c.is_resolved() { " ✓" } else { "" };
                    println!("[{user}] ({}){resolved}", c.created_at);
                    println!("  {}", c.body);
                    println!();
                }
                println!("{} comment(s)", comments.len());
            }
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

async fn cmd_create(cli: &Cli, issue: &str, body: &str) {
    let (client, ws, is_json) = resolve(cli).await;
    let path = format!("/api/workspaces/{ws}/issues/{issue}/comments");
    let req = CreateCommentRequest {
        body: body.to_string(),
    };

    match client.post::<_, CommentResponse>(&path, &req).await {
        Ok(comment) => {
            println!("✓ Comment created on issue {issue} (id: {})", comment.id);
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

/// Mark a comment as resolved.
/// POST /api/workspaces/{ws}/issues/{issue}/comments/{id}/resolve (no body).
/// 将评论标记为已解决。
/// POST /api/workspaces/{ws}/issues/{issue}/comments/{id}/resolve（无 body）。
async fn cmd_resolve(cli: &Cli, issue: &str, id: &str) {
    let (client, ws, is_json) = resolve(cli).await;
    let path = format!("/api/workspaces/{ws}/issues/{issue}/comments/{id}/resolve");

    match client
        .post::<_, CommentResponse>(&path, &serde_json::Value::Null)
        .await
    {
        Ok(_) => println!("✓ Comment {id} resolved on issue {issue}"),
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

/// Reopen a resolved comment.
/// POST /api/workspaces/{ws}/issues/{issue}/comments/{id}/reopen (no body).
/// 重新打开已解决的评论。
/// POST /api/workspaces/{ws}/issues/{issue}/comments/{id}/reopen（无 body）。
async fn cmd_reopen(cli: &Cli, issue: &str, id: &str) {
    let (client, ws, is_json) = resolve(cli).await;
    let path = format!("/api/workspaces/{ws}/issues/{issue}/comments/{id}/reopen");

    match client
        .post::<_, CommentResponse>(&path, &serde_json::Value::Null)
        .await
    {
        Ok(_) => println!("✓ Comment {id} reopened on issue {issue}"),
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}
