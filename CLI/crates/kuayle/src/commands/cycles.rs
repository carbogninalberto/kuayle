// Cycle commands — engine-driven list/read + hand-written burndown/velocity.
use crate::cli::{Cli, CycleAction};
use crate::engine;
use crate::output::{self, is_json_output};
use crate::registry::RESOURCES;
fn spec() -> &'static crate::registry::ResourceSpec {
    RESOURCES.iter().find(|r| r.name == "cycles").unwrap()
}
pub async fn handle(action: &CycleAction, cli: &Cli) {
    match action {
        CycleAction::List { team } => cmd_list(cli, team).await,
        CycleAction::Read { team, id } => cmd_read(cli, team, id).await,
        CycleAction::Burndown { team, id } => cmd_burndown(cli, team, id).await,
        CycleAction::Velocity { team } => cmd_velocity(cli, team).await,
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
    let path = format!("/api/workspaces/{ws}/teams/{team}/cycles");
    engine::execute_list(spec(), &path, &client, is_json, false).await;
}
async fn cmd_read(cli: &Cli, team: &str, id: &str) {
    let (client, ws, is_json) = setup(cli).await;
    let path = format!("/api/workspaces/{ws}/teams/{team}/cycles/{id}");
    engine::execute_read(spec(), &path, &client, is_json).await;
}
async fn cmd_burndown(cli: &Cli, team: &str, id: &str) {
    let (client, ws, is_json) = setup(cli).await;
    let path = format!("/api/workspaces/{ws}/teams/{team}/cycles/{id}/burndown");
    let v: serde_json::Value = match client.get(&path).await {
        Ok(v) => v,
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    };
    println!("{}", serde_json::to_string_pretty(&v).unwrap_or_default());
}
async fn cmd_velocity(cli: &Cli, team: &str) {
    let (client, ws, is_json) = setup(cli).await;
    let path = format!("/api/workspaces/{ws}/teams/{team}/cycles/velocity");
    let v: serde_json::Value = match client.get(&path).await {
        Ok(v) => v,
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    };
    println!("{}", serde_json::to_string_pretty(&v).unwrap_or_default());
}
