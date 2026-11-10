// Configuration and profile loading.
// 配置与 profile 加载。
//
// Reads `~/.config/kuayle/config.toml` with profile-based settings.
// Each profile binds an instance URL and optional default workspace.
// 读取 `~/.config/kuayle/config.toml`，基于 profile 的设置。
// 每个 profile 绑定实例 URL 和可选的默认工作区。

use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::path::PathBuf;

/// Top-level config file structure.
/// 顶层配置文件结构。
///
/// ```toml
/// default_profile = "work"
///
/// [profiles.work]
/// url = "https://kuayle.example.com"
/// workspace = "acme"
/// ```
#[derive(Debug, Clone, Deserialize, Serialize, Default)]
pub struct Config {
    #[serde(default)]
    pub default_profile: Option<String>,

    #[serde(default)]
    pub profiles: BTreeMap<String, ProfileConfig>,
}

/// Per-profile configuration (non-sensitive).
/// 每个 profile 的配置（非敏感信息）。
///
/// Credentials are NOT stored here — they go to the keychain
/// via `CredentialStore`.
/// 凭据不在此存储 — 它们通过 `CredentialStore` 存入 keychain。
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct ProfileConfig {
    /// Base URL of the kuayle instance (e.g. `http://localhost:5173`).
    /// kuayle 实例的 base URL（如 `http://localhost:5173`）。
    pub url: String,

    /// Default workspace slug for this profile.
    /// 此 profile 的默认工作区 slug。
    #[serde(default)]
    pub workspace: Option<String>,
}

/// Resolved configuration for the current invocation.
/// 当前调用的已解析配置。
#[derive(Debug, Clone)]
pub struct ResolvedConfig {
    /// The active profile name.
    /// 活跃的 profile 名称。
    pub profile: String,

    /// The base URL of the kuayle instance.
    /// kuayle 实例的 base URL。
    pub url: String,

    /// The workspace slug (from profile, flag, or env).
    /// 工作区 slug（来自 profile、flag 或环境变量）。
    #[allow(dead_code)]
    pub workspace: Option<String>,
}

impl Config {
    /// Load config from the default path: `~/.config/kuayle/config.toml`.
    /// 从默认路径加载配置：`~/.config/kuayle/config.toml`。
    pub fn load() -> Result<Self, String> {
        let path = config_path()?;
        Self::load_from(&path)
    }

    /// Load config from a specific path (for testing).
    /// 从指定路径加载配置（用于测试）。
    pub fn load_from(path: &PathBuf) -> Result<Self, String> {
        if !path.exists() {
            return Ok(Config::default());
        }
        let content = std::fs::read_to_string(path).map_err(|e| format!("read config: {e}"))?;
        toml::from_str(&content).map_err(|e| format!("parse config: {e}"))
    }

    /// Load config from default path, falling back to default on any error.
    /// 从默认路径加载配置，任何错误时回退到默认值。
    pub fn load_or_default() -> Self {
        Self::load().unwrap_or_default()
    }

    /// Resolve the effective profile name.
    /// 解析生效的 profile 名称。
    ///
    /// Priority: `cli_profile` flag → `KUAYLE_PROFILE` env → `default_profile` field → `"default"`.
    /// 优先级：`cli_profile` flag → `KUAYLE_PROFILE` 环境变量 → `default_profile` 字段 → `"default"`。
    pub fn resolve_profile(&self, cli_profile: Option<&str>) -> String {
        if let Some(p) = cli_profile {
            return p.to_string();
        }
        if let Ok(p) = std::env::var("KUAYLE_PROFILE") {
            if !p.is_empty() {
                return p;
            }
        }
        self.default_profile
            .clone()
            .unwrap_or_else(|| "default".to_string())
    }

    /// Resolve the full configuration for the current invocation.
    /// 解析当前调用的完整配置。
    pub fn resolve(
        &self,
        cli_profile: Option<&str>,
        cli_url: Option<&str>,
        cli_workspace: Option<&str>,
    ) -> Result<ResolvedConfig, String> {
        let profile = self.resolve_profile(cli_profile);

        // URL: --url flag → KUAYLE_URL env → profile config.
        // URL：--url flag → KUAYLE_URL 环境变量 → profile 配置。
        let url = if let Some(u) = cli_url {
            u.to_string()
        } else if let Ok(u) = std::env::var("KUAYLE_URL") {
            u
        } else {
            self.profiles
                .get(&profile)
                .map(|p| p.url.clone())
                .ok_or_else(|| {
                    format!(
                        "profile '{profile}' not found in config. Run 'kuayle auth login' first."
                    )
                })?
        };

        // Workspace: --workspace flag → KUAYLE_WORKSPACE env → profile config.
        // 工作区：--workspace flag → KUAYLE_WORKSPACE 环境变量 → profile 配置。
        let workspace = if let Some(w) = cli_workspace {
            Some(w.to_string())
        } else if let Ok(w) = std::env::var("KUAYLE_WORKSPACE") {
            if !w.is_empty() {
                Some(w)
            } else {
                None
            }
        } else {
            self.profiles
                .get(&profile)
                .and_then(|p| p.workspace.clone())
        };

        Ok(ResolvedConfig {
            profile,
            url,
            workspace,
        })
    }
}

/// Compute the default config file path.
/// 计算默认配置文件路径。
pub fn config_path() -> Result<PathBuf, String> {
    let dir = config_dir()?;
    Ok(dir.join("config.toml"))
}

/// Compute the kuayle config directory: `~/.config/kuayle/`.
/// 计算 kuayle 配置目录：`~/.config/kuayle/`。
pub fn config_dir() -> Result<PathBuf, String> {
    dirs::config_dir()
        .map(|d| d.join("kuayle"))
        .ok_or_else(|| "could not determine config directory".to_string())
}

/// Compute the credential store directory: `~/.config/kuayle/credentials/`.
/// 计算凭据存储目录：`~/.config/kuayle/credentials/`。
pub fn credentials_dir() -> Result<PathBuf, String> {
    config_dir().map(|d| d.join("credentials"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;
    use tempfile::TempDir;

    fn write_config(dir: &TempDir, content: &str) -> PathBuf {
        let path = dir.path().join("config.toml");
        let mut f = std::fs::File::create(&path).unwrap();
        f.write_all(content.as_bytes()).unwrap();
        path
    }

    #[test]
    fn load_empty_config() {
        let dir = TempDir::new().unwrap();
        let path = write_config(&dir, "");
        let config = Config::load_from(&path).unwrap();
        assert!(config.default_profile.is_none());
        assert!(config.profiles.is_empty());
    }

    #[test]
    fn load_config_with_profiles() {
        let dir = TempDir::new().unwrap();
        let path = write_config(
            &dir,
            r#"
default_profile = "work"

[profiles.work]
url = "https://kuayle.work.com"
workspace = "acme"

[profiles.personal]
url = "http://localhost:5173"
"#,
        );
        let config = Config::load_from(&path).unwrap();
        assert_eq!(config.default_profile.as_deref(), Some("work"));
        assert_eq!(config.profiles.len(), 2);
        assert_eq!(config.profiles["work"].url, "https://kuayle.work.com");
        assert_eq!(config.profiles["work"].workspace.as_deref(), Some("acme"));
        assert_eq!(config.profiles["personal"].workspace, None);
    }

    #[test]
    fn missing_file_returns_default() {
        let path = PathBuf::from("/nonexistent/config.toml");
        let config = Config::load_from(&path).unwrap();
        assert!(config.default_profile.is_none());
    }

    // ── Profile resolution ────────────────────────────────────────

    #[test]
    fn resolve_profile_from_cli_flag() {
        let config = Config::default();
        assert_eq!(config.resolve_profile(Some("work")), "work");
    }

    #[test]
    fn resolve_profile_from_default_field() {
        let config = Config {
            default_profile: Some("personal".into()),
            ..Default::default()
        };
        assert_eq!(config.resolve_profile(None), "personal");
    }

    #[test]
    fn resolve_profile_fallback_to_default() {
        let config = Config::default();
        assert_eq!(config.resolve_profile(None), "default");
    }

    // ── Full resolution ───────────────────────────────────────────

    #[test]
    fn resolve_full_config_from_profile() {
        let mut profiles = BTreeMap::new();
        profiles.insert(
            "work".into(),
            ProfileConfig {
                url: "http://localhost:5173".into(),
                workspace: Some("acme".into()),
            },
        );

        let config = Config {
            default_profile: Some("work".into()),
            profiles,
        };

        let resolved = config.resolve(Some("work"), None, None).unwrap();
        assert_eq!(resolved.profile, "work");
        assert_eq!(resolved.url, "http://localhost:5173");
        assert_eq!(resolved.workspace.as_deref(), Some("acme"));
    }

    #[test]
    fn cli_url_overrides_profile() {
        let mut profiles = BTreeMap::new();
        profiles.insert(
            "work".into(),
            ProfileConfig {
                url: "http://localhost:5173".into(),
                workspace: None,
            },
        );

        let config = Config {
            default_profile: Some("work".into()),
            profiles,
        };

        let resolved = config
            .resolve(Some("work"), Some("https://other.example.com"), None)
            .unwrap();
        assert_eq!(resolved.url, "https://other.example.com");
    }

    #[test]
    fn cli_workspace_overrides_profile() {
        let mut profiles = BTreeMap::new();
        profiles.insert(
            "work".into(),
            ProfileConfig {
                url: "http://localhost:5173".into(),
                workspace: Some("acme".into()),
            },
        );

        let config = Config {
            default_profile: Some("work".into()),
            profiles,
        };

        let resolved = config
            .resolve(Some("work"), None, Some("side-project"))
            .unwrap();
        assert_eq!(resolved.workspace.as_deref(), Some("side-project"));
    }

    #[test]
    fn missing_profile_returns_error() {
        let config = Config::default();
        let err = config.resolve(Some("nonexistent"), None, None).unwrap_err();
        assert!(err.contains("nonexistent"));
    }
}
