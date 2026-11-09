// Workspace commands: list.
// 工作区命令：list。

use kuayle_sdk::client::Client;
use kuayle_sdk::types::workspace::WorkspaceResponse;
use url::Url;

use crate::cli::{Cli, WorkspaceAction};
use crate::output::{self, is_json_output};

/// Handle workspace subcommand dispatch.
/// 处理 workspace 子命令分发。
pub async fn handle(action: &WorkspaceAction, cli: &Cli) {
    match action {
        WorkspaceAction::List => cmd_list(cli).await,
    }
}

async fn cmd_list(cli: &Cli) {
    let is_json = is_json_output(cli);
    let client = match resolve_client(cli).await {
        Ok(c) => c,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };

    match client
        .get::<Vec<WorkspaceResponse>>("/api/workspaces")
        .await
    {
        Ok(workspaces) => {
            if is_json {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&workspaces).unwrap_or_default()
                );
            } else {
                if workspaces.is_empty() {
                    println!("No workspaces found.");
                    return;
                }
                println!("{:<40} {:<20} {:<15}", "NAME", "SLUG", "ROLE");
                println!("{:-<40} {:-<20} {:-<15}", "", "", "");
                for ws in &workspaces {
                    println!(
                        "{:<40} {:<20} {:<15}",
                        ws.name, ws.slug, ws.current_user_role
                    );
                }
                println!("\n{} workspace(s)", workspaces.len());
            }
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

async fn resolve_client(cli: &Cli) -> Result<Client, String> {
    let config = crate::config::Config::load_or_default();
    let resolved = config
        .resolve(cli.profile.as_deref(), cli.url.as_deref(), None)
        .map_err(|e| format!("config: {e}"))?;

    let store =
        crate::creds::get_credential_store().map_err(|e| format!("credential store: {e}"))?;

    let session = store
        .load(&resolved.profile)
        .map_err(|e| format!("load session: {e}"))?
        .ok_or_else(|| "not logged in. Run 'kuayle auth login' to authenticate.".to_string())?;

    let base_url = Url::parse(&resolved.url).map_err(|e| format!("invalid URL: {e}"))?;
    Ok(Client::new(base_url, session.bearer_token().to_string()))
}
