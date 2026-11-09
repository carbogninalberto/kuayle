// Statuses commands — list team statuses via engine.
use crate::cli::{Cli, StatusesAction};
use crate::output::{self, is_json_output};
pub async fn handle(action: &StatusesAction, cli: &Cli) {
    match action {
        StatusesAction::List { team } => cmd_list(cli, team).await,
    }
}
async fn setup(cli: &Cli) -> (kuayle_sdk::client::Client, String, bool) {
    let is_json = is_json_output(cli);
    let (client, _url) = match crate::commands::resolve_client(cli).await {
        Ok(c) => c,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };
    (
        client,
        cli.workspace.as_deref().unwrap_or("acme").to_string(),
        is_json,
    )
}
async fn cmd_list(cli: &Cli, team: &str) {
    let (client, ws, is_json) = setup(cli).await;
    // Resolve team name to team_id via Resolver, then list statuses.
    let resolver = crate::resolve::Resolver::new(client.clone(), &ws, cli.no_cache);
    let team_id = match resolver
        .resolve(crate::resolve::ResolveKind::Teams, team)
        .await
    {
        Ok(id) => id,
        Err(e) => output::print_string_error(&e, 3, is_json),
    };
    let path = format!("/api/workspaces/{ws}/teams/{team_id}/statuses");
    // Use a simple engine-like list (teams spec headers not right for statuses, use raw).
    // Actually, statuses has its own shape -- print name | category | color.
    let items: Vec<serde_json::Value> = match client.get(&path).await {
        Ok(v) => v,
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    };
    if is_json {
        println!(
            "{}",
            serde_json::to_string_pretty(&items).unwrap_or_default()
        );
    } else {
        if items.is_empty() {
            println!("No statuses found.");
            return;
        }
        println!("{:<20} {:<15} {:<10}", "NAME", "CATEGORY", "COLOR");
        println!("{:-<20} {:-<15} {:-<10}", "", "", "");
        for item in &items {
            let name = item["name"].as_str().unwrap_or("-");
            let cat = item["category"].as_str().unwrap_or("-");
            let color = item["color"].as_str().unwrap_or("-");
            println!("{:<20} {:<15} {:<10}", name, cat, color);
        }
        println!("\n{} status(es)", items.len());
    }
}
