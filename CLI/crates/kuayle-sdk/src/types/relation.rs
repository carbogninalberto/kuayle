// Relation types aligned with kuayle API.
// 与 kuayle API 对齐的关系类型。

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// A relation between two issues (e.g. blocks, related, duplicates).
/// 两个 issue 之间的关系（如 blocks、related、duplicates）。
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct RelationResponse {
    pub id: String,
    pub issue_id: String,
    pub related_issue_id: String,
    #[serde(rename = "type")]
    pub relation_type: String,
    pub created_at: DateTime<Utc>,
}

/// Request body for creating a relation.
/// 创建关系的请求体。
#[derive(Debug, Clone, Serialize)]
pub struct CreateRelationRequest {
    pub related_identifier: String,
    #[serde(rename = "type")]
    pub relation_type: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_relation() {
        let json = r#"{
            "id": "r1",
            "issue_id": "10000000-0000-0000-0000-000000000040",
            "related_issue_id": "10000000-0000-0000-0000-000000000009",
            "type": "blocks",
            "created_at": "2026-07-28T15:58:38+08:00"
        }"#;
        let r: RelationResponse = serde_json::from_str(json).unwrap();
        assert_eq!(r.relation_type, "blocks");
    }
}
