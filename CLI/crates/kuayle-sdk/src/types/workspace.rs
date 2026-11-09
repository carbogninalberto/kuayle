// Workspace types aligned with kuayle API.
// 与 kuayle API 对齐的工作区类型。

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::user::UserSummary;

/// A kuayle workspace, returned by `GET /api/workspaces` and
/// `GET /api/workspaces/:slug`.
/// kuayle 工作区，由 `GET /api/workspaces` 和 `GET /api/workspaces/:slug` 返回。
///
/// JSON shape confirmed against local kuayle instance:
/// JSON 形状已对照本地 kuayle 实例确认：
/// ```json
/// {
///   "id": "b0000000-0000-0000-0000-000000000001",
///   "name": "Acme Corp",
///   "slug": "acme",
///   "logo_url": null,
///   "owner_id": "a0000000-0000-0000-0000-000000000001",
///   "owner": {
///     "id": "a0000000-0000-0000-0000-000000000001",
///     "email": "alice@kuayle.dev",
///     "name": "Alice Chen",
///     "avatar_url": null
///   },
///   "share_link_min_role": "admin",
///   "current_user_role": "owner",
///   "created_at": "2026-07-28T15:58:38.119837+08:00",
///   "updated_at": "2026-07-28T15:58:38.119837+08:00"
/// }
/// ```
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct WorkspaceResponse {
    pub id: String,
    pub name: String,
    pub slug: String,
    pub logo_url: Option<String>,
    pub owner_id: String,
    pub owner: UserSummary,
    pub share_link_min_role: String,
    pub current_user_role: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_workspace_from_real_api() {
        // Exact JSON from local kuayle instance `GET /api/workspaces`.
        // 来自本地 kuayle 实例 `GET /api/workspaces` 的精确 JSON。
        let json = r#"{
            "id": "b0000000-0000-0000-0000-000000000001",
            "name": "Acme Corp",
            "slug": "acme",
            "logo_url": null,
            "owner_id": "a0000000-0000-0000-0000-000000000001",
            "owner": {
                "id": "a0000000-0000-0000-0000-000000000001",
                "email": "alice@kuayle.dev",
                "name": "Alice Chen",
                "avatar_url": null
            },
            "share_link_min_role": "admin",
            "current_user_role": "owner",
            "created_at": "2026-07-28T15:58:38.119837+08:00",
            "updated_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let ws: WorkspaceResponse = serde_json::from_str(json).unwrap();
        assert_eq!(ws.id, "b0000000-0000-0000-0000-000000000001");
        assert_eq!(ws.name, "Acme Corp");
        assert_eq!(ws.slug, "acme");
        assert_eq!(ws.current_user_role, "owner");
        assert_eq!(ws.owner.name, "Alice Chen");
        assert!(ws.logo_url.is_none());
    }

    #[test]
    fn deserialize_workspace_array_from_real_api() {
        // Two workspaces from `GET /api/workspaces`.
        // 来自 `GET /api/workspaces` 的两个工作区。
        let json = r#"[
            {
                "id": "w1",
                "name": "Acme Corp",
                "slug": "acme",
                "logo_url": null,
                "owner_id": "u1",
                "owner": {
                    "id": "u1",
                    "email": "a@b.com",
                    "name": "Alice",
                    "avatar_url": null
                },
                "share_link_min_role": "admin",
                "current_user_role": "owner",
                "created_at": "2026-07-28T15:58:38.119837+08:00",
                "updated_at": "2026-07-28T15:58:38.119837+08:00"
            },
            {
                "id": "w2",
                "name": "Side Project",
                "slug": "side-project",
                "logo_url": "https://example.com/logo.png",
                "owner_id": "u1",
                "owner": {
                    "id": "u1",
                    "email": "a@b.com",
                    "name": "Alice",
                    "avatar_url": null
                },
                "share_link_min_role": "admin",
                "current_user_role": "member",
                "created_at": "2026-07-28T15:58:38.119837+08:00",
                "updated_at": "2026-07-28T15:58:38.119837+08:00"
            }
        ]"#;
        let workspaces: Vec<WorkspaceResponse> = serde_json::from_str(json).unwrap();
        assert_eq!(workspaces.len(), 2);
        assert_eq!(workspaces[0].slug, "acme");
        assert_eq!(workspaces[1].slug, "side-project");
        assert_eq!(
            workspaces[1].logo_url.as_deref(),
            Some("https://example.com/logo.png")
        );
    }
}
