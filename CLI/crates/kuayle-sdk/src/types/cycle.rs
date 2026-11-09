// Cycle types aligned with kuayle API.
// 与 kuayle API 对齐的周期类型。

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::common::ProgressInfo;

/// Cycle status values.
/// 周期状态值。
#[derive(Debug, Clone, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "lowercase")]
pub enum CycleStatus {
    Active,
    Upcoming,
    Completed,
}

/// A kuayle cycle, returned by `GET /api/cycles`.
/// kuayle 周期，由 `GET /api/cycles` 返回。
///
/// JSON shape:
/// ```json
/// {
///   "id": "f0000000-0000-0000-0000-000000000001",
///   "team_id": "c0000000-0000-0000-0000-000000000001",
///   "name": "Sprint 42",
///   "number": 42,
///   "status": "active",
///   "description": "Bug bash sprint",
///   "goals": "Fix top 10 bugs",
///   "retrospective": null,
///   "start_date": "2026-07-14",
///   "end_date": "2026-07-28",
///   "completed_at": null,
///   "progress": {
///     "total": 30,
///     "completed": 22,
///     "cancelled": 2
///   },
///   "created_at": "2026-07-28T15:58:38.119837+08:00",
///   "updated_at": "2026-07-28T15:58:38.119837+08:00"
/// }
/// ```
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct CycleResponse {
    pub id: String,

    #[serde(default)]
    pub team_id: Option<String>,

    pub name: String,

    #[serde(default)]
    pub number: Option<i32>,

    #[serde(default)]
    pub status: Option<CycleStatus>,

    #[serde(default)]
    pub description: Option<String>,

    #[serde(default)]
    pub goals: Option<String>,

    #[serde(default)]
    pub retrospective: Option<String>,

    #[serde(default)]
    pub start_date: Option<String>,

    #[serde(default)]
    pub end_date: Option<String>,

    #[serde(default)]
    pub completed_at: Option<DateTime<Utc>>,

    #[serde(default)]
    pub progress: Option<ProgressInfo>,

    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

/// Request body for creating a cycle.
/// 创建周期的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct CreateCycleRequest {
    pub name: String,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub team_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub number: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<CycleStatus>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub goals: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub retrospective: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub start_date: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub end_date: Option<String>,
}

/// Request body for updating a cycle.
/// 更新周期的请求体。
#[derive(Debug, Clone, Serialize, Default)]
pub struct UpdateCycleRequest {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub number: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<CycleStatus>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub goals: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub retrospective: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub start_date: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub end_date: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_cycle_from_api() {
        let json = r#"{
            "id": "f0000000-0000-0000-0000-000000000001",
            "team_id": "c0000000-0000-0000-0000-000000000001",
            "name": "Sprint 42",
            "number": 42,
            "status": "active",
            "description": "Bug bash sprint",
            "goals": "Fix top 10 bugs",
            "retrospective": null,
            "start_date": "2026-07-14",
            "end_date": "2026-07-28",
            "completed_at": null,
            "progress": {
                "total": 30,
                "completed": 22,
                "cancelled": 2
            },
            "created_at": "2026-07-28T15:58:38.119837+08:00",
            "updated_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let cycle: CycleResponse = serde_json::from_str(json).unwrap();
        assert_eq!(cycle.id, "f0000000-0000-0000-0000-000000000001");
        assert_eq!(cycle.name, "Sprint 42");
        assert_eq!(cycle.number, Some(42));
        assert_eq!(cycle.status, Some(CycleStatus::Active));
        let progress = cycle.progress.as_ref().unwrap();
        assert_eq!(progress.total, 30);
        assert_eq!(progress.completed, 22);
        assert_eq!(progress.cancelled, 2);
    }

    #[test]
    fn deserialize_cycle_minimal() {
        let json = r#"{
            "id": "f0000000-0000-0000-0000-000000000002",
            "name": "Minimal Sprint",
            "created_at": "2026-07-28T15:58:38.119837+08:00",
            "updated_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let cycle: CycleResponse = serde_json::from_str(json).unwrap();
        assert_eq!(cycle.name, "Minimal Sprint");
        assert!(cycle.team_id.is_none());
        assert!(cycle.number.is_none());
        assert!(cycle.status.is_none());
    }

    #[test]
    fn cycle_status_roundtrip() {
        let cases = vec![
            ("\"active\"", CycleStatus::Active),
            ("\"upcoming\"", CycleStatus::Upcoming),
            ("\"completed\"", CycleStatus::Completed),
        ];
        for (json_str, expected) in cases {
            let status: CycleStatus = serde_json::from_str(json_str).unwrap();
            assert_eq!(status, expected);
            let back = serde_json::to_string(&status).unwrap();
            assert_eq!(back, json_str);
        }
    }
}
