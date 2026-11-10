// Team types aligned with kuayle API.
// 与 kuayle API 对齐的团队类型。

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// A kuayle team, returned by `GET /api/teams`.
/// kuayle 团队，由 `GET /api/teams` 返回。
///
/// JSON shape:
/// ```json
/// {
///   "id": "c0000000-0000-0000-0000-000000000001",
///   "name": "Engineering",
///   "key": "ENG",
///   "description": "Engineering team",
///   "color": "#3b82f6",
///   "icon": "rocket",
///   "triage_enabled": true,
///   "parent_auto_close_enabled": false,
///   "sub_issue_auto_close_enabled": true,
///   "issue_copy_prompt": null,
///   "created_at": "2026-07-28T15:58:38.119837+08:00",
///   "updated_at": "2026-07-28T15:58:38.119837+08:00"
/// }
/// ```
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct TeamResponse {
    pub id: String,
    pub name: String,
    pub key: String,

    #[serde(default)]
    pub description: Option<String>,

    #[serde(default)]
    pub color: Option<String>,

    #[serde(default)]
    pub icon: Option<String>,

    #[serde(default)]
    pub triage_enabled: Option<bool>,

    #[serde(default)]
    pub parent_auto_close_enabled: Option<bool>,

    #[serde(default)]
    pub sub_issue_auto_close_enabled: Option<bool>,

    #[serde(default)]
    pub issue_copy_prompt: Option<String>,

    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

/// Request body for creating a team.
/// 创建团队的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct CreateTeamRequest {
    pub name: String,
    pub key: String,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub color: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub icon: Option<String>,
}

/// Request body for updating a team.
/// 更新团队的请求体。
#[derive(Debug, Clone, Serialize, Default)]
pub struct UpdateTeamRequest {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub key: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub color: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub icon: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub triage_enabled: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub parent_auto_close_enabled: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub sub_issue_auto_close_enabled: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub issue_copy_prompt: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_team_from_api() {
        let json = r##"{
            "id": "c0000000-0000-0000-0000-000000000001",
            "name": "Engineering",
            "key": "ENG",
            "description": "Engineering team",
            "color": "#3b82f6",
            "icon": "rocket",
            "triage_enabled": true,
            "parent_auto_close_enabled": false,
            "sub_issue_auto_close_enabled": true,
            "issue_copy_prompt": null,
            "created_at": "2026-07-28T15:58:38.119837+08:00",
            "updated_at": "2026-07-28T15:58:38.119837+08:00"
        }"##;
        let team: TeamResponse = serde_json::from_str(json).unwrap();
        assert_eq!(team.id, "c0000000-0000-0000-0000-000000000001");
        assert_eq!(team.name, "Engineering");
        assert_eq!(team.key, "ENG");
        assert_eq!(team.description.as_deref(), Some("Engineering team"));
        assert_eq!(team.color.as_deref(), Some("#3b82f6"));
        assert!(team.triage_enabled.unwrap());
        assert!(!team.parent_auto_close_enabled.unwrap());
        assert!(team.issue_copy_prompt.is_none());
    }

    #[test]
    fn deserialize_team_minimal() {
        // Minimal JSON with only required fields.
        // 仅包含必填字段的最小 JSON。
        let json = r#"{
            "id": "c0000000-0000-0000-0000-000000000002",
            "name": "Design",
            "key": "DES",
            "created_at": "2026-07-28T15:58:38.119837+08:00",
            "updated_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let team: TeamResponse = serde_json::from_str(json).unwrap();
        assert_eq!(team.name, "Design");
        assert!(team.description.is_none());
        assert!(team.color.is_none());
        assert!(team.icon.is_none());
        assert!(team.triage_enabled.is_none());
    }

    #[test]
    fn create_team_request_only_required() {
        let req = CreateTeamRequest {
            name: "Engineering".into(),
            key: "ENG".into(),
            description: None,
            color: None,
            icon: None,
        };
        let json = serde_json::to_value(&req).unwrap();
        assert_eq!(
            json,
            serde_json::json!({"name": "Engineering", "key": "ENG"})
        );
    }
}
