// agent-tasks and workflows: read-only views of Kuayle's agent features.
//
// Need the agent_tasks:read / workflows:read token scopes (members only).

use kuayle_sdk::error::KuayleError;
use kuayle_sdk::resources::agents::Agents;
use serde_json::Value;

use crate::cli::{AgentTaskAction, Cli, WorkflowAction};
use crate::output::{self, is_json_output, Format};

fn fail(err: KuayleError, is_json: bool) -> ! {
    output::print_error(&err, is_json);
    std::process::exit(err.exit_code());
}

async fn setup(cli: &Cli) -> (Agents, bool) {
    let is_json = is_json_output(cli);
    let (client, _url) = match crate::commands::resolve_client(cli).await {
        Ok(c) => c,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };
    let ws = crate::commands::workspace_slug(cli);
    (Agents::new(&client, &ws), is_json)
}

fn text(v: &Value, key: &str) -> String {
    match &v[key] {
        Value::String(s) if !s.is_empty() => s.clone(),
        Value::Null => "-".into(),
        Value::String(_) => "-".into(),
        other => other.to_string(),
    }
}

fn json(v: &Value) {
    println!("{}", serde_json::to_string_pretty(v).unwrap_or_default());
}

/// A list at `key` (or the value itself when it is an array).
fn items(v: &Value, key: &str) -> Vec<Value> {
    v.as_array()
        .or_else(|| v[key].as_array())
        .cloned()
        .unwrap_or_default()
}

pub async fn handle_tasks(action: &AgentTaskAction, cli: &Cli) {
    let (agents, is_json) = setup(cli).await;
    match action {
        AgentTaskAction::List => {
            let v = agents.tasks().await.unwrap_or_else(|e| fail(e, is_json));
            if is_json {
                return json(&v);
            }
            output::print_table(
                Format::Human,
                &items(&v, "tasks"),
                &["ID", "ISSUE", "STATE", "PROVIDER", "TITLE"],
                |t| {
                    vec![
                        text(t, "id"),
                        text(t, "issue_identifier"),
                        text(t, "state"),
                        text(t, "provider"),
                        text(t, "issue_title"),
                    ]
                },
            );
        }
        AgentTaskAction::Read { id } => {
            let v = agents.task(id).await.unwrap_or_else(|e| fail(e, is_json));
            if is_json {
                return json(&v);
            }
            for key in [
                "id",
                "state",
                "kind",
                "provider",
                "issue_id",
                "created_at",
                "updated_at",
            ] {
                println!("{:<11}{}", format!("{key}:"), text(&v, key));
            }
        }
        AgentTaskAction::History { id, after } => {
            let v = agents
                .task_history(id, *after)
                .await
                .unwrap_or_else(|e| fail(e, is_json));
            if is_json {
                return json(&v);
            }
            output::print_table(
                Format::Human,
                &items(&v, "transitions"),
                &["SEQ", "AT", "FROM", "TO", "REASON"],
                |t| {
                    vec![
                        text(t, "sequence"),
                        text(t, "created_at"),
                        text(t, "from_state"),
                        text(t, "to_state"),
                        text(t, "reason"),
                    ]
                },
            );
            if !v["next_after"].is_null() {
                println!("… more: --after {}", v["next_after"]);
            }
        }
        AgentTaskAction::Runs { id } => {
            let v = agents
                .task_runs(id)
                .await
                .unwrap_or_else(|e| fail(e, is_json));
            if is_json {
                return json(&v);
            }
            output::print_table(
                Format::Human,
                &items(&v, "runs"),
                &["ID", "STATUS", "MODE", "PROVIDER", "CREATED"],
                |r| {
                    vec![
                        text(r, "id"),
                        text(r, "status"),
                        text(r, "mode"),
                        text(r, "provider_id"),
                        text(r, "created_at"),
                    ]
                },
            );
        }
    }
}

pub async fn handle_workflows(action: &WorkflowAction, cli: &Cli) {
    let (agents, is_json) = setup(cli).await;
    match action {
        WorkflowAction::List => {
            let v = agents
                .workflows()
                .await
                .unwrap_or_else(|e| fail(e, is_json));
            if is_json {
                return json(&v);
            }
            output::print_table(
                Format::Human,
                &items(&v, "workflows"),
                &["ID", "NAME", "VERSION", "BUILT-IN", "ARCHIVED"],
                |w| {
                    vec![
                        text(w, "id"),
                        text(w, "name"),
                        text(w, "latest_version"),
                        text(w, "builtin"),
                        text(w, "archived"),
                    ]
                },
            );
        }
        WorkflowAction::Read { id } => {
            let v = agents
                .workflow(id)
                .await
                .unwrap_or_else(|e| fail(e, is_json));
            // The workflow graph is structured data; JSON is its readable form.
            json(&v);
        }
        WorkflowAction::Profiles => {
            let v = agents.profiles().await.unwrap_or_else(|e| fail(e, is_json));
            if is_json {
                return json(&v);
            }
            output::print_table(
                Format::Human,
                &items(&v, "profiles"),
                &["ID", "NAME", "ROLE", "HARNESS", "MODEL"],
                |p| {
                    vec![
                        text(p, "id"),
                        text(p, "name"),
                        text(p, "role"),
                        text(p, "harness"),
                        text(p, "model"),
                    ]
                },
            );
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn items_accepts_arrays_and_wrapped_lists() {
        let wrapped = serde_json::json!({"workflows": [{"id": "a"}], "can_manage": false});
        assert_eq!(items(&wrapped, "workflows").len(), 1);
        assert_eq!(items(&serde_json::json!([1, 2]), "x").len(), 2);
        assert!(items(&serde_json::json!({}), "x").is_empty());
    }

    #[test]
    fn text_renders_missing_and_non_string_values() {
        let v = serde_json::json!({"a": "x", "b": null, "c": 3, "d": true, "e": ""});
        assert_eq!(text(&v, "a"), "x");
        assert_eq!(text(&v, "b"), "-");
        assert_eq!(text(&v, "missing"), "-");
        assert_eq!(text(&v, "c"), "3");
        assert_eq!(text(&v, "d"), "true");
        assert_eq!(text(&v, "e"), "-");
    }
}
