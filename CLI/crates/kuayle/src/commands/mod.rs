// Command modules.
// 命令模块。
pub mod assets;
pub mod auth_cmd;
pub mod comments;
pub mod cycles;
pub mod favorites;
pub mod issues;
pub mod labels;
pub mod members;
pub mod notifications;
pub mod projects;
pub mod relations;
pub mod statuses;
pub mod teams;
pub mod templates;
pub mod views;
pub mod wiki;
pub mod workspaces;

use kuayle_sdk::client::Client;
use url::Url;

use crate::cli::Cli;

/// The workspace slug for this invocation: --workspace, then KUAYLE_WORKSPACE,
/// then the profile's saved workspace. Exits with a usage error (code 4) when
/// none is set, rather than guessing a workspace.
pub fn workspace_slug(cli: &Cli) -> String {
    let is_json = crate::output::is_json_output(cli);
    let config = crate::config::Config::load_or_default();
    let resolved = config
        .resolve(
            cli.profile.as_deref(),
            cli.url.as_deref(),
            cli.workspace.as_deref(),
        )
        .ok()
        .and_then(|r| r.workspace)
        .filter(|w| !w.is_empty());
    match resolved {
        Some(ws) => ws,
        None => crate::output::print_string_error(
            "no workspace selected. Pass --workspace <slug>, set KUAYLE_WORKSPACE, or run 'kuayle auth login --workspace <slug>'.",
            4,
            is_json,
        ),
    }
}

/// Shared helper: resolve a Client from config and credentials.
/// 共享辅助：从配置和凭据解析 Client。
pub async fn resolve_client(cli: &Cli) -> Result<(Client, String), String> {
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
    let client = Client::new(base_url.clone(), session.bearer_token().to_string());
    Ok((client, resolved.url))
}
