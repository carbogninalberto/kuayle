// Comment types aligned with kuayle API.
// 与 kuayle API 对齐的评论类型。

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::issue::IssueUser;

/// A comment on an issue, as returned by the kuayle API.
/// kuayle API 返回的 issue 评论。
///
/// JSON shape confirmed against local kuayle instance (2026-07-29):
/// ```json
/// {
///   "id": "d5f3e1f8-...",
///   "issue_id": "d0da5315-...",
///   "user_id": "a0000000-...",
///   "body": "test comment",
///   "resolved_at": "2026-07-29T20:11:07.419298+08:00",
///   "user": { "id": "...", "email": "...", ... },
///   "created_at": "...",
///   "updated_at": "..."
/// }
/// ```
/// JSON 形状已对照本地 kuayle 实例确认（2026-07-29）。
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct CommentResponse {
    pub id: String,
    pub issue_id: String,
    pub body: String,
    pub user_id: String,
    pub user: Option<IssueUser>,
    /// null if not resolved, timestamp if resolved.
    /// 未解决时为 null，已解决时为时间戳。
    #[serde(default)]
    pub resolved_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl CommentResponse {
    /// Whether this comment has been resolved.
    /// 此评论是否已被解决。
    pub fn is_resolved(&self) -> bool {
        self.resolved_at.is_some()
    }
}

/// Request body for creating a comment.
/// 创建评论的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct CreateCommentRequest {
    pub body: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_comment_unresolved() {
        // From real API: GET /api/workspaces/acme/issues/ENG-28/comments
        let json = r#"{
            "id": "d5f3e1f8-353a-41f8-86fb-5c8df082ff5c",
            "issue_id": "d0da5315-4bd5-452d-a29d-71f86afa03bb",
            "user_id": "a0000000-0000-0000-0000-000000000001",
            "body": "test comment",
            "user": {
                "id": "a0000000-0000-0000-0000-000000000001",
                "email": "alice@kuayle.dev",
                "name": "Alice Chen",
                "display_name": "Alice",
                "avatar_url": null,
                "is_sysadmin": false
            },
            "created_at": "2026-07-29T20:11:07.341675+08:00",
            "updated_at": "2026-07-29T20:11:07.341675+08:00"
        }"#;
        let c: CommentResponse = serde_json::from_str(json).unwrap();
        assert_eq!(c.body, "test comment");
        assert!(!c.is_resolved());
        assert!(c.resolved_at.is_none());
    }

    #[test]
    fn deserialize_comment_resolved() {
        let json = r#"{
            "id": "abc",
            "issue_id": "abc",
            "body": "done",
            "user_id": "u1",
            "user": null,
            "resolved_at": "2026-07-29T20:11:07.419298+08:00",
            "created_at": "2026-07-29T20:11:07+08:00",
            "updated_at": "2026-07-29T20:11:07+08:00"
        }"#;
        let c: CommentResponse = serde_json::from_str(json).unwrap();
        assert!(c.is_resolved());
    }
}
