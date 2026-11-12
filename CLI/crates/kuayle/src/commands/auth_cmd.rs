// Auth commands: login, logout, status, whoami.
// 认证命令：login、logout、status、whoami。

use std::io::{IsTerminal, Read};
use std::path::PathBuf;

use kuayle_sdk::client::Client;
use kuayle_sdk::session::{CredentialStore, Session};
use kuayle_sdk::types::user::UserResponse;
use serde::{Deserialize, Serialize};
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
        AuthAction::Connect { link, force } => cmd_connect(cli, link.as_deref(), *force).await,
        AuthAction::Logout => cmd_logout(cli).await,
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
                Err(e) => output::print_string_error(&redact(&e), 1, is_json),
            };

            let profile = Config::load_or_default().resolve_profile(cli.profile.as_deref());
            let session = Session::pat(&token);
            let snapshot = match SetupSnapshot::capture(&profile, store.as_ref()) {
                Ok(snapshot) => snapshot,
                Err(e) => output::print_string_error(&redact(&e), 1, is_json),
            };

            if let Err(e) = store.save(&profile, &session) {
                snapshot.restore(store.as_ref());
                output::print_string_error(
                    &redact(&format!("saving credentials: {e}")),
                    1,
                    is_json,
                );
            }

            if let Err(e) = save_profile_to_config(&profile, &url, cli.workspace.as_deref()) {
                // The profile must end up either fully configured or untouched.
                snapshot.restore(store.as_ref());
                output::print_string_error(&redact(&format!("saving profile: {e}")), 1, is_json);
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

// ── connect ───────────────────────────────────────────────────────

/// The parts of a connection link the CLI needs: the instance (origin plus
/// any path prefix before `/cli/connect`) and the one-time code.
#[derive(Debug, Clone, PartialEq)]
struct ConnectLink {
    base_url: Url,
    code: String,
    workspace: Option<String>,
}

impl ConnectLink {
    fn parse(raw: &str) -> Result<Self, String> {
        let url = Url::parse(raw.trim()).map_err(|e| format!("invalid connection link: {e}"))?;
        match url.scheme() {
            "https" => {}
            "http" => {
                let host = url.host_str().unwrap_or("");
                if host != "localhost" && host != "127.0.0.1" {
                    return Err(
                        "connection links must use https unless they point at localhost".into(),
                    );
                }
            }
            scheme => return Err(format!("unsupported connection link scheme '{scheme}'")),
        }

        let path = url.path();
        let marker = "/cli/connect";
        let Some(index) = path.rfind(marker) else {
            return Err("connection link does not point at /cli/connect".into());
        };
        if index + marker.len() != path.len() {
            return Err("connection link does not point at /cli/connect".into());
        }

        let mut base_url = url.clone();
        let prefix = &path[..index];
        base_url.set_path(if prefix.is_empty() { "/" } else { prefix });
        base_url.set_query(None);
        base_url.set_fragment(None);

        let fragment = url.fragment().unwrap_or_default();
        let params: std::collections::HashMap<String, String> =
            url::form_urlencoded::parse(fragment.as_bytes())
                .into_owned()
                .collect();
        let code = params.get("code").cloned().unwrap_or_default();
        let code_prefix = "kuayle_cl_";
        let random = code.strip_prefix(code_prefix).unwrap_or_default();
        if random.len() != 40 || !random.chars().all(|ch| ch.is_ascii_alphanumeric()) {
            return Err("connection link does not contain a valid code".into());
        }
        let workspace = params
            .get("workspace")
            .map(|w| w.trim())
            .filter(|w| !w.is_empty())
            .map(str::to_string);

        Ok(ConnectLink {
            base_url,
            code,
            workspace,
        })
    }
}

/// Read the link from the argument, KUAYLE_CONNECT_LINK, or stdin so the code
/// never has to be typed into a shell history.
fn read_connect_link(argument: Option<&str>) -> Result<String, String> {
    if let Some(link) = argument.map(str::trim).filter(|link| !link.is_empty()) {
        return Ok(link.to_string());
    }
    if let Ok(link) = std::env::var("KUAYLE_CONNECT_LINK") {
        if !link.trim().is_empty() {
            return Ok(link.trim().to_string());
        }
    }
    if std::io::stdin().is_terminal() {
        return Err("no connection link given. Pass it as an argument, set KUAYLE_CONNECT_LINK, or pipe it on stdin.".into());
    }
    let mut input = String::new();
    std::io::stdin()
        .read_to_string(&mut input)
        .map_err(|e| format!("reading the connection link from stdin: {e}"))?;
    let link = input.trim();
    if link.is_empty() {
        return Err("no connection link on stdin".into());
    }
    Ok(link.to_string())
}

#[derive(Serialize)]
struct RedeemRequest<'a> {
    code: &'a str,
    client_name: &'a str,
}

#[derive(Deserialize)]
struct RedeemResponse {
    token: String,
}

/// Configure a profile from a connection link. Nothing is written until the
/// link is redeemed and the minted token is verified; any later failure
/// restores the profile snapshot and revokes the token.
async fn cmd_connect(cli: &Cli, link: Option<&str>, force: bool) {
    let is_json = is_json_output(cli);

    let raw = match read_connect_link(link) {
        Ok(link) => link,
        Err(e) => output::print_string_error(&redact(&e), 4, is_json),
    };
    let link = match ConnectLink::parse(&raw) {
        Ok(link) => link,
        Err(e) => output::print_string_error(&redact(&e), 4, is_json),
    };

    let profile = Config::load_or_default().resolve_profile(cli.profile.as_deref());
    let store = match creds::get_credential_store() {
        Ok(store) => store,
        Err(e) => output::print_string_error(&redact(&e), 1, is_json),
    };
    match store.load(&profile) {
        Ok(Some(_)) if !force => output::print_string_error(
            &format!("profile '{profile}' already has credentials; pass --force to replace them"),
            4,
            is_json,
        ),
        Ok(_) => {}
        Err(e) => output::print_string_error(&redact(&format!("loading session: {e}")), 1, is_json),
    }
    let snapshot = match SetupSnapshot::capture(&profile, store.as_ref()) {
        Ok(snapshot) => snapshot,
        Err(e) => output::print_string_error(&redact(&e), 1, is_json),
    };

    // The code goes in the request body, never in the URL.
    let anonymous = Client::anonymous(link.base_url.clone());
    let redeemed: RedeemResponse = match anonymous
        .post(
            "/api/cli/connect",
            &RedeemRequest {
                code: &link.code,
                client_name: "kuayle-cli",
            },
        )
        .await
    {
        Ok(response) => response,
        Err(e) => {
            print_redacted_error(&e, is_json);
        }
    };
    let token = redeemed.token;
    if token.is_empty() {
        snapshot.restore(store.as_ref());
        output::print_string_error("the server returned an empty token", 1, is_json);
    }

    let client = Client::new(link.base_url.clone(), token.clone());
    let user = match client.get::<UserResponse>("/api/auth/me").await {
        Ok(user) => user,
        Err(e) => {
            snapshot.restore(store.as_ref());
            revoke_token(&link.base_url, &token).await;
            print_redacted_error(&e, is_json);
        }
    };

    let base = link.base_url.as_str().trim_end_matches('/');
    let result = store
        .save(&profile, &Session::pat(&token))
        .map_err(|e| format!("saving credentials: {e}"))
        .and_then(|()| save_profile_to_config(&profile, base, link.workspace.as_deref()));
    if let Err(e) = result {
        snapshot.restore(store.as_ref());
        revoke_token(&link.base_url, &token).await;
        output::print_string_error(&redact(&e), 1, is_json);
    }

    if is_json {
        println!(
            "{}",
            serde_json::json!({
                "profile": profile,
                "url": base,
                "workspace": link.workspace,
                "user": {
                    "email": user.email,
                    "display_name": user.display_name,
                },
            })
        );
    } else {
        println!("✓ Connected as {} ({})", user.display_name, user.email);
        println!("  Profile: {profile}");
        println!("  URL: {base}");
        if let Some(workspace) = &link.workspace {
            println!("  Workspace: {workspace}");
        }
    }
}

/// Best-effort revocation of a token minted during a failed setup.
async fn revoke_token(base_url: &Url, token: &str) {
    let client = Client::new(base_url.clone(), token.to_string());
    let _ = client
        .delete::<serde_json::Value>("/api/tokens/current")
        .await;
}

// ── snapshot ──────────────────────────────────────────────────────

/// The profile's credentials and config file before setup changed them.
struct SetupSnapshot {
    profile: String,
    session: Option<Session>,
    config_path: PathBuf,
    config_contents: Option<String>,
}

impl SetupSnapshot {
    fn capture(profile: &str, store: &dyn CredentialStore) -> Result<Self, String> {
        let config_path = crate::config::config_path()?;
        let config_contents = std::fs::read_to_string(&config_path).ok();
        let session = store.load(profile)?;
        Ok(SetupSnapshot {
            profile: profile.to_string(),
            session,
            config_path,
            config_contents,
        })
    }

    fn restore(&self, store: &dyn CredentialStore) {
        match &self.session {
            Some(session) => {
                let _ = store.save(&self.profile, session);
            }
            None => {
                let _ = store.delete(&self.profile);
            }
        }
        match &self.config_contents {
            Some(contents) => {
                if let Some(parent) = self.config_path.parent() {
                    let _ = std::fs::create_dir_all(parent);
                }
                let _ = std::fs::write(&self.config_path, contents);
            }
            None => {
                let _ = std::fs::remove_file(&self.config_path);
            }
        }
    }
}

// ── logout ────────────────────────────────────────────────────────

async fn cmd_logout(cli: &Cli) {
    let is_json = is_json_output(cli);
    let config = Config::load_or_default();
    let profile = config.resolve_profile(cli.profile.as_deref());

    let store = match creds::get_credential_store() {
        Ok(s) => s,
        Err(e) => output::print_string_error(&redact(&e), 1, is_json),
    };

    // Best-effort server-side revocation, so a copied credential file cannot
    // keep using the token after logout.
    if let Ok(Some(session)) = store.load(&profile) {
        if let Ok(resolved) = config.resolve(cli.profile.as_deref(), cli.url.as_deref(), None) {
            if let Ok(base_url) = Url::parse(&resolved.url) {
                revoke_token(&base_url, session.bearer_token()).await;
            }
        }
    }

    match store.delete(&profile) {
        Ok(()) => {
            println!("✓ Logged out of profile '{}'", profile);
        }
        Err(e) => output::print_string_error(&redact(&e), 1, is_json),
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

    if is_json {
        let base_url = Url::parse(&url).map_err(|e| e.to_string());
        let user = match base_url {
            Ok(base) => Client::new(base, session.bearer_token().to_string())
                .get::<UserResponse>("/api/auth/me")
                .await
                .map_err(|e| e.to_string()),
            Err(e) => Err(format!("invalid URL: {e}")),
        };
        let authenticated = user.is_ok();
        println!(
            "{}",
            serde_json::json!({
                "profile": profile,
                "url": url,
                "auth": "personal_access_token",
                "authenticated": authenticated,
                "user": user.as_ref().ok().map(|u| serde_json::json!({"email": u.email, "display_name": u.display_name})),
                "error": user.err(),
            })
        );
        if !authenticated {
            std::process::exit(2);
        }
        return;
    }

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

/// Mask Kuayle tokens and connection-link codes in a message, keeping only
/// the recognizable prefix and its 8 display characters.
fn redact(value: &str) -> String {
    const PREFIXES: [&str; 2] = ["kuayle_pat_", "kuayle_cl_"];
    const VISIBLE: usize = 8;

    let mut output = String::with_capacity(value.len());
    let mut rest = value;
    loop {
        let found = PREFIXES
            .iter()
            .filter_map(|prefix| rest.find(prefix).map(|index| (index, *prefix)))
            .min_by_key(|(index, _)| *index);
        let Some((index, prefix)) = found else {
            output.push_str(rest);
            break;
        };
        output.push_str(&rest[..index]);

        let candidate = &rest[index..];
        let tail = &candidate[prefix.len()..];
        let random_len: usize = tail
            .chars()
            .take_while(|ch| ch.is_ascii_alphanumeric())
            .map(char::len_utf8)
            .sum();
        let secret = &candidate[..prefix.len() + random_len];
        let keep = (prefix.len() + VISIBLE).min(secret.len());

        output.push_str(&secret[..keep]);
        if secret.len() > keep {
            output.push('…');
        }
        rest = &candidate[secret.len()..];
    }
    output
}

fn print_redacted_error(error: &kuayle_sdk::error::KuayleError, is_json: bool) -> ! {
    let message = redact(&error.to_string());
    if is_json {
        println!(
            "{}",
            serde_json::json!({"error": {"kind": error.kind(), "message": message}})
        );
    } else {
        eprintln!("Error: {message}");
    }
    std::process::exit(error.exit_code());
}

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

fn save_profile_to_config(profile: &str, url: &str, workspace: Option<&str>) -> Result<(), String> {
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
    if let Some(ws) = workspace.filter(|w| !w.is_empty()) {
        entry.workspace = Some(ws.to_string());
    }

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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_a_mounted_link() {
        let link = ConnectLink::parse(
            "https://app.example.com/kuayle/cli/connect#code=kuayle_cl_abcdefghijklmnopqrstuvwxyz0123456789ABCD&workspace=acme",
        )
        .unwrap();
        assert_eq!(link.base_url.as_str(), "https://app.example.com/kuayle");
        assert_eq!(
            link.code,
            "kuayle_cl_abcdefghijklmnopqrstuvwxyz0123456789ABCD"
        );
        assert_eq!(link.workspace.as_deref(), Some("acme"));
    }

    #[test]
    fn parses_a_root_link_without_workspace() {
        let link = ConnectLink::parse(
            "https://app.example.com/cli/connect#code=kuayle_cl_abcdefghijklmnopqrstuvwxyz0123456789ABCD",
        )
        .unwrap();
        assert_eq!(link.base_url.as_str(), "https://app.example.com/");
        assert_eq!(link.workspace, None);
    }

    #[test]
    fn allows_http_only_for_localhost() {
        assert!(ConnectLink::parse(
            "http://localhost:5173/cli/connect#code=kuayle_cl_abcdefghijklmnopqrstuvwxyz0123456789ABCD"
        )
        .is_ok());
        assert!(ConnectLink::parse(
            "http://127.0.0.1:5173/cli/connect#code=kuayle_cl_abcdefghijklmnopqrstuvwxyz0123456789ABCD"
        )
        .is_ok());
        assert!(ConnectLink::parse(
            "http://app.example.com/cli/connect#code=kuayle_cl_abcdefghijklmnopqrstuvwxyz0123456789ABCD"
        )
        .is_err());
    }

    #[test]
    fn rejects_links_without_a_valid_code() {
        assert!(ConnectLink::parse("https://app.example.com/cli/connect#workspace=acme").is_err());
        assert!(ConnectLink::parse(
            "https://app.example.com/cli/connect#code=kuayle_pat_abcdefghijklmnop"
        )
        .is_err());
        assert!(
            ConnectLink::parse("https://app.example.com/cli/connect#code=kuayle_cl_x").is_err()
        );
    }

    #[test]
    fn rejects_links_that_do_not_point_at_the_connect_page() {
        assert!(ConnectLink::parse(
            "https://app.example.com/settings#code=kuayle_cl_abcdefghijklmnopqrstuvwxyz0123456789ABCD"
        )
        .is_err());
        assert!(ConnectLink::parse(
            "https://app.example.com/cli/connect/extra#code=kuayle_cl_abcdefghijklmnopqrstuvwxyz0123456789ABCD"
        )
        .is_err());
        assert!(ConnectLink::parse(
            "ftp://app.example.com/cli/connect#code=kuayle_cl_abcdefghijklmnopqrstuvwxyz0123456789ABCD"
        )
        .is_err());
    }

    #[test]
    fn redacts_tokens_and_codes_but_keeps_context() {
        assert_eq!(
            redact("request failed for kuayle_pat_abcdefghijklmnop with 401"),
            "request failed for kuayle_pat_abcdefgh… with 401"
        );
        assert_eq!(
            redact("link kuayle_cl_0123456789abcdef is expired"),
            "link kuayle_cl_01234567… is expired"
        );
        assert_eq!(redact("no secret here"), "no secret here");
    }

    #[test]
    fn redacts_every_occurrence_and_short_values() {
        assert_eq!(
            redact("kuayle_pat_aaaaaaaaaaaaaaaa kuayle_cl_bbbbbbbbbbbbbbbb"),
            "kuayle_pat_aaaaaaaa… kuayle_cl_bbbbbbbb…"
        );
        assert_eq!(redact("kuayle_pat_short"), "kuayle_pat_short");
    }
}
