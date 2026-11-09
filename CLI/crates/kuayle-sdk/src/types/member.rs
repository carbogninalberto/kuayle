// Member types aligned with kuayle API.
// 与 kuayle API 对齐的成员类型。

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// A kuayle workspace member, returned by `GET /api/workspaces/:slug/members`.
/// kuayle 工作区成员，由 `GET /api/workspaces/:slug/members` 返回。
///
/// JSON shape:
/// ```json
/// {
///   "user_id": "a0000000-0000-0000-0000-000000000001",
///   "email": "alice@kuayle.dev",
///   "name": "Alice Chen",
///   "role": "admin",
///   "created_at": "2026-07-28T15:58:38.119837+08:00"
/// }
/// ```
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct MemberResponse {
    pub user_id: String,

    #[serde(default)]
    pub email: Option<String>,

    pub name: String,

    #[serde(default)]
    pub role: Option<String>,

    #[serde(default)]
    pub created_at: Option<DateTime<Utc>>,
}

/// Request body for adding a member to a workspace.
/// 向工作区添加成员的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct AddMemberRequest {
    pub user_id: String,
    pub role: String,
}

/// Request body for updating a member's role.
/// 更新成员角色的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct UpdateMemberRequest {
    pub role: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_member_from_api() {
        let json = r#"{
            "user_id": "a0000000-0000-0000-0000-000000000001",
            "email": "alice@kuayle.dev",
            "name": "Alice Chen",
            "role": "admin",
            "created_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let member: MemberResponse = serde_json::from_str(json).unwrap();
        assert_eq!(member.user_id, "a0000000-0000-0000-0000-000000000001");
        assert_eq!(member.email.as_deref(), Some("alice@kuayle.dev"));
        assert_eq!(member.name, "Alice Chen");
        assert_eq!(member.role.as_deref(), Some("admin"));
        assert!(member.created_at.is_some());
    }

    #[test]
    fn deserialize_member_minimal() {
        let json = r#"{
            "user_id": "a0000000-0000-0000-0000-000000000002",
            "name": "Bob"
        }"#;
        let member: MemberResponse = serde_json::from_str(json).unwrap();
        assert_eq!(member.user_id, "a0000000-0000-0000-0000-000000000002");
        assert_eq!(member.name, "Bob");
        assert!(member.email.is_none());
        assert!(member.role.is_none());
        assert!(member.created_at.is_none());
    }

    #[test]
    fn add_member_request() {
        let req = AddMemberRequest {
            user_id: "a0000000-0000-0000-0000-000000000003".into(),
            role: "member".into(),
        };
        let json = serde_json::to_value(&req).unwrap();
        assert_eq!(json["user_id"], "a0000000-0000-0000-0000-000000000003");
        assert_eq!(json["role"], "member");
    }
}
