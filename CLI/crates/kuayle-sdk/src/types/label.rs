// Label types aligned with kuayle `/api/workspaces/:slug/labels`.
// 与 kuayle `/api/workspaces/:slug/labels` 对齐的标签类型。

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// A kuayle label, workspace-scoped with optional parent hierarchy.
/// kuayle 标签，工作区级别，支持可选的父子层级。
///
/// JSON shape confirmed against local kuayle instance:
/// ```json
/// {
///   "id": "d0000000-0000-0000-0000-000000000001",
///   "name": "Bug",
///   "color": "#ef4444",
///   "description": null,
///   "parent_id": null,
///   "created_at": "2026-07-28T15:58:38.119837+08:00",
///   "updated_at": "2026-07-28T15:58:38.119837+08:00"
/// }
/// ```
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct LabelResponse {
    pub id: String,
    pub name: String,
    pub color: Option<String>,
    pub description: Option<String>,
    pub parent_id: Option<String>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

/// Request body for creating a label.
/// 创建标签的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct CreateLabelRequest {
    pub name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub color: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub parent_id: Option<String>,
}

/// Request body for updating a label.
/// 更新标签的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct UpdateLabelRequest {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub color: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub parent_id: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_label_from_real_api() {
        let json = r##"{
            "id": "d0000000-0000-0000-0000-000000000001",
            "name": "Bug",
            "color": "#ef4444",
            "description": null,
            "parent_id": null,
            "created_at": "2026-07-28T15:58:38.119837+08:00",
            "updated_at": "2026-07-28T15:58:38.119837+08:00"
        }"##;
        let label: LabelResponse = serde_json::from_str(json).unwrap();
        assert_eq!(label.name, "Bug");
        assert_eq!(label.color.as_deref(), Some("#ef4444"));
        assert!(label.parent_id.is_none());
    }
}
