// Notification types aligned with kuayle API.
// 与 kuayle API 对齐的通知类型。

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// A kuayle notification, returned by `GET /api/notifications`.
/// kuayle 通知，由 `GET /api/notifications` 返回。
///
/// JSON shape:
/// ```json
/// {
///   "id": "j0000000-0000-0000-0000-000000000001",
///   "user_id": "a0000000-0000-0000-0000-000000000001",
///   "type": "issue_assigned",
///   "title": "You were assigned to ENG-25",
///   "body": "Alice assigned you to Dark theme polish",
///   "read_at": null,
///   "snoozed_until": null,
///   "archived_at": null,
///   "created_at": "2026-07-28T15:58:38.119837+08:00"
/// }
/// ```
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct NotificationResponse {
    pub id: String,
    pub user_id: String,

    #[serde(rename = "type")]
    pub notification_type: String,

    #[serde(default)]
    pub title: Option<String>,

    #[serde(default)]
    pub body: Option<String>,

    #[serde(default)]
    pub read_at: Option<DateTime<Utc>>,

    #[serde(default)]
    pub snoozed_until: Option<DateTime<Utc>>,

    #[serde(default)]
    pub archived_at: Option<DateTime<Utc>>,

    #[serde(default)]
    pub created_at: Option<DateTime<Utc>>,
}

/// Request body for marking notifications as read.
/// 标记通知为已读的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct MarkReadRequest {
    /// Notification IDs to mark as read.
    /// 要标记为已读的通知 ID。
    pub notification_ids: Vec<String>,
}

/// Request body for snoozing notifications.
/// 推迟通知的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct SnoozeRequest {
    /// Notification IDs to snooze.
    /// 要推迟的通知 ID。
    pub notification_ids: Vec<String>,

    /// ISO 8601 datetime until which to snooze.
    /// 推迟到哪个 ISO 8601 时间。
    pub snoozed_until: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_notification_from_api() {
        let json = r#"{
            "id": "j0000000-0000-0000-0000-000000000001",
            "user_id": "a0000000-0000-0000-0000-000000000001",
            "type": "issue_assigned",
            "title": "You were assigned to ENG-25",
            "body": "Alice assigned you to Dark theme polish",
            "read_at": null,
            "snoozed_until": null,
            "archived_at": null,
            "created_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let notif: NotificationResponse = serde_json::from_str(json).unwrap();
        assert_eq!(notif.id, "j0000000-0000-0000-0000-000000000001");
        assert_eq!(notif.user_id, "a0000000-0000-0000-0000-000000000001");
        assert_eq!(notif.notification_type, "issue_assigned");
        assert_eq!(notif.title.as_deref(), Some("You were assigned to ENG-25"));
        assert!(notif.read_at.is_none());
        assert!(notif.snoozed_until.is_none());
        assert!(notif.archived_at.is_none());
        assert!(notif.created_at.is_some());
    }

    #[test]
    fn deserialize_notification_read() {
        let json = r#"{
            "id": "j0000000-0000-0000-0000-000000000002",
            "user_id": "a0000000-0000-0000-0000-000000000001",
            "type": "comment",
            "read_at": "2026-07-28T16:00:00.000000+08:00",
            "created_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let notif: NotificationResponse = serde_json::from_str(json).unwrap();
        assert_eq!(notif.notification_type, "comment");
        assert!(notif.read_at.is_some());
        assert!(notif.title.is_none());
        assert!(notif.body.is_none());
    }
}
