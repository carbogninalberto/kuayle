// Wiki and registry commands, run as the real binary against wiremock.

use assert_cmd::Command;
use predicates::prelude::*;
use serde_json::{json, Value};
use std::fs;
use tempfile::TempDir;
use wiremock::matchers::{body_json, method, path, query_param};
use wiremock::{Mock, MockServer, Request, ResponseTemplate};

const TEAM: &str = "11111111-1111-1111-1111-111111111111";
const PAGE: &str = "22222222-2222-2222-2222-222222222222";

fn wiki_base() -> String {
    format!("/api/workspaces/acme/teams/{TEAM}/wiki")
}

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
        .env("KUAYLE_CREDENTIAL_STORE", "file")
        .env("KUAYLE_NO_CACHE", "1");
    cmd
}

/// A logged-in home plus a server that resolves team "ENG".
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
    Mock::given(method("GET"))
        .and(path("/api/workspaces/acme/teams"))
        .respond_with(
            ResponseTemplate::new(200)
                .set_body_json(json!([{"id": TEAM, "name": "Engineering", "key": "ENG"}])),
        )
        .mount(&server)
        .await;
    kuayle(&home, &server)
        .args(["auth", "login", "--token", "kuayle_pat_test"])
        .assert()
        .success();
    (home, server)
}

fn page_json(version: i64, title: &str, content: &str, registry: Option<&str>) -> Value {
    json!({
        "id": PAGE, "team_id": TEAM, "project_id": null, "title": title,
        "version": version, "updated_by": null, "updated_by_name": "Ada",
        "registry_date": registry, "created_at": "2026-11-01T10:00:00Z",
        "updated_at": "2026-11-02T10:00:00Z", "content": content, "created_by": null
    })
}

#[tokio::test]
async fn list_resolves_team_key_and_hides_registry_days() {
    let (home, server) = logged_in().await;
    Mock::given(method("GET"))
        .and(path(wiki_base()))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!([
            page_json(1, "Runbook", "", None),
            page_json(1, "2026-11-02", "", Some("2026-11-02")),
        ])))
        .expect(2)
        .mount(&server)
        .await;

    let out = kuayle(&home, &server)
        .args(["wiki", "list", "--team", "ENG", "--format", "json"])
        .assert()
        .success();
    let pages: Vec<Value> = serde_json::from_slice(&out.get_output().stdout).unwrap();
    assert_eq!(pages.len(), 1);
    assert_eq!(pages[0]["title"], "Runbook");

    let out = kuayle(&home, &server)
        .args([
            "wiki",
            "list",
            "--team",
            "eng",
            "--registry",
            "--format",
            "json",
        ])
        .assert()
        .success();
    let pages: Vec<Value> = serde_json::from_slice(&out.get_output().stdout).unwrap();
    assert_eq!(pages.len(), 2);
}

#[tokio::test]
async fn update_without_version_uses_the_current_version() {
    let (home, server) = logged_in().await;
    let file = home.path().join("page.md");
    fs::write(&file, "# New body").unwrap();
    Mock::given(method("GET"))
        .and(path(format!("{}/{PAGE}", wiki_base())))
        .respond_with(
            ResponseTemplate::new(200).set_body_json(page_json(7, "Runbook", "old", None)),
        )
        .expect(1)
        .mount(&server)
        .await;
    Mock::given(method("PATCH"))
        .and(path(format!("{}/{PAGE}", wiki_base())))
        .and(body_json(json!({"content": "# New body", "version": 7})))
        .respond_with(ResponseTemplate::new(200).set_body_json(page_json(
            8,
            "Runbook",
            "<h1>New body</h1>",
            None,
        )))
        .expect(1)
        .mount(&server)
        .await;

    kuayle(&home, &server)
        .args(["wiki", "update", PAGE, "--team", "ENG", "--file"])
        .arg(&file)
        .args(["--format", "human"])
        .assert()
        .success()
        .stdout(predicate::str::contains("version 8"));
}

#[tokio::test]
async fn stale_version_conflict_reports_the_server_code() {
    let (home, server) = logged_in().await;
    Mock::given(method("PATCH"))
        .and(path(format!("{}/{PAGE}", wiki_base())))
        .respond_with(ResponseTemplate::new(409).set_body_json(json!({
            "error": {"code": "WIKI_VERSION_CONFLICT", "message": "This page was changed by someone else."}
        })))
        .mount(&server)
        .await;

    kuayle(&home, &server)
        .args([
            "wiki",
            "update",
            PAGE,
            "--team",
            "ENG",
            "--title",
            "X",
            "--version",
            "3",
        ])
        .args(["--format", "json"])
        .assert()
        .failure()
        .code(1)
        .stdout(predicate::str::contains("WIKI_VERSION_CONFLICT"));
}

#[tokio::test]
async fn restore_writes_the_revision_at_the_current_version() {
    let (home, server) = logged_in().await;
    Mock::given(method("GET"))
        .and(path(format!("{}/{PAGE}/versions", wiki_base())))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!([
            {"id": "r3", "version": 3, "old_title": "B", "new_title": "C", "old_content": "b", "new_content": "c",
             "edited_by": null, "edited_by_name": "Ada", "created_at": "2026-11-02T10:00:00Z", "updated_at": "2026-11-02T10:00:00Z"},
            {"id": "r2", "version": 2, "old_title": "A", "new_title": "B", "old_content": "a", "new_content": "b",
             "edited_by": null, "edited_by_name": "Ada", "created_at": "2026-11-01T10:00:00Z", "updated_at": "2026-11-01T10:00:00Z"}
        ])))
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path(format!("{}/{PAGE}", wiki_base())))
        .respond_with(ResponseTemplate::new(200).set_body_json(page_json(3, "C", "c", None)))
        .mount(&server)
        .await;
    Mock::given(method("PATCH"))
        .and(path(format!("{}/{PAGE}", wiki_base())))
        .and(body_json(
            json!({"title": "A", "content": "a", "version": 3}),
        ))
        .respond_with(ResponseTemplate::new(200).set_body_json(page_json(4, "A", "a", None)))
        .expect(1)
        .mount(&server)
        .await;

    kuayle(&home, &server)
        .args([
            "wiki",
            "restore",
            PAGE,
            "--team",
            "ENG",
            "--version",
            "2",
            "--before",
        ])
        .args(["--format", "human"])
        .assert()
        .success()
        .stdout(predicate::str::contains("now version 4"));

    // A version outside the history is a not-found error, and nothing is written.
    kuayle(&home, &server)
        .args(["wiki", "restore", PAGE, "--team", "ENG", "--version", "9"])
        .args(["--format", "json"])
        .assert()
        .failure()
        .code(3);
}

#[tokio::test]
async fn create_in_a_folder_uses_the_tree_endpoint() {
    let (home, server) = logged_in().await;
    Mock::given(method("POST"))
        .and(path(format!("{}/tree", wiki_base())))
        .and(body_json(
            json!({"title": "Child", "content": "hi", "kind": "page", "parent_id": "f1"}),
        ))
        .respond_with(ResponseTemplate::new(201).set_body_json(page_json(
            1,
            "Child",
            "<p>hi</p>",
            None,
        )))
        .expect(1)
        .mount(&server)
        .await;

    kuayle(&home, &server)
        .args([
            "wiki",
            "create",
            "--team",
            "ENG",
            "--title",
            "Child",
            "--content",
            "hi",
            "--parent",
            "f1",
        ])
        .args(["--format", "json"])
        .assert()
        .success();
}

#[tokio::test]
async fn registry_append_and_feed() {
    let (home, server) = logged_in().await;
    Mock::given(method("POST"))
        .and(path(format!("{}/registry", wiki_base())))
        .and(body_json(json!({"text": "Deployed v1.2"})))
        .respond_with(ResponseTemplate::new(200).set_body_json(page_json(
            2,
            "2026-11-09",
            "<p>…</p>",
            Some("2026-11-09"),
        )))
        .expect(1)
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path(format!("{}/registry", wiki_base())))
        .and(query_param("before", "2026-11-09"))
        .and(query_param("limit", "3"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "days": [{"date": "2026-11-08", "page_id": PAGE, "title": "2026-11-08",
                      "content": "<p><strong>09:00</strong> standup</p>", "version": 1,
                      "updated_at": "2026-11-08T09:00:00Z"}],
            "next_before": "2026-11-08"
        })))
        .expect(1)
        .mount(&server)
        .await;

    kuayle(&home, &server)
        .args([
            "registry",
            "append",
            "--team",
            "ENG",
            "Deployed v1.2",
            "--format",
            "human",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("Logged to 2026-11-09"));
    kuayle(&home, &server)
        .args([
            "registry",
            "feed",
            "--team",
            "ENG",
            "--before",
            "2026-11-09",
            "--limit",
            "3",
        ])
        .args(["--format", "human"])
        .assert()
        .success()
        .stdout(
            predicate::str::contains("standup")
                .and(predicate::str::contains("--before 2026-11-08")),
        );
}

#[tokio::test]
async fn project_scope_resolves_project_names() {
    let (home, server) = logged_in().await;
    let project = "33333333-3333-3333-3333-333333333333";
    Mock::given(method("GET"))
        .and(path("/api/workspaces/acme/projects"))
        .respond_with(
            ResponseTemplate::new(200).set_body_json(json!([{"id": project, "name": "Launch"}])),
        )
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path(format!(
            "/api/workspaces/acme/projects/{project}/wiki/registry/overviews"
        )))
        .respond_with(move |req: &Request| {
            assert!(req.url.query().is_none());
            ResponseTemplate::new(200).set_body_json(json!({"overviews": [], "next_before": null}))
        })
        .expect(1)
        .mount(&server)
        .await;

    kuayle(&home, &server)
        .args([
            "registry",
            "overview",
            "--project",
            "launch",
            "--format",
            "human",
        ])
        .assert()
        .success()
        .stdout(predicate::str::contains("No overviews."));
}

#[test]
fn wiki_requires_exactly_one_scope() {
    let home = TempDir::new().unwrap();
    let mut cmd = Command::cargo_bin("kuayle").unwrap();
    cmd.env("HOME", home.path())
        .args(["wiki", "list"])
        .assert()
        .failure()
        .stderr(predicate::str::contains("--team"));
    let mut cmd = Command::cargo_bin("kuayle").unwrap();
    cmd.env("HOME", home.path())
        .args(["wiki", "list", "--team", "A", "--project", "B"])
        .assert()
        .failure()
        .stderr(predicate::str::contains("cannot be used with"));
}
