// Common types shared across kuayle resources.
// kuayle 各资源共享的通用类型。

use serde::{Deserialize, Serialize};

/// Nested progress counters shared by Project and Cycle responses.
/// Project 和 Cycle 响应共享的嵌套进度计数。
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct ProgressInfo {
    pub total: u64,
    pub completed: u64,
    pub cancelled: u64,
}

/// Unified paginated list response from kuayle.
/// kuayle 的统一分页列表响应。
///
/// Note: Not all list endpoints paginate (e.g. `/api/workspaces`
/// returns a plain array). This type is used for resources that do.
/// 注意：并非所有列表端点都分页（例如 `/api/workspaces` 返回纯数组）。
/// 此类型用于确实分页的资源。
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct ListResponse<T> {
    pub data: Vec<T>,
    pub total_count: u64,
    pub page: u32,
    pub per_page: u32,
    pub has_more: bool,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_list_response() {
        let json = r#"{
            "data": [1, 2, 3],
            "total_count": 42,
            "page": 1,
            "per_page": 100,
            "has_more": true
        }"#;
        let resp: ListResponse<i32> = serde_json::from_str(json).unwrap();
        assert_eq!(resp.data, vec![1, 2, 3]);
        assert_eq!(resp.total_count, 42);
        assert_eq!(resp.page, 1);
        assert_eq!(resp.per_page, 100);
        assert!(resp.has_more);
    }

    #[test]
    fn deserialize_list_response_empty() {
        let json = r#"{
            "data": [],
            "total_count": 0,
            "page": 1,
            "per_page": 100,
            "has_more": false
        }"#;
        let resp: ListResponse<String> = serde_json::from_str(json).unwrap();
        assert!(resp.data.is_empty());
        assert!(!resp.has_more);
    }
}
