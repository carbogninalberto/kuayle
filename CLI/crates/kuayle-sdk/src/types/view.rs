// View types aligned with kuayle API.
// 与 kuayle API 对齐的视图类型。

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;

/// A kuayle view (saved filter), returned by `GET /api/views`.
/// kuayle 视图（已保存的筛选器），由 `GET /api/views` 返回。
///
/// JSON shape:
/// ```json
/// {
///   "id": "h0000000-0000-0000-0000-000000000001",
///   "name": "My bugs",
///   "description": "Bugs assigned to me",
///   "filter": {
///     "assignee": { "is": "me" },
///     "status": { "in": ["backlog", "in_progress"] }
///   },
///   "team_id": "c0000000-0000-0000-0000-000000000001",
///   "created_at": "2026-07-28T15:58:38.119837+08:00",
///   "updated_at": "2026-07-28T15:58:38.119837+08:00"
/// }
/// ```
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct ViewResponse {
    pub id: String,
    pub name: String,

    #[serde(default)]
    pub description: Option<String>,

    #[serde(default)]
    pub filter: Option<Value>,

    #[serde(default)]
    pub team_id: Option<String>,

    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

/// Request body for creating a view.
/// 创建视图的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct CreateViewRequest {
    pub name: String,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub filter: Option<Value>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub team_id: Option<String>,
}

/// Request body for updating a view.
/// 更新视图的请求体。
#[derive(Debug, Clone, Serialize, Default)]
pub struct UpdateViewRequest {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub filter: Option<Value>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub team_id: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_view_from_api() {
        let json = r#"{
            "id": "h0000000-0000-0000-0000-000000000001",
            "name": "My bugs",
            "description": "Bugs assigned to me",
            "filter": {
                "assignee": { "is": "me" },
                "status": { "in": ["backlog", "in_progress"] }
            },
            "team_id": "c0000000-0000-0000-0000-000000000001",
            "created_at": "2026-07-28T15:58:38.119837+08:00",
            "updated_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let view: ViewResponse = serde_json::from_str(json).unwrap();
        assert_eq!(view.id, "h0000000-0000-0000-0000-000000000001");
        assert_eq!(view.name, "My bugs");
        assert_eq!(view.description.as_deref(), Some("Bugs assigned to me"));
        assert!(view.filter.is_some());
        let filter = view.filter.as_ref().unwrap();
        assert_eq!(filter["assignee"]["is"], "me");
    }

    #[test]
    fn deserialize_view_minimal() {
        let json = r#"{
            "id": "h0000000-0000-0000-0000-000000000002",
            "name": "All issues",
            "created_at": "2026-07-28T15:58:38.119837+08:00",
            "updated_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let view: ViewResponse = serde_json::from_str(json).unwrap();
        assert_eq!(view.name, "All issues");
        assert!(view.description.is_none());
        assert!(view.filter.is_none());
        assert!(view.team_id.is_none());
    }

    #[test]
    fn create_view_request_with_filter() {
        let filter = serde_json::json!({"assignee": {"is": "me"}});
        let req = CreateViewRequest {
            name: "My bugs".into(),
            description: Some("Bugs assigned to me".into()),
            filter: Some(filter.clone()),
            team_id: None,
        };
        let json = serde_json::to_value(&req).unwrap();
        assert_eq!(json["name"], "My bugs");
        assert_eq!(json["filter"], filter);
    }
}
