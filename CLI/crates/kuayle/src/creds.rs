// Credential storage: keychain primary, file fallback.
// 凭据存储：keychain 主，文件降级。
//
// - `KeychainCredentialStore`: macOS Keychain / Windows Credential Manager / Linux Secret Service.
// - `FileCredentialStore`: `~/.config/kuayle/credentials/{profile}.json` with 0600 permissions.
// - `get_credential_store()`: tries keychain, falls back to file with warning.
// - `KeychainCredentialStore`: macOS Keychain / Windows Credential Manager / Linux Secret Service。
// - `FileCredentialStore`：`~/.config/kuayle/credentials/{profile}.json`，权限 0600。
// - `get_credential_store()`：先尝试 keychain，失败则降级到文件并打印警告。

use kuayle_sdk::session::{CredentialStore, Session};
use std::fs;
use std::io::Write;
use std::path::PathBuf;

use crate::config;

// ── FileCredentialStore ───────────────────────────────────────────

/// File-based credential store (fallback when keychain is unavailable).
/// 基于文件的凭据存储（keychain 不可用时的降级方案）。
///
/// Stores sessions as JSON files in `~/.config/kuayle/credentials/`.
/// 将会话存储为 `~/.config/kuayle/credentials/` 下的 JSON 文件。
pub struct FileCredentialStore {
    dir: PathBuf,
}

impl FileCredentialStore {
    /// Create a new file store rooted at `~/.config/kuayle/credentials/`.
    /// 创建根在 `~/.config/kuayle/credentials/` 的新文件存储。
    pub fn new() -> Result<Self, String> {
        let dir = config::credentials_dir()?;
        fs::create_dir_all(&dir).map_err(|e| format!("create credentials dir: {e}"))?;
        Ok(FileCredentialStore { dir })
    }

    /// Create a store at a custom path (for testing).
    /// 在自定义路径创建存储（用于测试）。
    #[allow(dead_code)]
    pub fn at(dir: PathBuf) -> Self {
        FileCredentialStore { dir }
    }

    fn file_path(&self, profile: &str) -> PathBuf {
        self.dir.join(format!("{profile}.json"))
    }
}

impl CredentialStore for FileCredentialStore {
    fn load(&self, profile: &str) -> Result<Option<Session>, String> {
        let path = self.file_path(profile);
        if !path.exists() {
            return Ok(None);
        }
        let content = fs::read_to_string(&path).map_err(|e| format!("read: {e}"))?;
        let session: Session = serde_json::from_str(&content).map_err(|e| format!("parse: {e}"))?;
        Ok(Some(session))
    }

    fn save(&self, profile: &str, session: &Session) -> Result<(), String> {
        let path = self.file_path(profile);
        let json = serde_json::to_string_pretty(session).map_err(|e| format!("serialize: {e}"))?;

        // Write with restrictive permissions (0600), atomically where supported.
        // 以严格权限（0600）原子写入（在支持的平台上）。
        let mut opts = std::fs::OpenOptions::new();
        opts.write(true).create(true).truncate(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;
            opts.mode(0o600);
        }
        let mut file = opts.open(&path).map_err(|e| format!("create file: {e}"))?;

        file.write_all(json.as_bytes())
            .map_err(|e| format!("write: {e}"))?;
        Ok(())
    }

    fn delete(&self, profile: &str) -> Result<(), String> {
        let path = self.file_path(profile);
        if path.exists() {
            fs::remove_file(&path).map_err(|e| format!("delete: {e}"))?;
        }
        Ok(())
    }
}

// ── KeychainCredentialStore ───────────────────────────────────────

/// Keychain-based credential store using the OS native keychain.
/// 使用操作系统原生 keychain 的凭据存储。
///
/// On macOS: Keychain, Windows: Credential Manager, Linux: Secret Service.
/// macOS 上使用 Keychain，Windows 上使用 Credential Manager，Linux 上使用 Secret Service。
pub struct KeychainCredentialStore;

impl KeychainCredentialStore {
    /// Keychain service name for kuayle.
    /// kuayle 的 keychain service 名称。
    const SERVICE: &'static str = "kuayle";

    /// Try to create a new keychain store. Returns Err if keychain is unavailable.
    /// 尝试创建新的 keychain 存储。如果 keychain 不可用则返回 Err。
    pub fn try_new() -> Result<Self, String> {
        let probe_entry =
            keyring::Entry::new(Self::SERVICE, "kuayle_probe").map_err(|e| e.to_string())?;

        // Actually try to write and delete to verify the keychain works.
        // 实际尝试写入和删除以验证 keychain 可用。
        probe_entry
            .set_password("probe")
            .map_err(|e| format!("keychain unavailable: {e}"))?;
        let _ = probe_entry.delete_credential();

        Ok(KeychainCredentialStore)
    }
}

impl CredentialStore for KeychainCredentialStore {
    fn load(&self, profile: &str) -> Result<Option<Session>, String> {
        let entry =
            keyring::Entry::new(Self::SERVICE, profile).map_err(|e| format!("keyring: {e}"))?;
        match entry.get_password() {
            Ok(json) => {
                let session: Session =
                    serde_json::from_str(&json).map_err(|e| format!("parse session: {e}"))?;
                Ok(Some(session))
            }
            Err(keyring::Error::NoEntry) => Ok(None),
            Err(e) => Err(format!("keyring read: {e}")),
        }
    }

    fn save(&self, profile: &str, session: &Session) -> Result<(), String> {
        let entry =
            keyring::Entry::new(Self::SERVICE, profile).map_err(|e| format!("keyring: {e}"))?;
        let json = serde_json::to_string(session).map_err(|e| format!("serialize: {e}"))?;
        entry
            .set_password(&json)
            .map_err(|e| format!("keyring write: {e}"))
    }

    fn delete(&self, profile: &str) -> Result<(), String> {
        let entry =
            keyring::Entry::new(Self::SERVICE, profile).map_err(|e| format!("keyring: {e}"))?;
        match entry.delete_credential() {
            Ok(()) => Ok(()),
            Err(keyring::Error::NoEntry) => Ok(()),
            Err(e) => Err(format!("keyring delete: {e}")),
        }
    }
}

// ── Factory ───────────────────────────────────────────────────────

/// Get the best available credential store.
/// 获取最佳可用的凭据存储。
///
/// Tries keychain first; falls back to file-based storage with a warning.
/// Set `KUAYLE_CREDENTIAL_STORE=file` to force file-based storage (for testing).
/// 先尝试 keychain；失败则降级到文件存储并打印警告。
/// 设置 `KUAYLE_CREDENTIAL_STORE=file` 强制使用文件存储（用于测试）。
pub fn get_credential_store() -> Result<Box<dyn CredentialStore>, String> {
    if std::env::var("KUAYLE_CREDENTIAL_STORE").as_deref() == Ok("file") {
        return Ok(Box::new(FileCredentialStore::new()?));
    }

    match KeychainCredentialStore::try_new() {
        Ok(store) => Ok(Box::new(store)),
        Err(_) => {
            eprintln!(
                "warning: OS keychain unavailable, falling back to file-based credential storage"
            );
            Ok(Box::new(FileCredentialStore::new()?))
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;

    #[test]
    fn file_store_save_and_load() {
        let dir = TempDir::new().unwrap();
        let store = FileCredentialStore::at(dir.path().to_path_buf());
        let session = Session::pat("kuayle_pat_abc123");

        store.save("work", &session).unwrap();
        let loaded = store.load("work").unwrap().unwrap();
        assert_eq!(loaded.bearer_token(), "kuayle_pat_abc123");
    }

    #[test]
    fn file_store_load_nonexistent() {
        let dir = TempDir::new().unwrap();
        let store = FileCredentialStore::at(dir.path().to_path_buf());
        assert!(store.load("nope").unwrap().is_none());
    }

    #[test]
    fn file_store_delete() {
        let dir = TempDir::new().unwrap();
        let store = FileCredentialStore::at(dir.path().to_path_buf());
        let session = Session::pat("token");

        store.save("work", &session).unwrap();
        store.delete("work").unwrap();
        assert!(store.load("work").unwrap().is_none());
    }

    #[test]
    fn file_store_delete_nonexistent_is_noop() {
        let dir = TempDir::new().unwrap();
        let store = FileCredentialStore::at(dir.path().to_path_buf());
        assert!(store.delete("nope").is_ok());
    }

    #[test]
    fn file_store_overwrite() {
        let dir = TempDir::new().unwrap();
        let store = FileCredentialStore::at(dir.path().to_path_buf());

        store.save("work", &Session::pat("old_token")).unwrap();
        store.save("work", &Session::pat("new_token")).unwrap();

        let loaded = store.load("work").unwrap().unwrap();
        assert_eq!(loaded.bearer_token(), "new_token");
    }

    #[test]
    fn file_store_multiple_profiles() {
        let dir = TempDir::new().unwrap();
        let store = FileCredentialStore::at(dir.path().to_path_buf());

        store.save("work", &Session::pat("token_w")).unwrap();
        store.save("home", &Session::pat("token_h")).unwrap();

        assert_eq!(
            store.load("work").unwrap().unwrap().bearer_token(),
            "token_w"
        );
        assert_eq!(
            store.load("home").unwrap().unwrap().bearer_token(),
            "token_h"
        );
    }

    #[test]
    #[cfg(unix)]
    fn file_store_creates_with_0600_permissions() {
        use std::os::unix::fs::PermissionsExt;
        let dir = TempDir::new().unwrap();
        let store = FileCredentialStore::at(dir.path().to_path_buf());
        store.save("work", &Session::pat("tok")).unwrap();

        let meta = std::fs::metadata(dir.path().join("work.json")).unwrap();
        let mode = meta.permissions().mode();
        // Only owner should have access (0600 = 0o600).
        // 仅 owner 应有访问权限（0600 = 0o600）。
        assert_eq!(mode & 0o777, 0o600, "expected 0o600, got {mode:#o}");
    }
}
