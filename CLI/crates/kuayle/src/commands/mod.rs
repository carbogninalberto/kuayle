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
pub mod workspaces;

use kuayle_sdk::client::Client;
use url::Url;

use crate::cli::Cli;

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
