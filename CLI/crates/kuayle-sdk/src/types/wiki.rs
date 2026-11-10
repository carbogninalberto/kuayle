// Wiki and registry types, mirroring BE/internal/dto/wiki.go and wiki_tree.go.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// A page as listed: everything but the content.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct WikiPageSummary {
    pub id: String,
    pub team_id: Option<String>,
    pub project_id: Option<String>,
    pub title: String,
    pub version: i64,
    pub updated_by: Option<String>,
    pub updated_by_name: Option<String>,
    /// The UTC day (YYYY-MM-DD) a registry page logs; None for documents.
    pub registry_date: Option<String>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

/// A full page. `content` is sanitized editor HTML.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct WikiPage {
    #[serde(flatten)]
    pub summary: WikiPageSummary,
    pub content: String,
    pub created_by: Option<String>,
}

/// Create a page at the top level of the wiki. Content may be HTML or
/// Markdown; the server converts and sanitizes it.
#[derive(Debug, Clone, Serialize)]
pub struct CreateWikiPageRequest {
    pub title: String,
    pub content: String,
}

/// Create a page ("page") or folder ("folder"), optionally under a parent.
#[derive(Debug, Clone, Serialize)]
pub struct CreateWikiTreePageRequest {
    pub title: String,
    pub content: String,
    pub kind: String,
    pub parent_id: Option<String>,
}

/// Replace the fields that are present. `version` is the version the edit
/// started from; the server rejects a stale version with 409.
#[derive(Debug, Clone, Serialize)]
pub struct UpdateWikiPageRequest {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub title: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub content: Option<String>,
    pub version: i64,
}

/// What one save (or a run of saves by one person) changed.
/// `version` is the page version the save produced: `new_*` is the page at
/// that version and `old_*` the page just before it.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct WikiRevision {
    pub id: String,
    pub version: i64,
    pub old_title: String,
    pub new_title: String,
    pub old_content: String,
    pub new_content: String,
    pub edited_by: Option<String>,
    pub edited_by_name: Option<String>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

/// One page's place in the wiki tree.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct WikiTreeNode {
    pub id: String,
    pub parent_id: Option<String>,
    pub position: i64,
    /// "page" or "folder".
    pub kind: String,
}

/// Move a page under `parent_id` (None for the top level) at `index` among
/// its new siblings, counted without the page.
#[derive(Debug, Clone, Serialize)]
pub struct MoveWikiPageRequest {
    pub parent_id: Option<String>,
    pub index: i64,
}

/// One plain-text entry for today's (UTC) registry page.
#[derive(Debug, Clone, Serialize)]
pub struct RegistryEntryRequest {
    pub text: String,
}

/// One day of the registry feed.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct RegistryDay {
    pub date: String,
    pub page_id: String,
    pub title: String,
    pub content: String,
    pub version: i64,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct RegistryFeed {
    pub days: Vec<RegistryDay>,
    /// Pass as `before` to fetch the next, older page of days.
    pub next_before: Option<String>,
}

/// An AI overview of a registry period.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct RegistryOverview {
    pub id: String,
    pub period_start: String,
    pub period_end: String,
    pub content: String,
    pub generated_at: DateTime<Utc>,
    pub generated_by_name: Option<String>,
    pub trigger: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct RegistryOverviewList {
    pub overviews: Vec<RegistryOverview>,
    pub next_before: Option<String>,
}

/// Generate an overview from `period_start` to `period_end` (YYYY-MM-DD,
/// inclusive, at most 31 days).
#[derive(Debug, Clone, Serialize)]
pub struct GenerateOverviewRequest {
    pub period_start: String,
    pub period_end: String,
}

/// A wiki's settings; the overview schedule is kept as JSON.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct WikiSettings {
    pub registry_enabled: bool,
    pub overview_schedule: serde_json::Value,
    pub overview_timezone: String,
    pub overview_next_run_at: Option<DateTime<Utc>>,
    pub overview_last_run_at: Option<DateTime<Utc>>,
    pub overview_last_error: Option<String>,
}

/// Change the settings that are present.
#[derive(Debug, Clone, Default, Serialize)]
pub struct UpdateWikiSettingsRequest {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub registry_enabled: Option<bool>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn page_deserializes_flattened_summary() {
        let page: WikiPage = serde_json::from_value(serde_json::json!({
            "id": "p1", "team_id": "t1", "project_id": null, "title": "Runbook",
            "version": 3, "updated_by": null, "updated_by_name": "Ada",
            "registry_date": null, "created_at": "2026-11-01T10:00:00Z",
            "updated_at": "2026-11-02T10:00:00Z", "content": "<p>hi</p>", "created_by": "u1"
        }))
        .unwrap();
        assert_eq!(page.summary.title, "Runbook");
        assert_eq!(page.summary.version, 3);
        assert_eq!(page.content, "<p>hi</p>");
    }

    #[test]
    fn update_omits_unchanged_fields() {
        let body = serde_json::to_value(UpdateWikiPageRequest {
            title: None,
            content: Some("x".into()),
            version: 2,
        })
        .unwrap();
        assert_eq!(body, serde_json::json!({"content": "x", "version": 2}));
    }
}
