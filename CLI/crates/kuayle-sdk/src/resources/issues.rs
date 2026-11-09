// Issue resource endpoints.
// Issue 资源端点。

use crate::client::Client;
use crate::error::KuayleError;
use crate::filter::IssueFilter;
use crate::types::comment::CommentResponse;
use crate::types::issue::{
    BatchUpdateRequest, CreateIssueRequest, IssueResponse, UpdateIssueRequest,
};
use crate::types::relation::RelationResponse;
use futures_core::Stream;

/// Issue resource helper with typed methods for all issue endpoints.
/// Issue 资源辅助，为所有 issue 端点提供类型化方法。
#[derive(Clone)]
pub struct Issues {
    client: Client,
    workspace: String,
}

impl Issues {
    pub fn new(client: &Client, workspace: &str) -> Self {
        Issues {
            client: client.clone(),
            workspace: workspace.to_string(),
        }
    }

    /// Return the workspace slug this Issues instance is scoped to.
    /// 返回此 Issues 实例所限定范围的 workspace slug。
    pub fn workspace(&self) -> &str {
        &self.workspace
    }

    fn path(&self, suffix: &str) -> String {
        format!("/api/workspaces/{}/issues{}", self.workspace, suffix)
    }

    /// List issues with optional filter. Paginated.
    /// 列出 issue，可选 filter。分页。
    pub fn list(
        &self,
        filter: IssueFilter,
    ) -> impl Stream<Item = Result<IssueResponse, KuayleError>> + '_ {
        let path = self.path("");
        let query_value = serde_json::to_value(&filter).unwrap_or_default();
        self.client.paginate_raw(&path, query_value)
    }

    /// Read a single issue by identifier (e.g. "ENG-25") or UUID.
    /// 通过 identifier（如 "ENG-25"）或 UUID 读取单个 issue。
    pub async fn read(&self, identifier: &str) -> Result<IssueResponse, KuayleError> {
        self.client.get(&self.path(&format!("/{identifier}"))).await
    }

    /// Create a new issue.
    /// 创建新 issue。
    pub async fn create(&self, req: &CreateIssueRequest) -> Result<IssueResponse, KuayleError> {
        self.client.post(&self.path(""), req).await
    }

    /// Update an issue by identifier or UUID.
    /// 通过 identifier 或 UUID 更新 issue。
    pub async fn update(
        &self,
        identifier: &str,
        req: &UpdateIssueRequest,
    ) -> Result<IssueResponse, KuayleError> {
        self.client
            .patch(&self.path(&format!("/{identifier}")), req)
            .await
    }

    /// Delete an issue by identifier or UUID.
    /// 通过 identifier 或 UUID 删除 issue。
    pub async fn delete(&self, identifier: &str) -> Result<serde_json::Value, KuayleError> {
        self.client
            .delete(&self.path(&format!("/{identifier}")))
            .await
    }

    /// Batch update multiple issues.
    /// 批量更新多个 issue。
    pub async fn batch_update(
        &self,
        req: &BatchUpdateRequest,
    ) -> Result<serde_json::Value, KuayleError> {
        self.client.post(&self.path("/batch"), req).await
    }

    /// Batch delete multiple issues.
    /// 批量删除多个 issue。
    pub async fn batch_delete(
        &self,
        identifiers: &[String],
    ) -> Result<serde_json::Value, KuayleError> {
        let body = serde_json::json!({"issue_identifiers": identifiers});
        self.client.post(&self.path("/batch-delete"), &body).await
    }

    /// Subscribe to an issue.
    /// 订阅 issue。
    pub async fn subscribe(&self, identifier: &str) -> Result<serde_json::Value, KuayleError> {
        self.client
            .post(
                &self.path(&format!("/{identifier}/subscribe")),
                &serde_json::Value::Null,
            )
            .await
    }

    /// Unsubscribe from an issue.
    /// 取消订阅 issue。
    pub async fn unsubscribe(&self, identifier: &str) -> Result<serde_json::Value, KuayleError> {
        self.client
            .post(
                &self.path(&format!("/{identifier}/unsubscribe")),
                &serde_json::Value::Null,
            )
            .await
    }

    /// Get issue history (activity log).
    /// 获取 issue 历史（活动日志）。
    pub async fn history(&self, identifier: &str) -> Result<serde_json::Value, KuayleError> {
        self.client
            .get(&self.path(&format!("/{identifier}/history")))
            .await
    }

    /// List comments on an issue. Paginated.
    /// 列出 issue 的评论。分页。
    pub fn comments(
        &self,
        identifier: &str,
    ) -> impl Stream<Item = Result<CommentResponse, KuayleError>> + '_ {
        let path = self.path(&format!("/{identifier}/comments"));
        self.client.paginate_raw(&path, serde_json::json!({}))
    }

    /// List relations on an issue.
    /// 列出 issue 的关系。
    pub async fn relations(&self, identifier: &str) -> Result<Vec<RelationResponse>, KuayleError> {
        self.client
            .get(&self.path(&format!("/{identifier}/relations")))
            .await
    }

    /// List sub-issues of an issue. Paginated.
    /// 列出 issue 的子 issue。分页。
    pub fn sub_issues(
        &self,
        identifier: &str,
    ) -> impl Stream<Item = Result<IssueResponse, KuayleError>> + '_ {
        let path = self.path(&format!("/{identifier}/sub-issues"));
        self.client.paginate_raw(&path, serde_json::json!({}))
    }
}
