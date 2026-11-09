// Contract tests: run the real binary against a live Kuayle server.
//
// Skipped unless these are set (see docs/cli.md#develop):
//   KUAYLE_CONTRACT_URL        e.g. http://localhost:8080
//   KUAYLE_CONTRACT_TOKEN      a PAT with the Developer preset plus issue:delete_own
//                              (Developer includes agent_tasks:read and workflows:read)
//   KUAYLE_CONTRACT_WORKSPACE  a workspace slug with at least one team and project
// Optional:
//   KUAYLE_CONTRACT_READONLY_TOKEN  a PAT without wiki:write, agent_tasks:read or
//                                   workflows:read, to check denials
//
// The tests create and then delete their own issue, label, template and wiki
// pages. Registry entries cannot be deleted through the API, so use a
// disposable server (for example a seeded `make dev` database).

use assert_cmd::Command;
use serde_json::Value;
use std::cell::RefCell;
use tempfile::TempDir;

struct Live {
    url: String,
    workspace: String,
    home: TempDir,
    failures: RefCell<Vec<String>>,
}

struct Out {
    code: i32,
    json: Value,
    stdout: String,
    stderr: String,
}

impl Live {
    fn from_env() -> Option<(Live, String)> {
        let url = std::env::var("KUAYLE_CONTRACT_URL").ok()?;
        let token = std::env::var("KUAYLE_CONTRACT_TOKEN").ok()?;
        let workspace = std::env::var("KUAYLE_CONTRACT_WORKSPACE").ok()?;
        let live = Live {
            url,
            workspace,
            home: TempDir::new().unwrap(),
            failures: RefCell::new(vec![]),
        };
        Some((live, token))
    }

    fn cmd(&self) -> Command {
        let mut cmd = Command::cargo_bin("kuayle").unwrap();
        cmd.env("HOME", self.home.path())
            .env("XDG_CONFIG_HOME", self.home.path().join(".config"))
            .env("XDG_CACHE_HOME", self.home.path().join(".cache"))
            .env("KUAYLE_URL", &self.url)
            .env("KUAYLE_WORKSPACE", &self.workspace)
            .env("KUAYLE_CREDENTIAL_STORE", "file")
            .env("KUAYLE_NO_CACHE", "1");
        cmd
    }

    fn run(&self, args: &[&str]) -> Out {
        let output = self
            .cmd()
            .args(args)
            .args(["--format", "json"])
            .output()
            .unwrap();
        let stdout = String::from_utf8_lossy(&output.stdout).to_string();
        Out {
            code: output.status.code().unwrap_or(-1),
            json: serde_json::from_str(&stdout).unwrap_or(Value::Null),
            stdout,
            stderr: String::from_utf8_lossy(&output.stderr).to_string(),
        }
    }

    /// Run a command that must succeed with JSON output; record a failure
    /// (instead of panicking) so one run reports every mismatch.
    fn ok(&self, args: &[&str]) -> Value {
        let out = self.run(args);
        if out.code != 0 || out.json.is_null() {
            self.failures.borrow_mut().push(format!(
                "kuayle {}: exit {} stdout={} stderr={}",
                args.join(" "),
                out.code,
                out.stdout.trim(),
                out.stderr.trim()
            ));
        }
        out.json
    }

    fn expect_code(&self, args: &[&str], code: i32) {
        let out = self.run(args);
        if out.code != code {
            self.failures.borrow_mut().push(format!(
                "kuayle {}: expected exit {code}, got {} stdout={}",
                args.join(" "),
                out.code,
                out.stdout.trim()
            ));
        }
    }

    fn finish(self) {
        let failures = self.failures.into_inner();
        assert!(
            failures.is_empty(),
            "{} contract failure(s):\n{}",
            failures.len(),
            failures.join("\n")
        );
    }
}

fn s(v: &Value, key: &str) -> String {
    v[key].as_str().unwrap_or_default().to_string()
}

#[test]
fn every_command_matches_the_live_api() {
    let Some((live, token)) = Live::from_env() else {
        println!(
            "SKIPPED: set KUAYLE_CONTRACT_URL, KUAYLE_CONTRACT_TOKEN and KUAYLE_CONTRACT_WORKSPACE"
        );
        return;
    };
    live.cmd()
        .args(["auth", "login", "--token", &token])
        .assert()
        .success();

    // ── Reads ─────────────────────────────────────────────────────
    live.ok(&["whoami"]);
    live.ok(&["auth", "status"]);
    live.ok(&["workspaces", "list"]);
    let teams = live.ok(&["teams", "list"]);
    let team = teams.get(0).cloned().unwrap_or(Value::Null);
    let team_key = s(&team, "key");
    let team_id = s(&team, "id");
    assert!(!team_id.is_empty(), "workspace needs a team");
    live.ok(&["teams", "read", &team_id]);
    live.ok(&["statuses", "list", &team_id]);
    let projects = live.ok(&["projects", "list"]);
    let project = projects.get(0).cloned().unwrap_or(Value::Null);
    let project_name = s(&project, "name");
    live.ok(&["projects", "read", &s(&project, "id")]);
    let cycles = live.ok(&["cycles", "list", "--team", &team_id]);
    if let Some(cycle) = cycles.get(0) {
        let id = cycle["id"].as_str().unwrap_or_default().to_string();
        live.ok(&["cycles", "read", "--team", &team_id, &id]);
        live.ok(&["cycles", "burndown", "--team", &team_id, &id]);
    }
    // Velocity is JSON null until a cycle has completed.
    live.expect_code(&["cycles", "velocity", "--team", &team_id], 0);
    live.ok(&["labels", "list"]);
    live.ok(&["members", "list"]);
    live.ok(&["templates", "list"]);
    let views = live.ok(&["views", "list"]);
    if let Some(view) = views.get(0) {
        live.ok(&["views", "read", view["id"].as_str().unwrap_or_default()]);
    }
    live.ok(&["favorites", "list"]);
    // Assets: upload a 1x1 PNG and download it again.
    let png: &[u8] = &[
        137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6,
        0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 13, 73, 68, 65, 84, 120, 218, 99, 100, 248, 207, 80,
        15, 0, 3, 134, 1, 128, 90, 52, 125, 107, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130,
    ];
    let src = live.home.path().join("pixel.png");
    std::fs::write(&src, png).unwrap();
    let uploaded = live.ok(&["assets", "upload", src.to_str().unwrap()]);
    let asset_id = s(&uploaded, "asset_id");
    if !asset_id.is_empty() {
        let dest = live.home.path().join("back.png");
        live.ok(&["assets", "download", &asset_id, dest.to_str().unwrap()]);
        if std::fs::read(&dest).ok().as_deref() != Some(png) {
            live.failures
                .borrow_mut()
                .push("assets download: bytes differ from the upload".into());
        }
    }
    live.ok(&["notifications", "list"]);
    let issues = live.ok(&["issues", "list"]);
    if let Some(issue) = issues.get(0) {
        let ident = s(issue, "identifier");
        live.ok(&["issues", "read", &ident]);
        live.ok(&["issues", "history", &ident]);
        live.ok(&["issues", "sub-issues-list", &ident]);
        live.ok(&["comments", "list", &ident]);
        live.ok(&["relations", "list", &ident]);
    }
    live.ok(&["issues", "list", "--team", &team_key, "--assignee", "me"]);
    live.expect_code(&["usage"], 0);

    // ── Agent tasks and workflows (read-only) ─────────────────────
    let queue = live.ok(&["agent-tasks", "list"]);
    if let Some(task) = queue.get(0) {
        let id = s(task, "id");
        live.ok(&["agent-tasks", "read", &id]);
        live.ok(&["agent-tasks", "history", &id]);
        live.ok(&["agent-tasks", "runs", &id]);
    }
    let workflows = live.ok(&["workflows", "list"]);
    if let Some(workflow) = workflows["workflows"].get(0) {
        live.ok(&["workflows", "read", &s(workflow, "id")]);
    } else {
        live.failures
            .borrow_mut()
            .push(format!("workflows list has no workflows: {workflows}"));
    }
    live.ok(&["workflows", "profiles"]);

    // ── Issue writes ──────────────────────────────────────────────
    let created = live.ok(&[
        "issues",
        "create",
        "--title",
        "CLI contract test",
        "--team",
        &team_key,
        "--priority",
        "3",
    ]);
    let ident = s(&created, "identifier");
    if !ident.is_empty() {
        live.ok(&[
            "issues",
            "update",
            &ident,
            "--title",
            "CLI contract test (edited)",
        ]);
        let comment = live.ok(&["comments", "create", &ident, "--body", "contract comment"]);
        let _ = comment;
        let child = live.ok(&["issues", "sub-issues-create", &ident, "--title", "child"]);
        let child_ident = s(&child, "identifier");
        live.ok(&["issues", "batch-update", &ident, "--priority", "2"]);
        let updated = live.ok(&["issues", "read", &ident]);
        if updated["priority"] != 2 {
            live.failures
                .borrow_mut()
                .push(format!("batch-update priority: {}", updated["priority"]));
        }
        if !child_ident.is_empty() {
            live.ok(&["issues", "batch-delete", &child_ident]);
            live.expect_code(&["issues", "read", &child_ident], 3);
        }
        live.ok(&["issues", "delete", &ident]);
    }

    // ── Labels and templates ──────────────────────────────────────
    let label = live.ok(&[
        "labels",
        "create",
        "--name",
        "cli-contract",
        "--color",
        "#6650eb",
    ]);
    let label_id = s(&label, "id");
    if !label_id.is_empty() {
        live.ok(&["labels", "update", &label_id, "--name", "cli-contract-2"]);
        live.ok(&["labels", "delete", &label_id]);
    }
    let template = live.ok(&[
        "templates",
        "create",
        "--name",
        "cli-contract",
        "--title",
        "T",
    ]);
    let template_id = s(&template, "id");
    if !template_id.is_empty() {
        live.ok(&["templates", "read", &template_id]);
        live.ok(&["templates", "update", &template_id, "--title", "T2"]);
        live.ok(&["templates", "delete", &template_id]);
    }

    // ── Wiki ──────────────────────────────────────────────────────
    let scope = ["--team", team_key.as_str()];
    let with = |args: &[&str]| -> Vec<String> {
        args.iter()
            .chain(scope.iter())
            .map(|a| a.to_string())
            .collect()
    };
    let run_with = |args: &[&str]| {
        let v = with(args);
        live.ok(&v.iter().map(String::as_str).collect::<Vec<_>>())
    };
    run_with(&["wiki", "list"]);
    run_with(&["wiki", "tree"]);
    let folder = run_with(&["wiki", "create", "--title", "CLI folder", "--folder"]);
    let page = run_with(&[
        "wiki",
        "create",
        "--title",
        "CLI page",
        "--content",
        "# Hello\n\nFrom the **CLI**.",
    ]);
    let page_id = s(&page, "id");
    let folder_id = s(&folder, "id");
    if !page_id.is_empty() {
        let read = run_with(&["wiki", "read", &page_id]);
        assert!(
            s(&read, "content").contains("<strong>CLI</strong>"),
            "Markdown is converted to HTML: {read}"
        );
        let v1 = read["version"].as_i64().unwrap_or(0);
        run_with(&["wiki", "update", &page_id, "--title", "CLI page v2"]);
        // A stale version is a conflict, never an overwrite.
        let stale = with(&["wiki", "update", &page_id, "--title", "stale", "--version"]);
        let mut stale: Vec<&str> = stale.iter().map(String::as_str).collect();
        let v1s = v1.to_string();
        stale.insert(stale.len() - 2, &v1s);
        let out = live.run(&stale);
        if out.code == 0 || !out.stdout.contains("WIKI_VERSION_CONFLICT") {
            live.failures.borrow_mut().push(format!(
                "stale wiki update: exit {} stdout={}",
                out.code, out.stdout
            ));
        }
        let history = run_with(&["wiki", "history", &page_id]);
        let newest = history
            .get(0)
            .and_then(|r| r["version"].as_i64())
            .unwrap_or(0);
        let restored = run_with(&[
            "wiki",
            "restore",
            &page_id,
            "--version",
            &newest.to_string(),
            "--before",
        ]);
        if s(&restored, "title") != "CLI page" {
            live.failures
                .borrow_mut()
                .push(format!("restore --before: {restored}"));
        }
        if !folder_id.is_empty() {
            run_with(&[
                "wiki", "move", &page_id, "--parent", &folder_id, "--index", "0",
            ]);
            let tree = run_with(&["wiki", "tree"]);
            let placed = tree
                .as_array()
                .into_iter()
                .flatten()
                .any(|n| n["id"] == page_id.as_str() && n["parent_id"] == folder_id.as_str());
            if !placed {
                live.failures
                    .borrow_mut()
                    .push(format!("wiki move: page not under folder in {tree}"));
            }
        }
        run_with(&["wiki", "delete", &page_id]);
    }
    if !folder_id.is_empty() {
        run_with(&["wiki", "delete", &folder_id]);
    }

    // ── Registry (project scope) ──────────────────────────────────
    let pscope = ["--project", project_name.as_str()];
    let mut args = vec!["registry", "settings", "--enable"];
    args.extend(pscope);
    let settings = live.ok(&args);
    if settings["registry_enabled"] != true {
        live.failures
            .borrow_mut()
            .push(format!("registry settings --enable: {settings}"));
    }
    let mut args = vec!["registry", "append"];
    args.extend(pscope);
    args.push("CLI contract entry");
    let day = live.ok(&args);
    if !s(&day, "content").contains("CLI contract entry") {
        live.failures
            .borrow_mut()
            .push(format!("registry append: {day}"));
    }
    let mut args = vec!["registry", "feed", "--limit", "2"];
    args.extend(pscope);
    let feed = live.ok(&args);
    if feed["days"].as_array().is_none_or(|d| d.is_empty()) {
        live.failures
            .borrow_mut()
            .push(format!("registry feed is empty: {feed}"));
    }
    let mut args = vec!["registry", "overview"];
    args.extend(pscope);
    live.ok(&args);

    // ── Not found and scope denials ───────────────────────────────
    live.expect_code(&["issues", "read", &format!("{team_key}-999999")], 3);
    live.expect_code(
        &[
            "wiki",
            "read",
            "00000000-0000-0000-0000-000000000000",
            "--team",
            &team_key,
        ],
        3,
    );
    if let Ok(readonly) = std::env::var("KUAYLE_CONTRACT_READONLY_TOKEN") {
        let ro = Live {
            url: live.url.clone(),
            workspace: live.workspace.clone(),
            home: TempDir::new().unwrap(),
            failures: RefCell::new(vec![]),
        };
        ro.cmd()
            .args(["auth", "login", "--token", &readonly])
            .assert()
            .success();
        ro.ok(&["wiki", "list", "--team", &team_key]);
        // Without agent_tasks:read / workflows:read.
        ro.expect_code(&["agent-tasks", "list"], 5);
        ro.expect_code(&["workflows", "list"], 5);
        ro.expect_code(
            &["wiki", "create", "--team", &team_key, "--title", "denied"],
            5,
        );
        ro.expect_code(&["registry", "append", "--team", &team_key, "denied"], 5);
        ro.expect_code(
            &["registry", "settings", "--team", &team_key, "--enable"],
            5,
        );
        live.failures.borrow_mut().extend(ro.failures.into_inner());
    }

    live.finish();
}
