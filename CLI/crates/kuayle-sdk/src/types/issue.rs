// Issue types aligned with kuayle backend DTOs.
// 与 kuayle 后端 DTO 对齐的 issue 类型。
//
// Includes embedded/linked types: labels, status_info, creator, assignee, assignees.
// 包含内嵌/关联类型：labels、status_info、creator、assignee、assignees。

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::label::LabelResponse;

/// Summary of a user, used in embedded contexts (creator, assignee).
/// 用户摘要，用于内嵌上下文（creator、assignee）。
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct IssueUser {
    pub id: String,
    pub email: String,
    pub name: String,
    pub display_name: String,
    pub avatar_url: Option<String>,
    pub is_sysadmin: bool,
}

/// Status information embedded in issue responses.
/// issue 响应中内嵌的状态信息。
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct StatusInfo {
    pub id: String,
    pub name: String,
    pub category: String,
    pub color: Option<String>,
    pub position: i32,
}

/// An issue as returned by the kuayle API.
/// kuayle API 返回的 issue。
///
/// JSON shape confirmed against local instance (list and single-read).
/// JSON 形状已对照本地实例确认（列表和单条读取）。
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct IssueResponse {
    pub id: String,
    pub identifier: String,
    pub title: String,
    pub description: Option<String>,
    pub status: String,
    pub status_id: String,
    pub status_info: Option<StatusInfo>,
    pub priority: i32,
    pub team_id: Option<String>,
    pub project_id: Option<String>,
    pub cycle_id: Option<String>,
    pub creator_id: String,
    pub assignee_id: Option<String>,
    pub parent_id: Option<String>,
    pub due_date: Option<String>,
    pub sort_order: i64,
    #[serde(default)]
    pub labels: Vec<LabelResponse>,
    pub creator: Option<IssueUser>,
    pub assignee: Option<IssueUser>,
    #[serde(default)]
    pub assignees: Vec<IssueUser>,
    #[serde(default)]
    pub is_subscribed: bool,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

/// Request body for creating an issue.
/// 创建 issue 的请求体。
///
/// Only `title` is required. All other fields use `skip_serializing_if`
/// to omit `None` (PATCH-style omit-means-no-change for optional fields).
/// 仅 `title` 必填。其余字段用 `skip_serializing_if` 省略 `None`。
#[derive(Debug, Clone, Serialize, Default)]
pub struct CreateIssueRequest {
    pub title: String,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<String>,

    /// Team status UUID for custom statuses.
    /// 自定义状态的团队状态 UUID。
    #[serde(skip_serializing_if = "Option::is_none")]
    pub status_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub priority: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub team_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub project_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub cycle_id: Option<String>,

    /// Single assignee user UUID.
    /// 单个指派人用户 UUID。
    #[serde(skip_serializing_if = "Option::is_none")]
    pub assignee_id: Option<String>,

    /// Multiple assignee UUIDs (migration 000007+).
    /// 多个指派人 UUID（migration 000007+）。
    #[serde(skip_serializing_if = "Option::is_none")]
    pub assignee_ids: Option<Vec<String>>,

    /// Parent issue UUID for sub-issues.
    /// 子 issue 的父 issue UUID。
    #[serde(skip_serializing_if = "Option::is_none")]
    pub parent_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub due_date: Option<String>,

    /// Label UUIDs to attach.
    /// 要附加的标签 UUID。
    #[serde(skip_serializing_if = "Option::is_none")]
    pub label_ids: Option<Vec<String>>,
}

/// Request body for updating an issue (PATCH).
/// 更新 issue 的请求体（PATCH）。
///
/// All fields optional — omitted fields are left unchanged.
/// A field set to `None` in Rust means "don't update".
/// To explicitly clear a field like `due_date`, use
/// `Option<Option<String>>` — inner None → JSON null.
/// 所有字段可选 — 省略的字段保持原值不变。
/// Rust 中 `None` 表示"不更新"。
/// 要显式清除如 `due_date` 的字段，使用
/// `Option<Option<String>>` — 内层 None → JSON null。
#[derive(Debug, Clone, Serialize, Default)]
pub struct UpdateIssueRequest {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub title: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub status_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub priority: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub team_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub project_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub cycle_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub assignee_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub assignee_ids: Option<Vec<String>>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub parent_id: Option<String>,

    /// Explicitly clear due_date: wrap in `Some(None)` to send null.
    /// 显式清除 due_date：用 `Some(None)` 发送 null。
    #[serde(
        skip_serializing_if = "Option::is_none",
        serialize_with = "serialize_clearable"
    )]
    pub due_date: Option<Option<String>>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub label_ids: Option<Vec<String>>,
}

/// Serialize `Option<Option<T>>` for clearable fields.
/// 序列化可清空字段的 `Option<Option<T>>`。
fn serialize_clearable<S>(val: &Option<Option<String>>, serializer: S) -> Result<S::Ok, S::Error>
where
    S: serde::Serializer,
{
    match val {
        None | Some(None) => serializer.serialize_none(),
        Some(Some(s)) => serializer.serialize_some(s),
    }
}

/// Request body for batch operations (update/delete).
/// 批量操作（update/delete）的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct BatchUpdateRequest {
    /// Issue identifiers to operate on (e.g. ["ENG-1", "ENG-2"]).
    /// 要操作的 issue identifier（如 ["ENG-1", "ENG-2"]）。
    pub issue_identifiers: Vec<String>,

    /// Fields to update on all matching issues.
    /// 要在所有匹配 issue 上更新的字段。
    pub update: UpdateIssueRequest,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_issue_from_real_api() {
        let json = r#"{
            "id": "10000000-0000-0000-0000-000000000040",
            "identifier": "ENG-25",
            "title": "Dark theme polish",
            "description": "<p>Fix contrast</p>",
            "status": "backlog",
            "status_id": "50000000-0001-0000-0000-000000000001",
            "status_info": {
                "id": "50000000-0001-0000-0000-000000000001",
                "name": "Backlog",
                "category": "backlog",
                "color": null,
                "position": 1
            },
            "priority": 3,
            "team_id": "c0000000-0000-0000-0000-000000000001",
            "project_id": "e0000000-0000-0000-0000-000000000001",
            "cycle_id": null,
            "creator_id": "a0000000-0000-0000-0000-000000000001",
            "assignee_id": "a0000000-0000-0000-0000-000000000003",
            "parent_id": null,
            "due_date": "2026-03-31T00:00:00Z",
            "sort_order": 25000,
            "labels": [],
            "creator": {
                "id": "a0000000-0000-0000-0000-000000000001",
                "email": "alice@kuayle.dev",
                "name": "Alice Chen",
                "display_name": "Alice",
                "avatar_url": null,
                "is_sysadmin": false
            },
            "assignee": null,
            "assignees": [],
            "is_subscribed": false,
            "created_at": "2026-07-28T15:58:38.119837+08:00",
            "updated_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let issue: IssueResponse = serde_json::from_str(json).unwrap();
        assert_eq!(issue.identifier, "ENG-25");
        assert_eq!(issue.title, "Dark theme polish");
        assert_eq!(issue.priority, 3);
        assert!(issue.status_info.is_some());
        assert_eq!(issue.status_info.as_ref().unwrap().name, "Backlog");
        assert!(issue.creator.is_some());
        assert_eq!(issue.creator.as_ref().unwrap().name, "Alice Chen");
    }

    #[test]
    fn create_request_only_title() {
        let req = CreateIssueRequest {
            title: "Test".into(),
            ..Default::default()
        };
        let json = serde_json::to_value(&req).unwrap();
        assert_eq!(json, serde_json::json!({"title": "Test"}));
    }
}
