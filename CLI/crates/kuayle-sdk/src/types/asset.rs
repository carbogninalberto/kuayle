// Asset types aligned with kuayle API.
// 与 kuayle API 对齐的附件/资源类型。

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// A kuayle asset (uploaded file), returned by `GET /api/assets`.
/// kuayle 附件（上传的文件），由 `GET /api/assets` 返回。
///
/// JSON shape:
/// ```json
/// {
///   "id": "k0000000-0000-0000-0000-000000000001",
///   "filename": "screenshot.png",
///   "content_type": "image/png",
///   "size": 204800,
///   "url": "https://cdn.kuayle.dev/assets/screenshot.png",
///   "created_at": "2026-07-28T15:58:38.119837+08:00"
/// }
/// ```
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct AssetResponse {
    pub id: String,
    pub filename: String,

    #[serde(default)]
    pub content_type: Option<String>,

    #[serde(default)]
    pub size: Option<u64>,

    #[serde(default)]
    pub url: Option<String>,

    #[serde(default)]
    pub created_at: Option<DateTime<Utc>>,
}

/// Multipart upload is handled at the resource layer via reqwest.
/// See `crates/kuayle-sdk/src/resources/assets.rs` for the upload builder.
/// 多部分上传在资源层通过 reqwest 处理。
/// 参见 `crates/kuayle-sdk/src/resources/assets.rs` 中的上传构建器。
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_asset_from_api() {
        let json = r#"{
            "id": "k0000000-0000-0000-0000-000000000001",
            "filename": "screenshot.png",
            "content_type": "image/png",
            "size": 204800,
            "url": "https://cdn.kuayle.dev/assets/screenshot.png",
            "created_at": "2026-07-28T15:58:38.119837+08:00"
        }"#;
        let asset: AssetResponse = serde_json::from_str(json).unwrap();
        assert_eq!(asset.id, "k0000000-0000-0000-0000-000000000001");
        assert_eq!(asset.filename, "screenshot.png");
        assert_eq!(asset.content_type.as_deref(), Some("image/png"));
        assert_eq!(asset.size, Some(204800));
        assert_eq!(
            asset.url.as_deref(),
            Some("https://cdn.kuayle.dev/assets/screenshot.png")
        );
        assert!(asset.created_at.is_some());
    }

    #[test]
    fn deserialize_asset_minimal() {
        let json = r#"{
            "id": "k0000000-0000-0000-0000-000000000002",
            "filename": "data.json"
        }"#;
        let asset: AssetResponse = serde_json::from_str(json).unwrap();
        assert_eq!(asset.filename, "data.json");
        assert!(asset.content_type.is_none());
        assert!(asset.size.is_none());
        assert!(asset.url.is_none());
        assert!(asset.created_at.is_none());
    }

    #[test]
    fn deserialize_asset_array() {
        let json = r#"[
            {
                "id": "k1",
                "filename": "a.png",
                "content_type": "image/png",
                "size": 100,
                "url": "https://cdn.example.com/a.png",
                "created_at": "2026-07-28T15:58:38.119837+08:00"
            },
            {
                "id": "k2",
                "filename": "b.pdf"
            }
        ]"#;
        let assets: Vec<AssetResponse> = serde_json::from_str(json).unwrap();
        assert_eq!(assets.len(), 2);
        assert_eq!(assets[0].filename, "a.png");
        assert_eq!(assets[1].filename, "b.pdf");
    }
}
