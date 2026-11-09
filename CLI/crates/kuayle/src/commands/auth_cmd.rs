// Auth commands: login, logout, status, whoami.
// 认证命令：login、logout、status、whoami。

use kuayle_sdk::client::Client;
use kuayle_sdk::types::user::UserResponse;
use url::Url;

use crate::cli::{AuthAction, Cli};
use crate::config::Config;
use crate::creds;
use crate::output::{self, is_json_output};

/// Handle auth subcommand dispatch.
/// 处理 auth 子命令分发。
pub async fn handle(action: &AuthAction, cli: &Cli) {
    match action {
        AuthAction::Login { token } => cmd_login(cli, token.as_deref()).await,
        AuthAction::Logout => cmd_logout(cli),
        AuthAction::Status => cmd_status(cli).await,
    }
}

/// Handle whoami command.
/// 处理 whoami 命令。
pub async fn handle_whoami(cli: &Cli) {
    cmd_whoami(cli).await;
}

// ── login ─────────────────────────────────────────────────────────

async fn cmd_login(cli: &Cli, token: Option<&str>) {
    let is_json = is_json_output(cli);

    let token = match token {
        Some(t) => t.to_string(),
        None => {
            output::print_string_error("--token is required for PAT authentication", 4, is_json);
        }
    };

    let url = match &cli.url {
        Some(u) => u.clone(),
        None => {
            output::print_string_error(
                "--url is required (e.g. --url http://localhost:5173)",
                4,
                is_json,
            );
        }
    };

    let base_url = match Url::parse(&url) {
        Ok(u) => u,
        Err(e) => {
            output::print_string_error(&format!("invalid URL '{url}': {e}"), 4, is_json);
        }
    };

    let client = Client::new(base_url, token.clone());
    match client.get::<UserResponse>("/api/auth/me").await {
        Ok(user) => {
            let store = match creds::get_credential_store() {
                Ok(s) => s,
                Err(e) => output::print_string_error(&e, 1, is_json),
            };

            let profile = Config::load_or_default().resolve_profile(cli.profile.as_deref());
            let session = kuayle_sdk::session::Session::pat(&token);

            if let Err(e) = store.save(&profile, &session) {
                output::print_string_error(&format!("saving credentials: {e}"), 1, is_json);
            }

            if let Err(e) = save_profile_to_config(&profile, &url) {
                eprintln!("Warning: could not save profile to config: {e}");
            }

            println!("✓ Logged in as {} ({})", user.display_name, user.email);
            println!("  Profile: {profile}");
            println!("  URL: {url}");
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

// ── logout ────────────────────────────────────────────────────────

fn cmd_logout(cli: &Cli) {
    let is_json = is_json_output(cli);
    let config = Config::load_or_default();
    let profile = config.resolve_profile(cli.profile.as_deref());

    let store = match creds::get_credential_store() {
        Ok(s) => s,
        Err(e) => output::print_string_error(&e, 1, is_json),
    };

    match store.delete(&profile) {
        Ok(()) => {
            println!("✓ Logged out of profile '{}'", profile);
        }
        Err(e) => output::print_string_error(&e, 1, is_json),
    }
}

// ── status ────────────────────────────────────────────────────────

async fn cmd_status(cli: &Cli) {
    let is_json = is_json_output(cli);
    let config = Config::load_or_default();
    let profile = config.resolve_profile(cli.profile.as_deref());

    let store = match creds::get_credential_store() {
        Ok(s) => s,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };

    let session = match store.load(&profile) {
        Ok(Some(s)) => s,
        Ok(None) => {
            if is_json {
                println!(r#"{{"error":{{"kind":"authentication","message":"not logged in"}}}}"#);
                std::process::exit(2);
            }
            println!("Not logged in. Run 'kuayle auth login' to authenticate.");
            std::process::exit(2);
        }
        Err(e) => output::print_string_error(&format!("loading session: {e}"), 1, is_json),
    };

    let url = match &cli.url {
        Some(u) => u.clone(),
        None => match config.resolve(cli.profile.as_deref(), None, None) {
            Ok(r) => r.url,
            Err(_) => {
                output::print_string_error(
                    "could not determine instance URL. Use --url or set it in config.",
                    1,
                    is_json,
                );
            }
        },
    };

    println!("Profile:  {profile}");
    println!("URL:      {url}");
    match session {
        kuayle_sdk::session::Session::Pat { .. } => {
            println!("Auth:     Personal Access Token");

            let base_url = match Url::parse(&url) {
                Ok(u) => u,
                Err(_) => {
                    println!("Status:   (could not validate — invalid URL)");
                    return;
                }
            };

            let client = Client::new(base_url, session.bearer_token().to_string());
            match client.get::<UserResponse>("/api/auth/me").await {
                Ok(user) => {
                    println!(
                        "Status:   ✓ authenticated as {} ({})",
                        user.display_name, user.email
                    );
                }
                Err(e) => {
                    println!("Status:   ✗ token invalid or expired ({e})");
                    std::process::exit(2);
                }
            }
        }
    }
}

// ── whoami ────────────────────────────────────────────────────────

async fn cmd_whoami(cli: &Cli) {
    let is_json = is_json_output(cli);
    let resolved = match resolve_client(cli).await {
        Ok((client, _url)) => client,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };

    match resolved.get::<UserResponse>("/api/auth/me").await {
        Ok(user) => {
            if is_json {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&user).unwrap_or_default()
                );
            } else {
                println!("ID:           {}", user.id);
                println!("Name:         {}", user.name);
                println!("Display Name: {}", user.display_name);
                println!("Email:        {}", user.email);
                println!("Sysadmin:     {}", user.is_sysadmin);
            }
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

// ── helpers ───────────────────────────────────────────────────────

async fn resolve_client(cli: &Cli) -> Result<(Client, String), String> {
    let config = Config::load_or_default();
    let resolved = config
        .resolve(cli.profile.as_deref(), cli.url.as_deref(), None)
        .map_err(|e| format!("config: {e}"))?;

    let store = creds::get_credential_store().map_err(|e| format!("credential store: {e}"))?;

    let session = store
        .load(&resolved.profile)
        .map_err(|e| format!("load session: {e}"))?
        .ok_or_else(|| "not logged in. Run 'kuayle auth login' to authenticate.".to_string())?;

    let base_url = Url::parse(&resolved.url).map_err(|e| format!("invalid URL: {e}"))?;
    let client = Client::new(base_url, session.bearer_token().to_string());

    Ok((client, resolved.url))
}

fn save_profile_to_config(profile: &str, url: &str) -> Result<(), String> {
    let path = crate::config::config_path()?;
    let mut config = Config::load_from(&path)?;

    let entry = config
        .profiles
        .entry(profile.to_string())
        .or_insert_with(|| crate::config::ProfileConfig {
            url: url.to_string(),
            workspace: None,
        });
    entry.url = url.to_string();

    if config.default_profile.is_none() {
        config.default_profile = Some(profile.to_string());
    }

    let toml_str = toml::to_string_pretty(&config).map_err(|e| format!("serialize: {e}"))?;
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).map_err(|e| format!("create config dir: {e}"))?;
    }
    std::fs::write(&path, toml_str).map_err(|e| format!("write config: {e}"))?;

    Ok(())
}
