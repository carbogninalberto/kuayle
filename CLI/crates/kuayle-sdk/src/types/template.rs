// Template types aligned with kuayle API.
// 与 kuayle API 对齐的模板类型。

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// A kuayle issue template, returned by `GET /api/templates`.
/// kuayle issue 模板，由 `GET /api/templates` 返回。
///
/// JSON shape:
/// ```json
/// {
///   "id": "g0000000-0000-0000-0000-000000000001",
///   "name": "Bug report",
///   "title": "Bug: ",
///   "description": "## Steps to reproduce\n\n## Expected behavior\n\n## Actual behavior",
///   "team_id": "c0000000-0000-0000-0000-000000000001",
///   "created_at": "2026-07-28T15:58:38.119837+08:00",
///   "updated_at": "2026-07-28T15:58:38.119837+08:00"
/// }
/// ```
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct TemplateResponse {
    pub id: String,
    pub name: String,

    #[serde(default)]
    pub title: Option<String>,

    #[serde(default)]
    pub description: Option<String>,

    #[serde(default)]
    pub team_id: Option<String>,

    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

/// Request body for creating a template.
/// 创建模板的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct CreateTemplateRequest {
    pub name: String,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub title: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub team_id: Option<String>,
}

/// Request body for updating a template.
/// 更新模板的请求体。
#[derive(Debug, Clone, Serialize, Default)]
pub struct UpdateTemplateRequest {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub title: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub team_id: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_template_from_api() {
        let json = r##"{
            "id": "g0000000-0000-0000-0000-000000000001",
            "name": "Bug report",
            "title": "Bug: ",
            "description": "Steps to reproduce the issue",
            "team_id": "c0000000-0000-0000-0000-000000000001",
            "created_at": "2026-07-28T15:58:38.119837+08:00",
            "updated_at": "2026-07-28T15:58:38.119837+08:00"
        }"##;
        let template: TemplateResponse = serde_json::from_str(json).unwrap();
        assert_eq!(template.id, "g0000000-0000-0000-0000-000000000001");
        assert_eq!(template.name, "Bug report");
        assert_eq!(template.title.as_deref(), Some("Bug: "));
        assert!(template
            .description
            .as_ref()
            .unwrap()
            .contains("Steps to reproduce"));
        assert_eq!(
            template.team_id.as_deref(),
            Some("c0000000-0000-0000-0000-000000000001")
        );
    }

    #[test]
    fn deserialize_template_minimal() {
        let json = r#"{
            "id": "g0000000-0000-0000-0000-000000000002",
            "name": "Feature request",
            "created_at": "2026-07-28T15:58:38.119837+08:00",
            "updated_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let template: TemplateResponse = serde_json::from_str(json).unwrap();
        assert_eq!(template.name, "Feature request");
        assert!(template.title.is_none());
        assert!(template.description.is_none());
        assert!(template.team_id.is_none());
    }

    #[test]
    fn create_template_request_only_required() {
        let req = CreateTemplateRequest {
            name: "Bug report".into(),
            title: None,
            description: None,
            team_id: None,
        };
        let json = serde_json::to_value(&req).unwrap();
        assert_eq!(json, serde_json::json!({"name": "Bug report"}));
    }
}
