// Wiki and registry endpoints for one team or project wiki.
//
// Routes: /api/workspaces/{ws}/teams/{team_id}/wiki/...
//         /api/workspaces/{ws}/projects/{project_id}/wiki/...
// Reads need the wiki:read token scope and writes wiki:write; the server
// still applies the owner's workspace role and team visibility.

use url::form_urlencoded;

use crate::client::Client;
use crate::error::KuayleError;
use crate::types::wiki::{
    CreateWikiPageRequest, CreateWikiTreePageRequest, GenerateOverviewRequest, MoveWikiPageRequest,
    RegistryEntryRequest, RegistryFeed, RegistryOverview, RegistryOverviewList,
    UpdateWikiPageRequest, UpdateWikiSettingsRequest, WikiPage, WikiPageSummary, WikiRevision,
    WikiSettings, WikiTreeNode,
};

/// Which wiki: a team's or a project's. Holds the resolved UUID.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum WikiScope {
    Team(String),
    Project(String),
}

/// Typed access to one wiki.
#[derive(Clone)]
pub struct Wiki {
    client: Client,
    base: String,
}

fn query(pairs: &[(&str, Option<String>)]) -> String {
    let mut serializer = form_urlencoded::Serializer::new(String::new());
    let mut any = false;
    for (key, value) in pairs {
        if let Some(value) = value {
            serializer.append_pair(key, value);
            any = true;
        }
    }
    if any {
        format!("?{}", serializer.finish())
    } else {
        String::new()
    }
}

impl Wiki {
    pub fn new(client: &Client, workspace: &str, scope: &WikiScope) -> Self {
        let base = match scope {
            WikiScope::Team(id) => format!("/api/workspaces/{workspace}/teams/{id}/wiki"),
            WikiScope::Project(id) => format!("/api/workspaces/{workspace}/projects/{id}/wiki"),
        };
        Wiki {
            client: client.clone(),
            base,
        }
    }

    /// The wiki's base path, e.g. `/api/workspaces/acme/teams/<id>/wiki`.
    pub fn base_path(&self) -> &str {
        &self.base
    }

    fn path(&self, suffix: &str) -> String {
        format!("{}{}", self.base, suffix)
    }

    /// Every page, including registry days (which carry a `registry_date`).
    pub async fn list(&self) -> Result<Vec<WikiPageSummary>, KuayleError> {
        self.client.get(&self.base).await
    }

    /// Where each document sits. Registry days are not part of the tree.
    pub async fn tree(&self) -> Result<Vec<WikiTreeNode>, KuayleError> {
        self.client.get(&self.path("/tree")).await
    }

    pub async fn get(&self, page_id: &str) -> Result<WikiPage, KuayleError> {
        self.client.get(&self.path(&format!("/{page_id}"))).await
    }

    /// Create a page at the top level.
    pub async fn create(&self, req: &CreateWikiPageRequest) -> Result<WikiPage, KuayleError> {
        self.client.post(&self.base, req).await
    }

    /// Create a page or folder placed last under an optional parent.
    pub async fn create_in_tree(
        &self,
        req: &CreateWikiTreePageRequest,
    ) -> Result<WikiPage, KuayleError> {
        self.client.post(&self.path("/tree"), req).await
    }

    pub async fn update(
        &self,
        page_id: &str,
        req: &UpdateWikiPageRequest,
    ) -> Result<WikiPage, KuayleError> {
        self.client
            .patch(&self.path(&format!("/{page_id}")), req)
            .await
    }

    pub async fn delete(&self, page_id: &str) -> Result<serde_json::Value, KuayleError> {
        self.client.delete(&self.path(&format!("/{page_id}"))).await
    }

    /// Move a page and return the new tree.
    pub async fn move_page(
        &self,
        page_id: &str,
        req: &MoveWikiPageRequest,
    ) -> Result<Vec<WikiTreeNode>, KuayleError> {
        self.client
            .post(&self.path(&format!("/{page_id}/move")), req)
            .await
    }

    /// Up to the 100 most recent revisions, newest first.
    pub async fn revisions(&self, page_id: &str) -> Result<Vec<WikiRevision>, KuayleError> {
        self.client
            .get(&self.path(&format!("/{page_id}/versions")))
            .await
    }

    /// Set the page back to a revision, as the web app's history dialog does:
    /// an ordinary update that writes the revision's title and content at
    /// the page's current version, so it is itself recorded in history and
    /// fails with 409 if someone saved in between.
    ///
    /// `before` restores the state just before that revision instead of the
    /// state it produced.
    pub async fn restore(
        &self,
        page_id: &str,
        version: i64,
        before: bool,
    ) -> Result<WikiPage, KuayleError> {
        let revisions = self.revisions(page_id).await?;
        let revision = revisions
            .iter()
            .find(|r| r.version == version)
            .ok_or_else(|| KuayleError::NotFound {
                message: format!(
                    "revision {version} of wiki page {page_id} is not in its last 100 revisions"
                ),
            })?;
        let current = self.get(page_id).await?;
        let (title, content) = if before {
            (&revision.old_title, &revision.old_content)
        } else {
            (&revision.new_title, &revision.new_content)
        };
        let req = UpdateWikiPageRequest {
            title: Some(title.clone()),
            content: Some(content.clone()),
            version: current.summary.version,
        };
        self.update(page_id, &req).await
    }

    pub async fn settings(&self) -> Result<WikiSettings, KuayleError> {
        self.client.get(&self.path("/settings")).await
    }

    /// Change settings, e.g. turn the registry on or off (wiki:write).
    pub async fn update_settings(
        &self,
        req: &UpdateWikiSettingsRequest,
    ) -> Result<WikiSettings, KuayleError> {
        self.client.patch(&self.path("/settings"), req).await
    }

    /// Add a plain-text entry to today's (UTC) registry page.
    pub async fn append_registry(&self, text: &str) -> Result<WikiPage, KuayleError> {
        let req = RegistryEntryRequest {
            text: text.to_string(),
        };
        self.client.post(&self.path("/registry"), &req).await
    }

    /// Registry days, newest first. `before` is an exclusive YYYY-MM-DD,
    /// `limit` 1..=31 (server default 7).
    pub async fn registry_feed(
        &self,
        before: Option<&str>,
        limit: Option<u32>,
    ) -> Result<RegistryFeed, KuayleError> {
        let q = query(&[
            ("before", before.map(str::to_string)),
            ("limit", limit.map(|l| l.to_string())),
        ]);
        self.client.get(&self.path(&format!("/registry{q}"))).await
    }

    /// Stored AI overviews, newest first. `before` is an RFC 3339 cursor,
    /// `limit` 1..=50 (server default 10).
    pub async fn overviews(
        &self,
        before: Option<&str>,
        limit: Option<u32>,
    ) -> Result<RegistryOverviewList, KuayleError> {
        let q = query(&[
            ("before", before.map(str::to_string)),
            ("limit", limit.map(|l| l.to_string())),
        ]);
        self.client
            .get(&self.path(&format!("/registry/overviews{q}")))
            .await
    }

    /// Summarise a period of the registry with the workspace's AI model.
    pub async fn generate_overview(
        &self,
        req: &GenerateOverviewRequest,
    ) -> Result<RegistryOverview, KuayleError> {
        self.client
            .post(&self.path("/registry/overviews"), req)
            .await
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn query_encodes_only_present_values() {
        assert_eq!(query(&[("before", None), ("limit", None)]), "");
        assert_eq!(
            query(&[
                ("before", Some("2026-11-01T00:00:00+01:00".into())),
                ("limit", Some("5".into()))
            ]),
            "?before=2026-11-01T00%3A00%3A00%2B01%3A00&limit=5"
        );
    }

    #[test]
    fn base_path_follows_scope() {
        let client = Client::new("http://localhost".parse().unwrap(), "t".into());
        assert_eq!(
            Wiki::new(&client, "acme", &WikiScope::Team("t1".into())).base_path(),
            "/api/workspaces/acme/teams/t1/wiki"
        );
        assert_eq!(
            Wiki::new(&client, "acme", &WikiScope::Project("p1".into())).base_path(),
            "/api/workspaces/acme/projects/p1/wiki"
        );
    }
}
