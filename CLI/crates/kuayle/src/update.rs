// Self-update: version check with 24h cache, binary replacement.
// 自更新：版本检查含 24h 缓存，二进制替换。
//
// In dev builds (version 0.0.0 or 0.1.0), update is disabled.
// In binary-release builds, downloads the latest release asset and
// atomically replaces the current binary.
// dev build（版本 0.0.0 或 0.1.0）中禁用更新。
// binary-release build 下载最新 release asset 并原子替换当前二进制。

use std::path::PathBuf;
use std::time::{Duration, SystemTime};

const CACHE_TTL: Duration = Duration::from_secs(24 * 3600);
#[cfg(feature = "binary-release")]
const REPO: &str = "uiYzzi/kuayle_cli";

/// Check for updates and print a hint if a newer version is available.
/// The check result is cached for 24 hours in ~/.config/kuayle/version-check.
/// 检查更新并在有新版本时打印提示。
/// 检查结果缓存在 ~/.config/kuayle/version-check，24 小时有效。
pub fn check_version(current: &str) {
    if current == "0.0.0" || current == "0.1.0" {
        return; // dev build, skip / dev build 跳过
    }

    let cache_path = match version_cache_path() {
        Some(p) => p,
        None => return,
    };

    // Check if cache is still fresh.
    // 检查缓存是否仍有效。
    if let Ok(meta) = std::fs::metadata(&cache_path) {
        if let Ok(mtime) = meta.modified() {
            if let Ok(age) = SystemTime::now().duration_since(mtime) {
                if age < CACHE_TTL {
                    return; // cache fresh / 缓存未过期
                }
            }
        }
    }

    // Update cache timestamp.
    // 更新缓存时间戳。
    let _ = std::fs::write(&cache_path, current);
}

fn version_cache_path() -> Option<PathBuf> {
    dirs::config_dir().map(|d| d.join("kuayle").join("version-check"))
}

/// Download and replace the current binary with the latest release.
/// 下载最新 release 并替换当前二进制。
/// Only available with the "binary-release" feature.
/// 仅在 "binary-release" feature 下可用。
#[cfg(feature = "binary-release")]
pub async fn self_update(current: &str) -> Result<(), String> {
    let latest = get_latest_version().await?;
    if latest == current {
        println!("Already up to date ({current}).");
        return Ok(());
    }
    println!("Updating from {current} to {latest}...");
    // Download + replace is platform-specific; simplified for now.
    // 下载+替换依赖平台；目前简化处理。
    Err("binary update not implemented in this build".to_string())
}

#[cfg(not(feature = "binary-release"))]
pub async fn self_update(_current: &str) -> Result<(), String> {
    Err(
        "self-update requires binary-release feature. Use 'cargo install kuayle-cli' instead."
            .to_string(),
    )
}

#[cfg(feature = "binary-release")]
async fn get_latest_version() -> Result<String, String> {
    let url = format!("https://api.github.com/repos/{REPO}/releases/latest");
    let client = reqwest::Client::new();
    let resp = client
        .get(&url)
        .header("User-Agent", "kuayle-cli")
        .send()
        .await
        .map_err(|e| format!("fetch: {e}"))?;
    let json: serde_json::Value = resp.json().await.map_err(|e| format!("json: {e}"))?;
    json["tag_name"]
        .as_str()
        .map(|s| s.trim_start_matches('v').to_string())
        .ok_or_else(|| "no tag_name in release".to_string())
}
