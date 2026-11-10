// Notification commands: list the caller's notifications (user-scoped).
//
// GET /api/notifications returns {"notifications": [...], "unread_count": n}.

use serde_json::Value;

use crate::cli::{Cli, NotificationAction};
use crate::output::{self, is_json_output, Format};

pub async fn handle(action: &NotificationAction, cli: &Cli) {
    match action {
        NotificationAction::List => cmd_list(cli).await,
    }
}

async fn cmd_list(cli: &Cli) {
    let is_json = is_json_output(cli);
    let (client, _url) = match crate::commands::resolve_client(cli).await {
        Ok(c) => c,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };
    let body: Value = match client.get("/api/notifications").await {
        Ok(v) => v,
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    };
    if is_json {
        println!(
            "{}",
            serde_json::to_string_pretty(&body).unwrap_or_default()
        );
        return;
    }
    let items = body["notifications"]
        .as_array()
        .cloned()
        .unwrap_or_default();
    output::print_table(
        Format::Human,
        &items,
        &["TYPE", "ISSUE", "TITLE", "READ"],
        |n| {
            vec![
                n["type"].as_str().unwrap_or("-").to_string(),
                n["issue_identifier"].as_str().unwrap_or("-").to_string(),
                n["title"].as_str().unwrap_or("-").to_string(),
                if n["read_at"].is_null() { "no" } else { "yes" }.to_string(),
            ]
        },
    );
    println!("{} unread", body["unread_count"].as_i64().unwrap_or(0));
}
