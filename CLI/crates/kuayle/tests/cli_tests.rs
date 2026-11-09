// CLI integration tests using assert_cmd + wiremock.
// 使用 assert_cmd + wiremock 的 CLI 集成测试。
//
// These tests run the actual kuayle binary against a wiremock server,
// with an isolated HOME to prevent interference with real config.
// 这些测试在隔离的 HOME 下对 wiremock 服务器运行真实的 kuayle 二进制文件。

use assert_cmd::Command;
use predicates::prelude::*;
use serde_json::json;
use std::fs;
use tempfile::TempDir;
use wiremock::matchers::{method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

/// Set up a fake HOME directory with a config directory.
/// 设置带配置目录的虚拟 HOME 目录。
fn setup_home() -> TempDir {
    let dir = TempDir::new().unwrap();
    let config_dir = dir.path().join(".config").join("kuayle");
    fs::create_dir_all(&config_dir).unwrap();
    dir
}

/// Build a kuayle Command pointed at a fake HOME.
/// 构建指向虚拟 HOME 的 kuayle Command。
fn kuayle_cmd(home: &TempDir, server_uri: &str) -> Command {
    let mut cmd = Command::cargo_bin("kuayle").unwrap();
    cmd.env("HOME", home.path())
        .env("XDG_CONFIG_HOME", home.path().join(".config"))
        .env("XDG_CACHE_HOME", home.path().join(".cache"))
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

// ── auth status ───────────────────────────────────────────────────

#[test]
fn auth_status_not_logged_in() {
    let home = setup_home();
    let mut cmd = Command::cargo_bin("kuayle").unwrap();
    cmd.env("HOME", home.path())
        .env("XDG_CONFIG_HOME", home.path().join(".config"))
        .env("XDG_CACHE_HOME", home.path().join(".cache"))
        .env("KUAYLE_CREDENTIAL_STORE", "file")
        .arg("auth")
        .arg("status")
        .arg("--format")
        .arg("human");

    cmd.assert()
        .failure()
        .code(predicate::eq(2))
        .stdout(predicate::str::contains("Not logged in"));
}

// ── auth login ────────────────────────────────────────────────────

#[tokio::test]
async fn auth_login_with_token_succeeds() {
    let home = setup_home();
    let server = MockServer::start().await;

    // Mock /api/auth/me to validate the token.
    // 模拟 /api/auth/me 验证 token。
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_test123"]);

    cmd.assert()
        .success()
        .stdout(predicate::str::contains("Logged in as Alice"));
}

#[tokio::test]
async fn auth_login_with_invalid_token_fails() {
    let home = setup_home();
    let server = MockServer::start().await;

    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(401).set_body_json(json!({
            "error": {
                "code": "UNAUTHORIZED",
                "message": "Authentication required"
            }
        })))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_bad_token"]);

    cmd.assert().failure().code(predicate::eq(2));
}

// ── auth status with session ──────────────────────────────────────

#[tokio::test]
async fn auth_status_after_login() {
    let home = setup_home();
    let server = MockServer::start().await;

    // Login first.
    // 先登录。
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_test123"]);
    cmd.assert().success();

    // Now check status — needs another mock for the status check.
    // 现在检查状态 — 需要另一个 mock 用于状态检查。
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "status"]);

    cmd.assert()
        .success()
        .stdout(predicate::str::contains("Personal Access Token"))
        .stdout(predicate::str::contains("authenticated as Alice"));
}

// ── auth logout ───────────────────────────────────────────────────

#[tokio::test]
async fn auth_logout_removes_session() {
    let home = setup_home();
    let server = MockServer::start().await;

    // Login.
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_test123"]);
    cmd.assert().success();

    // Logout.
    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "logout"]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("Logged out"));

    // Verify status shows not logged in.
    let mut cmd = Command::cargo_bin("kuayle").unwrap();
    cmd.env("HOME", home.path())
        .env("XDG_CONFIG_HOME", home.path().join(".config"))
        .env("XDG_CACHE_HOME", home.path().join(".cache"))
        .env("KUAYLE_CREDENTIAL_STORE", "file")
        .arg("auth")
        .arg("status");
    cmd.assert().failure().code(predicate::eq(2));
}

// ── whoami ────────────────────────────────────────────────────────

#[tokio::test]
async fn whoami_shows_user_info() {
    let home = setup_home();
    let server = MockServer::start().await;

    // Login first.
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_test123"]);
    cmd.assert().success();

    // whoami
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.arg("whoami");

    cmd.assert()
        .success()
        .stdout(predicate::str::contains("Alice Chen"))
        .stdout(predicate::str::contains("alice@kuayle.dev"));
}

#[tokio::test]
async fn whoami_not_logged_in_fails() {
    let home = setup_home();

    let mut cmd = Command::cargo_bin("kuayle").unwrap();
    cmd.env("HOME", home.path())
        .env("XDG_CONFIG_HOME", home.path().join(".config"))
        .env("XDG_CACHE_HOME", home.path().join(".cache"))
        .env("KUAYLE_CREDENTIAL_STORE", "file")
        .arg("whoami");

    cmd.assert().failure().code(predicate::eq(2));
}

// ── JSON output ───────────────────────────────────────────────────

#[tokio::test]
async fn whoami_json_output() {
    let home = setup_home();
    let server = MockServer::start().await;

    // Login.
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_test123"]);
    cmd.assert().success();

    // whoami --format json
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["whoami", "--format", "json"]);

    let output = cmd.assert().success();
    let stdout = String::from_utf8_lossy(&output.get_output().stdout);
    // Should be valid JSON.
    // 应为有效 JSON。
    let parsed: serde_json::Value = serde_json::from_str(&stdout).unwrap();
    assert_eq!(parsed["email"], "alice@kuayle.dev");
}

// ── workspaces list ────────────────────────────────────────────────

#[tokio::test]
async fn workspaces_list_human_output() {
    let home = setup_home();
    let server = MockServer::start().await;

    // Login.
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_test123"]);
    cmd.assert().success();

    // workspaces list
    Mock::given(method("GET"))
        .and(path("/api/workspaces"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!([
            {
                "id": "w1",
                "name": "Acme Corp",
                "slug": "acme",
                "logo_url": null,
                "owner_id": "u1",
                "owner": {
                    "id": "u1", "email": "a@b.com", "name": "Alice", "avatar_url": null
                },
                "share_link_min_role": "admin",
                "current_user_role": "owner",
                "created_at": "2026-07-28T15:58:38+08:00",
                "updated_at": "2026-07-28T15:58:38+08:00"
            }
        ])))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["workspaces", "list", "--format", "human"]);

    cmd.assert()
        .success()
        .stdout(predicate::str::contains("Acme Corp"))
        .stdout(predicate::str::contains("acme"))
        .stdout(predicate::str::contains("owner"))
        .stdout(predicate::str::contains("1 workspace"));
}

#[tokio::test]
async fn workspaces_list_json_output() {
    let home = setup_home();
    let server = MockServer::start().await;

    // Login.
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_test123"]);
    cmd.assert().success();

    Mock::given(method("GET"))
        .and(path("/api/workspaces"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!([
            {
                "id": "w1",
                "name": "Acme Corp",
                "slug": "acme",
                "logo_url": null,
                "owner_id": "u1",
                "owner": {
                    "id": "u1", "email": "a@b.com", "name": "Alice", "avatar_url": null
                },
                "share_link_min_role": "admin",
                "current_user_role": "owner",
                "created_at": "2026-07-28T15:58:38+08:00",
                "updated_at": "2026-07-28T15:58:38+08:00"
            }
        ])))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["workspaces", "list", "--format", "json"]);

    let output = cmd.assert().success();
    let stdout = String::from_utf8_lossy(&output.get_output().stdout);
    let parsed: Vec<serde_json::Value> = serde_json::from_str(&stdout).unwrap();
    assert_eq!(parsed.len(), 1);
    assert_eq!(parsed[0]["slug"], "acme");
}

#[tokio::test]
async fn workspaces_list_empty() {
    let home = setup_home();
    let server = MockServer::start().await;

    // Login.
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_test123"]);
    cmd.assert().success();

    Mock::given(method("GET"))
        .and(path("/api/workspaces"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!([])))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["workspaces", "list", "--format", "human"]);

    cmd.assert()
        .success()
        .stdout(predicate::str::contains("No workspaces found"));
}

// ── config.toml profile resolution ────────────────────────────────
// config.toml profile 解析

#[tokio::test]
async fn workspaces_list_uses_config_toml_url() {
    let home = setup_home();
    let server = MockServer::start().await;

    // Write config.toml with a profile pointing to the mock server.
    // 写入 config.toml，含指向 mock server 的 profile。
    let config_path = home
        .path()
        .join(".config")
        .join("kuayle")
        .join("config.toml");
    std::fs::create_dir_all(config_path.parent().unwrap()).unwrap();
    std::fs::write(
        &config_path,
        format!(
            r#"
default_profile = "test"

[profiles.test]
url = "{}"
workspace = "acme"
"#,
            server.uri()
        ),
    )
    .unwrap();

    // Login to store credentials.
    // 登录以存储凭据。
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_test123"]);
    cmd.assert().success();

    // workspaces list — WITHOUT KUAYLE_URL env, relying on config.toml.
    // workspaces list — 不设 KUAYLE_URL 环境变量，依赖 config.toml。
    Mock::given(method("GET"))
        .and(path("/api/workspaces"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!([
            {
                "id": "w1",
                "name": "Acme",
                "slug": "acme",
                "logo_url": null,
                "owner_id": "u1",
                "owner": {
                    "id": "u1", "email": "a@b.com", "name": "Alice", "avatar_url": null
                },
                "share_link_min_role": "admin",
                "current_user_role": "owner",
                "created_at": "2026-07-28T15:58:38+08:00",
                "updated_at": "2026-07-28T15:58:38+08:00"
            }
        ])))
        .expect(1)
        .mount(&server)
        .await;

    // No KUAYLE_URL; relies on config.toml profile.
    // 无 KUAYLE_URL；依赖 config.toml profile。
    let mut cmd = Command::cargo_bin("kuayle").unwrap();
    cmd.env("HOME", home.path())
        .env("XDG_CONFIG_HOME", home.path().join(".config"))
        .env("XDG_CACHE_HOME", home.path().join(".cache"))
        .env("KUAYLE_CREDENTIAL_STORE", "file")
        .args(["workspaces", "list", "--format", "human"]);

    cmd.assert()
        .success()
        .stdout(predicate::str::contains("Acme"))
        .stdout(predicate::str::contains("acme"));
}

// ── JSON error output ─────────────────────────────────────────────

#[tokio::test]
async fn json_error_output_on_auth_failure() {
    let home = setup_home();
    let server = MockServer::start().await;

    // Login first so there's a session, then whoami returns 401.
    // 先登录获取会话，然后 whoami 返回 401。
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_test123"]);
    cmd.assert().success();

    // Now whoami with --format json, but the API returns 401.
    // 现在 whoami --format json，但 API 返回 401。
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(401).set_body_json(json!({
            "error": {
                "code": "UNAUTHORIZED",
                "message": "bad token"
            }
        })))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = Command::cargo_bin("kuayle").unwrap();
    cmd.env("HOME", home.path())
        .env("XDG_CONFIG_HOME", home.path().join(".config"))
        .env("XDG_CACHE_HOME", home.path().join(".cache"))
        .env("KUAYLE_CREDENTIAL_STORE", "file")
        .env("KUAYLE_URL", server.uri())
        .args(["whoami", "--format", "json"]);

    let output = cmd.assert().failure().code(predicate::eq(2));
    let stdout = String::from_utf8_lossy(&output.get_output().stdout);
    let parsed: serde_json::Value = serde_json::from_str(&stdout).unwrap();
    assert_eq!(parsed["error"]["kind"], "authentication");
    assert!(parsed["error"]["message"]
        .as_str()
        .unwrap()
        .contains("bad token"));
}

// ── Cache hit ≤2 round trips ──────────────────────────────────────

#[tokio::test]
async fn issue_create_with_cached_names_makes_minimal_requests() {
    let home = setup_home();
    let server = MockServer::start().await;

    // Login.
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_test123"]);
    cmd.assert().success();

    // Mock teams list (resolved from "Engineering").
    Mock::given(method("GET"))
        .and(path("/api/workspaces/acme/teams"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!([
            {"id": "c0", "name": "Engineering", "key": "ENG"}
        ])))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    // Mock members list (resolved from "Alice Chen").
    Mock::given(method("GET"))
        .and(path("/api/workspaces/acme/members"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!([
            {"user_id": "a0", "name": "Alice Chen", "email": "a@b.com", "role": "owner"}
        ])))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    // Mock labels list (resolved from "Bug").
    Mock::given(method("GET"))
        .and(path("/api/workspaces/acme/labels"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!([
            {"id": "d0", "name": "Bug", "color": "#f00"}
        ])))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    // Create mock — both invocations hit this (second uses cache for resolves).
    // 创建 mock — 两次调用都命中此 mock（第二次通过缓存解析名称）。
    Mock::given(method("POST"))
        .and(path("/api/workspaces/acme/issues"))
        .respond_with(ResponseTemplate::new(201).set_body_json(json!({
            "id": "i1", "identifier": "ENG-1", "title": "test",
            "status": "backlog", "status_id": "s0", "priority": 3,
            "team_id": "c0", "creator_id": "u0", "sort_order": 0,
            "labels": [], "assignees": [], "is_subscribed": false,
            "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"
        })))
        .expect(2) // both creates / 两次创建
        .mount(&server)
        .await;

    // First create — resolves names (teams + members + labels = 3 resolve requests).
    // 第一次创建 — 解析名称（teams + members + labels = 3 个解析请求）。
    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args([
        "issues",
        "create",
        "--workspace",
        "acme",
        "--title",
        "test",
        "--team",
        "Engineering",
        "--assignee",
        "Alice Chen",
        "--labels",
        "Bug",
        "--priority",
        "3",
    ]);
    cmd.assert().success();

    // Second create — with disk cache, only POST create (0 extra resolve requests).
    // Wiremock total: 1 auth + 3 resolves + 2 creates = 6.
    // 第二次创建 — 磁盘缓存命中，仅 POST create（0 个额外解析请求）。
    // Wiremock 总计：1 auth + 3 resolves + 2 creates = 6。
    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args([
        "issues",
        "create",
        "--workspace",
        "acme",
        "--title",
        "test2",
        "--team",
        "Engineering",
        "--assignee",
        "Alice Chen",
        "--labels",
        "Bug",
        "--priority",
        "3",
    ]);
    cmd.assert().success();

    // Verification: total requests = 1(auth) + 1(teams) + 1(members) + 1(labels) + 2(creates) = 6.
    // The second invocation made exactly 1 network request (only POST create).
    // 验证：总请求 = 1(auth) + 1(teams) + 1(members) + 1(labels) + 2(creates) = 6。
    // 第二次调用仅发起 1 个网络请求（仅 POST create）。
}

// ── Relations delete correct path ─────────────────────────────────
// 关系删除的正确路径

#[tokio::test]
async fn relations_delete_uses_correct_path() {
    let home = setup_home();
    let server = MockServer::start().await;

    // Login.
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["auth", "login", "--token", "kuayle_pat_test123"]);
    cmd.assert().success();

    // Mock the correct DELETE path: /api/workspaces/acme/issues/ENG-1/relations/r1
    Mock::given(method("DELETE"))
        .and(path("/api/workspaces/acme/issues/ENG-1/relations/r1"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({"deleted": true})))
        .expect(1)
        .mount(&server)
        .await;

    let mut cmd = kuayle_cmd(&home, &server.uri());
    cmd.args(["relations", "delete", "ENG-1", "r1", "--format", "human"]);

    cmd.assert().success();
}
