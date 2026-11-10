// User types aligned with kuayle `GET /api/auth/me`.
// 与 kuayle `GET /api/auth/me` 对齐的用户类型。

use serde::{Deserialize, Serialize};

/// The authenticated user's profile, returned by `GET /api/auth/me`.
/// 已认证用户的个人资料，由 `GET /api/auth/me` 返回。
///
/// JSON shape confirmed against local kuayle instance:
/// JSON 形状已对照本地 kuayle 实例确认：
/// ```json
/// {
///   "id": "a0000000-0000-0000-0000-000000000001",
///   "email": "alice@kuayle.dev",
///   "name": "Alice Chen",
///   "display_name": "Alice",
///   "avatar_url": null,
///   "is_sysadmin": true
/// }
/// ```
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct UserResponse {
    pub id: String,
    pub email: String,
    pub name: String,
    pub display_name: String,
    pub avatar_url: Option<String>,
    pub is_sysadmin: bool,
}

/// Summary view of a user embedded in other resources.
/// 嵌入在其他资源中的用户摘要视图。
///
/// Appears inside workspace `owner`, issue `creator`, etc.
/// 出现在 workspace 的 `owner`、issue 的 `creator` 等字段中。
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct UserSummary {
    pub id: String,
    pub email: String,
    pub name: String,
    pub avatar_url: Option<String>,
}

// Note: There is no `created_at` / `updated_at` on the user profile
// response from `/api/auth/me`, so we do NOT add those fields here.
// 注意：`/api/auth/me` 的用户资料响应中没有 `created_at` / `updated_at`，
// 因此这里不添加这些字段。

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_user_response_from_real_api() {
        // Exact JSON from local kuayle instance `GET /api/auth/me`.
        // 来自本地 kuayle 实例 `GET /api/auth/me` 的精确 JSON。
        let json = r#"{
            "id": "a0000000-0000-0000-0000-000000000001",
            "email": "alice@kuayle.dev",
            "name": "Alice Chen",
            "display_name": "Alice",
            "avatar_url": null,
            "is_sysadmin": true
        }"#;
        let user: UserResponse = serde_json::from_str(json).unwrap();
        assert_eq!(user.id, "a0000000-0000-0000-0000-000000000001");
        assert_eq!(user.email, "alice@kuayle.dev");
        assert_eq!(user.name, "Alice Chen");
        assert_eq!(user.display_name, "Alice");
        assert!(user.avatar_url.is_none());
        assert!(user.is_sysadmin);
    }

    #[test]
    fn deserialize_user_response_with_avatar() {
        let json = r#"{
            "id": "u1",
            "email": "bob@example.com",
            "name": "Bob",
            "display_name": "Bobby",
            "avatar_url": "https://example.com/avatar.png",
            "is_sysadmin": false
        }"#;
        let user: UserResponse = serde_json::from_str(json).unwrap();
        assert_eq!(
            user.avatar_url.as_deref(),
            Some("https://example.com/avatar.png")
        );
        assert!(!user.is_sysadmin);
    }

    #[test]
    fn deserialize_user_summary() {
        // From workspace `owner` field.
        let json = r#"{
            "id": "a0000000-0000-0000-0000-000000000001",
            "email": "alice@kuayle.dev",
            "name": "Alice Chen",
            "avatar_url": null
        }"#;
        let summary: UserSummary = serde_json::from_str(json).unwrap();
        assert_eq!(summary.name, "Alice Chen");
    }
}
