// Name resolution with memo and disk cache.
#![allow(dead_code)]

use kuayle_sdk::client::Client;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::Mutex;
use std::time::SystemTime;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum ResolveKind {
    Teams,
    Labels,
    Members,
    Projects,
    Cycles,
}

impl ResolveKind {
    fn api_path(&self, workspace: &str) -> String {
        match self {
            ResolveKind::Teams => format!("/api/workspaces/{workspace}/teams"),
            ResolveKind::Labels => format!("/api/workspaces/{workspace}/labels"),
            ResolveKind::Members => format!("/api/workspaces/{workspace}/members"),
            ResolveKind::Projects => format!("/api/workspaces/{workspace}/projects"),
            ResolveKind::Cycles => format!("/api/workspaces/{workspace}/teams"),
        }
    }
    fn cache_file_name(&self) -> &str {
        match self {
            ResolveKind::Teams => "teams",
            ResolveKind::Labels => "labels",
            ResolveKind::Members => "members",
            ResolveKind::Projects => "projects",
            ResolveKind::Cycles => "cycles",
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct CacheEntry {
    mapping: HashMap<String, String>,
    cached_at: u64,
}

const CACHE_TTL_SECS: u64 = 300;

pub struct Resolver {
    client: Client,
    workspace: String,
    memo: Mutex<HashMap<ResolveKind, HashMap<String, String>>>,
    no_cache: bool,
}

impl Resolver {
    pub fn new(client: Client, workspace: &str, no_cache: bool) -> Self {
        Resolver {
            client,
            workspace: workspace.to_string(),
            memo: Mutex::new(HashMap::new()),
            no_cache,
        }
    }
    pub fn client(&self) -> &Client {
        &self.client
    }
    pub fn workspace(&self) -> &str {
        &self.workspace
    }

    pub async fn resolve(&self, kind: ResolveKind, name: &str) -> Result<String, String> {
        if is_uuid(name) {
            return Ok(name.to_string());
        }
        if let Some(id) = self.memo_get(kind, name) {
            return Ok(id);
        }
        if !self.no_cache {
            if let Some(id) = self.load_from_disk_cache(kind, name) {
                self.memo_insert(kind, name, &id);
                return Ok(id);
            }
        }
        let mapping = self.fetch_all(kind).await?;
        let id = match_name(&mapping, name)?;
        Ok(id)
    }

    async fn fetch_all(&self, kind: ResolveKind) -> Result<HashMap<String, String>, String> {
        let path = kind.api_path(&self.workspace);
        let items: Vec<serde_json::Value> = self
            .client
            .get(&path)
            .await
            .map_err(|e| format!("fetch {kind:?}: {e}"))?;
        let mut mapping = HashMap::new();
        for item in &items {
            let id = item["id"].as_str().or_else(|| item["user_id"].as_str());
            let name = item["name"].as_str();
            if let (Some(id), Some(name)) = (id, name) {
                mapping.insert(name.to_string(), id.to_string());
                mapping.insert(name.to_lowercase(), id.to_string());
                if kind == ResolveKind::Teams {
                    if let Some(key) = item["key"].as_str() {
                        mapping.insert(key.to_string(), id.to_string());
                        mapping.insert(key.to_lowercase(), id.to_string());
                    }
                }
                // Index members by email too / 成员也按 email 索引
                if kind == ResolveKind::Members {
                    if let Some(email) = item["email"].as_str() {
                        mapping.insert(email.to_string(), id.to_string());
                        mapping.insert(email.to_lowercase(), id.to_string());
                    }
                }
            }
        }
        let mut memo = self.memo.lock().unwrap();
        memo.insert(kind, mapping.clone());
        if !self.no_cache {
            let _ = self.save_to_disk_cache(kind, &mapping);
        }
        Ok(mapping)
    }

    fn memo_get(&self, kind: ResolveKind, name: &str) -> Option<String> {
        self.memo
            .lock()
            .unwrap()
            .get(&kind)
            .and_then(|m| m.get(&name.to_lowercase()).cloned())
    }
    fn memo_insert(&self, kind: ResolveKind, name: &str, id: &str) {
        self.memo
            .lock()
            .unwrap()
            .entry(kind)
            .or_default()
            .insert(name.to_lowercase(), id.to_string());
    }

    fn cache_path(&self, kind: ResolveKind) -> Option<PathBuf> {
        let dir = dirs::cache_dir()?;
        let url_str = self.client.base_url().to_string();
        let hash = hash_url(&url_str);
        Some(
            dir.join("kuayle")
                .join("resolve")
                .join(&hash)
                .join(&self.workspace)
                .join(kind.cache_file_name())
                .with_extension("json"),
        )
    }
    fn load_from_disk_cache(&self, kind: ResolveKind, name: &str) -> Option<String> {
        let path = self.cache_path(kind)?;
        if !path.exists() {
            return None;
        }
        let content = std::fs::read_to_string(&path).ok()?;
        let entry: CacheEntry = serde_json::from_str(&content).ok()?;
        let age = SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_secs()
            - entry.cached_at;
        if age > CACHE_TTL_SECS {
            return None;
        }
        entry.mapping.get(&name.to_lowercase()).cloned()
    }
    fn save_to_disk_cache(
        &self,
        kind: ResolveKind,
        mapping: &HashMap<String, String>,
    ) -> Result<(), String> {
        let path = match self.cache_path(kind) {
            Some(p) => p,
            None => return Ok(()),
        };
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent).map_err(|e| format!("mkdir: {e}"))?;
        }
        let entry = CacheEntry {
            mapping: mapping.clone(),
            cached_at: SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_secs(),
        };
        std::fs::write(
            &path,
            serde_json::to_string(&entry).map_err(|e| format!("serialize: {e}"))?,
        )
        .map_err(|e| format!("write: {e}"))
    }
}

fn is_uuid(s: &str) -> bool {
    s.len() >= 32 && s.chars().all(|c| c.is_ascii_hexdigit() || c == '-') && s.contains('-')
}

fn match_name(mapping: &HashMap<String, String>, name: &str) -> Result<String, String> {
    let lower = name.to_lowercase();
    if let Some(id) = mapping.get(&lower) {
        return Ok(id.clone());
    }
    let candidates: Vec<&String> = mapping
        .iter()
        .filter(|(k, _)| k.to_lowercase().contains(&lower))
        .map(|(k, _)| k)
        .take(5)
        .collect();
    if candidates.is_empty() {
        Err(format!("'{name}' not found"))
    } else {
        Err(format!(
            "'{name}' not found. Candidates: {}",
            candidates
                .iter()
                .map(|s| s.as_str())
                .collect::<Vec<_>>()
                .join(", ")
        ))
    }
}

fn hash_url(url: &str) -> String {
    use std::hash::{DefaultHasher, Hash, Hasher};
    let mut h = DefaultHasher::new();
    url.hash(&mut h);
    format!("{:016x}", h.finish())
}
