// agent-tasks and workflows commands against wiremock.

use assert_cmd::Command;
use predicates::prelude::*;
use serde_json::json;
use std::fs;
use tempfile::TempDir;
use wiremock::matchers::{method, path, query_param};
use wiremock::{Mock, MockServer, ResponseTemplate};

fn kuayle(home: &TempDir, server: &MockServer) -> Command {
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
        .env("KUAYLE_URL", server.uri())
        .env("KUAYLE_WORKSPACE", "acme")
        .env("KUAYLE_CREDENTIAL_STORE", "file");
    cmd
}

async fn logged_in() -> (TempDir, MockServer) {
    let home = TempDir::new().unwrap();
    fs::create_dir_all(home.path().join(".config/kuayle")).unwrap();
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "id": "a0000000-0000-0000-0000-000000000001", "email": "a@kuayle.dev",
            "name": "Ada", "display_name": "Ada", "avatar_url": null, "is_sysadmin": false
        })))
        .mount(&server)
        .await;
    kuayle(&home, &server)
        .args(["auth", "login", "--token", "kuayle_pat_test"])
        .assert()
        .success();
    (home, server)
}

#[tokio::test]
async fn agent_task_queue_and_history() {
    let (home, server) = logged_in().await;
    Mock::given(method("GET"))
        .and(path("/api/workspaces/acme/agent-tasks"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!([{
            "id": "t1", "conversation_id": "c1", "state": "running", "reason": "",
            "issue_identifier": "ENG-7", "issue_title": "Fix login", "provider": "claude-code",
            "updated_at": "2026-11-09T10:00:00Z"
        }])))
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/workspaces/acme/agent-tasks/t1/history"))
        .and(query_param("after", "4"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "transitions": [{"sequence": 5, "task_id": "t1", "from_state": "queued",
                             "to_state": "running", "actor_id": null, "reason": "started",
                             "created_at": "2026-11-09T10:00:00Z"}]
        })))
        .expect(1)
        .mount(&server)
        .await;

    kuayle(&home, &server)
        .args(["agent-tasks", "list", "--format", "human"])
        .assert()
        .success()
        .stdout(predicate::str::contains("ENG-7").and(predicate::str::contains("running")));
    kuayle(&home, &server)
        .args([
            "agent-tasks",
            "history",
            "t1",
            "--after",
            "4",
            "--format",
            "human",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("queued").and(predicate::str::contains("started")));
}

#[tokio::test]
async fn workflows_list_unwraps_the_response() {
    let (home, server) = logged_in().await;
    Mock::given(method("GET"))
        .and(path("/api/workspaces/acme/workflows"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "workflows": [{"id": "standard", "name": "Standard", "description": "",
                           "builtin": true, "customized": false, "latest_version": 0,
                           "archived": false, "graph": {}}],
            "defaults": [], "can_manage": false
        })))
        .mount(&server)
        .await;

    kuayle(&home, &server)
        .args(["workflows", "list", "--format", "human"])
        .assert()
        .success()
        .stdout(predicate::str::contains("Standard"));
}

#[tokio::test]
async fn missing_scope_exits_forbidden() {
    let (home, server) = logged_in().await;
    Mock::given(method("GET"))
        .and(path("/api/workspaces/acme/agent-profiles"))
        .respond_with(ResponseTemplate::new(403).set_body_json(json!({
            "error": {"code": "FORBIDDEN", "message": "You do not have permission to perform this action"}
        })))
        .mount(&server)
        .await;

    kuayle(&home, &server)
        .args(["workflows", "profiles", "--format", "json"])
        .assert()
        .failure()
        .code(5)
        .stdout(predicate::str::contains("forbidden"));
}
