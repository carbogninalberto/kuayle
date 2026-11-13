// `kuayle auth connect` against wiremock.
// 针对 wiremock 的 `kuayle auth connect` 测试。

use assert_cmd::Command;
use predicates::prelude::*;
use serde_json::json;
use std::fs;
use tempfile::TempDir;
use wiremock::matchers::{body_json, method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

fn setup_home() -> TempDir {
    let dir = TempDir::new().unwrap();
    fs::create_dir_all(dir.path().join(".config").join("kuayle")).unwrap();
    dir
}

fn kuayle_cmd(home: &TempDir, server_uri: &str) -> Command {
    let mut cmd = Command::cargo_bin("kuayle").unwrap();
    cmd.env("HOME", home.path())
        .env("XDG_CONFIG_HOME", home.path().join(".config"))
        .env("XDG_CACHE_HOME", home.path().join(".cache"))
        .env(
            "KUAYLE_CONFIG_DIR",
            home.path().join(".config").join("kuayle"),
        )
        .env(
            "KUAYLE_CACHE_DIR",
            home.path().join(".cache").join("kuayle"),
        )
        .env("KUAYLE_URL", server_uri)
        .env("KUAYLE_CREDENTIAL_STORE", "file");
    cmd
}

fn user_json() -> serde_json::Value {
    json!({
        "id": "a0000000-0000-0000-0000-000000000001",
        "email": "alice@kuayle.dev",
        "name": "Alice Chen",
        "display_name": "Alice",
        "avatar_url": null,
        "is_sysadmin": true
    })
}

fn link(server_uri: &str, workspace: Option<&str>) -> String {
    let code = format!("kuayle_cl_{}", "a".repeat(40));
    match workspace {
        Some(workspace) => format!("{server_uri}/cli/connect#code={code}&workspace={workspace}"),
        None => format!("{server_uri}/cli/connect#code={code}"),
    }
}

fn credentials_path(home: &TempDir) -> std::path::PathBuf {
    home.path()
        .join(".config")
        .join("kuayle")
        .join("credentials")
        .join("default.json")
}

fn config_path(home: &TempDir) -> std::path::PathBuf {
    home.path()
        .join(".config")
        .join("kuayle")
        .join("config.toml")
}

#[tokio::test]
async fn connect_configures_the_profile_from_a_link() {
    let home = setup_home();
    let server = MockServer::start().await;
    let link = link(&server.uri(), Some("acme"));
    let code = format!("kuayle_cl_{}", "a".repeat(40));

    Mock::given(method("POST"))
        .and(path("/api/cli/connect"))
        .and(body_json(
            json!({"code": code, "client_name": "kuayle-cli"}),
        ))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "token": "kuayle_pat_newtoken123",
            "token_prefix": "kuayle_pat_newt",
            "expires_at": null,
            "scopes": ["issues:read"],
            "workspace_slugs": ["acme"]
        })))
        .expect(1)
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "connect", &link, "--format", "human"]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("Connected as Alice"))
        .stdout(predicate::str::contains("kuayle_pat_").not())
        .stderr(predicate::str::contains("kuayle_pat_").not());

    let credentials = fs::read_to_string(credentials_path(&home)).unwrap();
    assert!(credentials.contains("kuayle_pat_newtoken123"));
    let config = fs::read_to_string(config_path(&home)).unwrap();
    assert!(config.contains(&server.uri()));
    assert!(config.contains("acme"));
}

#[tokio::test]
async fn connect_reads_the_link_from_stdin() {
    let home = setup_home();
    let server = MockServer::start().await;
    let link = link(&server.uri(), None);

    Mock::given(method("POST"))
        .and(path("/api/cli/connect"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "token": "kuayle_pat_newtoken123"
        })))
        .expect(1)
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "connect", "--format", "human"])
        .write_stdin(format!("{link}\n"));
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("Connected as Alice"));
}

#[tokio::test]
async fn expired_or_used_links_create_no_credentials() {
    for (status, code) in [
        (410, "LINK_EXPIRED"),
        (410, "LINK_USED"),
        (410, "LINK_REVOKED"),
    ] {
        let home = setup_home();
        let server = MockServer::start().await;
        let link = link(&server.uri(), None);

        Mock::given(method("POST"))
            .and(path("/api/cli/connect"))
            .respond_with(ResponseTemplate::new(status).set_body_json(json!({
                "error": {"code": code, "message": "This connection link cannot be used."}
            })))
            .expect(1)
            .mount(&server)
            .await;

        let mut cmd = kuayle_cmd(&home, &server.uri());
        cmd.args(["auth", "connect", &link]);
        cmd.assert()
            .failure()
            .stdout(predicate::str::contains("kuayle_cl_").not())
            .stderr(predicate::str::contains("kuayle_cl_").not());

        assert!(
            !credentials_path(&home).exists(),
            "{code}: credentials were written"
        );
        assert!(!config_path(&home).exists(), "{code}: config was written");
    }
}

#[tokio::test]
async fn connect_rolls_back_when_the_config_cannot_be_written() {
    let home = setup_home();
    let server = MockServer::start().await;
    let link = link(&server.uri(), Some("acme"));

    // A directory where config.toml should go makes the config write fail
    // after the credentials were saved.
    fs::create_dir_all(config_path(&home)).unwrap();

    Mock::given(method("POST"))
        .and(path("/api/cli/connect"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "token": "kuayle_pat_newtoken123"
        })))
        .expect(1)
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .expect(1)
        .mount(&server)
        .await;
    Mock::given(method("DELETE"))
        .and(path("/api/tokens/current"))
        .respond_with(ResponseTemplate::new(204))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "connect", &link]);
    cmd.assert()
        .failure()
        .stdout(predicate::str::contains("kuayle_pat_").not())
        .stderr(predicate::str::contains("kuayle_pat_").not());

    assert!(
        !credentials_path(&home).exists(),
        "failed setup left the minted credentials behind"
    );
}

#[tokio::test]
async fn connect_requires_force_to_replace_existing_credentials() {
    let home = setup_home();
    let server = MockServer::start().await;
    let link = link(&server.uri(), None);

    fs::create_dir_all(credentials_path(&home).parent().unwrap()).unwrap();
    fs::write(
        credentials_path(&home),
        r#"{"Pat":{"token":"kuayle_pat_existing"}}"#,
    )
    .unwrap();

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "connect", &link, "--format", "human"]);
    cmd.assert()
        .failure()
        .code(predicate::eq(4))
        .stderr(predicate::str::contains("--force"));

    let saved = fs::read_to_string(credentials_path(&home)).unwrap();
    assert!(saved.contains("kuayle_pat_existing"));
}

#[tokio::test]
async fn login_rolls_back_credentials_when_the_config_cannot_be_written() {
    let home = setup_home();
    let server = MockServer::start().await;

    fs::create_dir_all(config_path(&home)).unwrap();
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_test123"]);
    cmd.assert()
        .failure()
        .stdout(predicate::str::contains("kuayle_pat_test123").not())
        .stderr(predicate::str::contains("kuayle_pat_test123").not());

    assert!(
        !credentials_path(&home).exists(),
        "failed login left credentials behind"
    );
}

#[tokio::test]
async fn logout_revokes_the_current_token() {
    let home = setup_home();
    let server = MockServer::start().await;

    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .expect(1)
        .mount(&server)
        .await;
    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_test123"]);
    cmd.assert().success();

    Mock::given(method("DELETE"))
        .and(path("/api/tokens/current"))
        .respond_with(ResponseTemplate::new(204))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "logout", "--format", "human"]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("Logged out"));
    assert!(!credentials_path(&home).exists());
}
