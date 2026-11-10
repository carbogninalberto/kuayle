// Favorite types aligned with kuayle API.
// 与 kuayle API 对齐的收藏类型。

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// A kuayle favorite, returned by `GET /api/favorites`.
/// kuayle 收藏项，由 `GET /api/favorites` 返回。
///
/// JSON shape:
/// ```json
/// {
///   "id": "i0000000-0000-0000-0000-000000000001",
///   "user_id": "a0000000-0000-0000-0000-000000000001",
///   "favoritable_type": "issue",
///   "favoritable_id": "10000000-0000-0000-0000-000000000040",
///   "created_at": "2026-07-28T15:58:38.119837+08:00"
/// }
/// ```
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct FavoriteResponse {
    pub id: String,
    pub user_id: String,
    pub favoritable_type: String,
    pub favoritable_id: String,

    #[serde(default)]
    pub created_at: Option<DateTime<Utc>>,
}

/// Request body for creating a favorite.
/// 创建收藏的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct CreateFavoriteRequest {
    pub favoritable_type: String,
    pub favoritable_id: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_favorite_from_api() {
        let json = r#"{
            "id": "i0000000-0000-0000-0000-000000000001",
            "user_id": "a0000000-0000-0000-0000-000000000001",
            "favoritable_type": "issue",
            "favoritable_id": "10000000-0000-0000-0000-000000000040",
            "created_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let fav: FavoriteResponse = serde_json::from_str(json).unwrap();
        assert_eq!(fav.id, "i0000000-0000-0000-0000-000000000001");
        assert_eq!(fav.user_id, "a0000000-0000-0000-0000-000000000001");
        assert_eq!(fav.favoritable_type, "issue");
        assert_eq!(fav.favoritable_id, "10000000-0000-0000-0000-000000000040");
        assert!(fav.created_at.is_some());
    }

    #[test]
    fn deserialize_favorite_minimal() {
        let json = r#"{
            "id": "i0000000-0000-0000-0000-000000000002",
            "user_id": "u1",
            "favoritable_type": "project",
            "favoritable_id": "e1"
        }"#;
        let fav: FavoriteResponse = serde_json::from_str(json).unwrap();
        assert_eq!(fav.favoritable_type, "project");
        assert!(fav.created_at.is_none());
    }

    #[test]
    fn create_favorite_request() {
        let req = CreateFavoriteRequest {
            favoritable_type: "issue".into(),
            favoritable_id: "10000000-0000-0000-0000-000000000040".into(),
        };
        let json = serde_json::to_value(&req).unwrap();
        assert_eq!(json["favoritable_type"], "issue");
        assert_eq!(
            json["favoritable_id"],
            "10000000-0000-0000-0000-000000000040"
        );
    }
}
