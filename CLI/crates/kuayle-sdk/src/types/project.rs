// Project types aligned with kuayle API.
// 与 kuayle API 对齐的项目类型。

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::common::ProgressInfo;

/// Project status values.
/// 项目状态值。
#[derive(Debug, Clone, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "snake_case")]
pub enum ProjectStatus {
    Planned,
    #[serde(rename = "in_progress")]
    InProgress,
    Completed,
    Cancelled,
}

/// A kuayle project, returned by `GET /api/projects`.
/// kuayle 项目，由 `GET /api/projects` 返回。
///
/// JSON shape:
/// ```json
/// {
///   "id": "e0000000-0000-0000-0000-000000000001",
///   "name": "Mobile App v2",
///   "description": "Second major release",
///   "status": "in_progress",
///   "team_id": "c0000000-0000-0000-0000-000000000001",
///   "lead_id": "a0000000-0000-0000-0000-000000000001",
///   "start_date": "2026-01-01",
///   "target_date": "2026-06-30",
///   "sort_order": 1000,
///   "progress": {
///     "total": 42,
///     "completed": 15,
///     "cancelled": 3
///   },
///   "created_at": "2026-07-28T15:58:38.119837+08:00",
///   "updated_at": "2026-07-28T15:58:38.119837+08:00"
/// }
/// ```
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct ProjectResponse {
    pub id: String,
    pub name: String,

    #[serde(default)]
    pub description: Option<String>,

    #[serde(default)]
    pub status: Option<ProjectStatus>,

    #[serde(default)]
    pub team_id: Option<String>,

    #[serde(default)]
    pub lead_id: Option<String>,

    #[serde(default)]
    pub start_date: Option<String>,

    #[serde(default)]
    pub target_date: Option<String>,

    #[serde(default)]
    pub sort_order: Option<i64>,

    #[serde(default)]
    pub progress: Option<ProgressInfo>,

    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

/// Request body for creating a project.
/// 创建项目的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct CreateProjectRequest {
    pub name: String,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<ProjectStatus>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub team_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub lead_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub start_date: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub target_date: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub sort_order: Option<i64>,
}

/// Request body for updating a project.
/// 更新项目的请求体。
#[derive(Debug, Clone, Serialize, Default)]
pub struct UpdateProjectRequest {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<ProjectStatus>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub team_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub lead_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub start_date: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub target_date: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub sort_order: Option<i64>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_project_from_api() {
        let json = r#"{
            "id": "e0000000-0000-0000-0000-000000000001",
            "name": "Mobile App v2",
            "description": "Second major release",
            "status": "in_progress",
            "team_id": "c0000000-0000-0000-0000-000000000001",
            "lead_id": "a0000000-0000-0000-0000-000000000001",
            "start_date": "2026-01-01",
            "target_date": "2026-06-30",
            "sort_order": 1000,
            "progress": {
                "total": 42,
                "completed": 15,
                "cancelled": 3
            },
            "created_at": "2026-07-28T15:58:38.119837+08:00",
            "updated_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let project: ProjectResponse = serde_json::from_str(json).unwrap();
        assert_eq!(project.id, "e0000000-0000-0000-0000-000000000001");
        assert_eq!(project.name, "Mobile App v2");
        assert_eq!(project.status, Some(ProjectStatus::InProgress));
        assert_eq!(
            project.team_id.as_deref(),
            Some("c0000000-0000-0000-0000-000000000001")
        );
        let progress = project.progress.as_ref().unwrap();
        assert_eq!(progress.total, 42);
        assert_eq!(progress.completed, 15);
        assert_eq!(progress.cancelled, 3);
    }

    #[test]
    fn deserialize_project_minimal() {
        let json = r#"{
            "id": "e0000000-0000-0000-0000-000000000002",
            "name": "Minimal Project",
            "created_at": "2026-07-28T15:58:38.119837+08:00",
            "updated_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let project: ProjectResponse = serde_json::from_str(json).unwrap();
        assert_eq!(project.name, "Minimal Project");
        assert!(project.description.is_none());
        assert!(project.status.is_none());
        assert!(project.team_id.is_none());
        assert!(project.progress.is_none());
    }

    #[test]
    fn project_status_roundtrip() {
        let cases = vec![
            ("\"planned\"", ProjectStatus::Planned),
            ("\"in_progress\"", ProjectStatus::InProgress),
            ("\"completed\"", ProjectStatus::Completed),
            ("\"cancelled\"", ProjectStatus::Cancelled),
        ];
        for (json_str, expected) in cases {
            let status: ProjectStatus = serde_json::from_str(json_str).unwrap();
            assert_eq!(status, expected);
            let back = serde_json::to_string(&status).unwrap();
            assert_eq!(back, json_str);
        }
    }
}
