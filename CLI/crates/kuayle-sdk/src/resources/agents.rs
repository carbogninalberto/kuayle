// Read-only agent task and workflow endpoints.
//
// Tokens need agent_tasks:read or workflows:read. Both are member-only:
// guests are denied, and the server applies the same task and workflow
// visibility as in the web app. Responses are returned as JSON because their
// shapes follow the server's agent features closely.

use serde_json::Value;

use crate::client::Client;
use crate::error::KuayleError;

#[derive(Clone)]
pub struct Agents {
    client: Client,
    workspace: String,
}

impl Agents {
    pub fn new(client: &Client, workspace: &str) -> Self {
        Agents {
            client: client.clone(),
            workspace: workspace.to_string(),
        }
    }

    fn path(&self, suffix: &str) -> String {
        format!("/api/workspaces/{}{suffix}", self.workspace)
    }

    /// The workspace's agent task queue, as the web app shows it.
    pub async fn tasks(&self) -> Result<Value, KuayleError> {
        self.client.get(&self.path("/agent-tasks")).await
    }

    pub async fn task(&self, id: &str) -> Result<Value, KuayleError> {
        self.client
            .get(&self.path(&format!("/agent-tasks/{id}")))
            .await
    }

    /// State transitions, oldest first; `after` is a sequence cursor.
    pub async fn task_history(&self, id: &str, after: Option<i64>) -> Result<Value, KuayleError> {
        let q = after.map(|a| format!("?after={a}")).unwrap_or_default();
        self.client
            .get(&self.path(&format!("/agent-tasks/{id}/history{q}")))
            .await
    }

    pub async fn task_runs(&self, id: &str) -> Result<Value, KuayleError> {
        self.client
            .get(&self.path(&format!("/agent-tasks/{id}/runs")))
            .await
    }

    pub async fn workflows(&self) -> Result<Value, KuayleError> {
        self.client.get(&self.path("/workflows")).await
    }

    pub async fn workflow(&self, id: &str) -> Result<Value, KuayleError> {
        self.client
            .get(&self.path(&format!("/workflows/{id}")))
            .await
    }

    pub async fn profiles(&self) -> Result<Value, KuayleError> {
        self.client.get(&self.path("/agent-profiles")).await
    }
}
