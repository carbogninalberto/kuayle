// Session and credential store abstraction.
// 会话与凭据存储抽象。
//
// kuayle supports two auth modes:
// - PAT (Personal Access Token): long-lived, no refresh.
// - JWT (password login): short-lived access + refresh token rotation.
//
// For M0/M1, we implement PAT-first. JWT support is stubbed.
// kuayle 支持两种认证模式：
// - PAT（个人访问令牌）：长期有效，无需刷新。
// - JWT（密码登录）：短期访问令牌 + 刷新令牌轮换。
//
// M0/M1 阶段以 PAT 优先实现。JWT 支持留桩。

use serde::{Deserialize, Serialize};

/// An authenticated session with a kuayle instance.
/// 与 kuayle 实例的已认证会话。
///
/// For PAT sessions, `token` is the full `kuayle_pat_...` string.
/// JWT sessions (future) carry access + refresh tokens with expiry.
/// 对于 PAT 会话，`token` 是完整的 `kuayle_pat_...` 字符串。
/// JWT 会话（未来）携带 access + refresh token 及过期时间。
#[derive(Clone, Serialize, Deserialize)]
pub enum Session {
    /// Personal Access Token authentication.
    /// 个人访问令牌认证。
    Pat {
        /// The full PAT string (`kuayle_pat_...`).
        /// 完整的 PAT 字符串（`kuayle_pat_...`）。
        token: String,
    },
}

impl std::fmt::Debug for Session {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Session::Pat { token } => {
                let prefix: String = token.chars().take(16).collect();
                write!(f, "Pat {{ token: \"{prefix}...\" }}")
            }
        }
    }
}

impl Session {
    /// Create a new PAT session from a token string.
    /// 从 token 字符串创建新的 PAT 会话。
    pub fn pat(token: impl Into<String>) -> Self {
        Session::Pat {
            token: token.into(),
        }
    }

    /// Return the Bearer token for HTTP Authorization header.
    /// 返回用于 HTTP Authorization 头的 Bearer token。
    pub fn bearer_token(&self) -> &str {
        match self {
            Session::Pat { token } => token,
        }
    }

    /// Whether the session uses a PAT (as opposed to JWT).
    /// 会话是否使用 PAT（相对于 JWT）。
    pub fn is_pat(&self) -> bool {
        matches!(self, Session::Pat { .. })
    }
}

/// Abstraction for persisting and loading sessions.
/// 持久化和加载会话的抽象。
///
/// Implementations: keychain (primary), file-based (fallback),
/// in-memory (for testing).
/// 实现：keychain（主）、文件（降级）、内存（测试用）。
pub trait CredentialStore: Send + Sync {
    /// Load a session for the given profile name.
    /// 加载指定 profile 名称的会话。
    fn load(&self, profile: &str) -> Result<Option<Session>, String>;

    /// Save a session for the given profile name.
    /// 保存指定 profile 名称的会话。
    fn save(&self, profile: &str, session: &Session) -> Result<(), String>;

    /// Delete the session for the given profile name.
    /// 删除指定 profile 名称的会话。
    fn delete(&self, profile: &str) -> Result<(), String>;
}

/// In-memory credential store for testing.
/// 用于测试的内存凭据存储。
#[derive(Default)]
pub struct MemoryStore {
    sessions: std::sync::Mutex<std::collections::HashMap<String, Session>>,
}

impl CredentialStore for MemoryStore {
    fn load(&self, profile: &str) -> Result<Option<Session>, String> {
        let guard = self
            .sessions
            .lock()
            .map_err(|e| format!("lock error: {e}"))?;
        Ok(guard.get(profile).cloned())
    }

    fn save(&self, profile: &str, session: &Session) -> Result<(), String> {
        let mut guard = self
            .sessions
            .lock()
            .map_err(|e| format!("lock error: {e}"))?;
        guard.insert(profile.to_string(), session.clone());
        Ok(())
    }

    fn delete(&self, profile: &str) -> Result<(), String> {
        let mut guard = self
            .sessions
            .lock()
            .map_err(|e| format!("lock error: {e}"))?;
        guard.remove(profile);
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // ── Session ────────────────────────────────────────────────────

    #[test]
    fn session_pat_bearer_token() {
        let session = Session::pat("kuayle_pat_abc123");
        assert_eq!(session.bearer_token(), "kuayle_pat_abc123");
    }

    #[test]
    fn session_pat_is_pat_returns_true() {
        let session = Session::pat("kuayle_pat_xyz");
        assert!(session.is_pat());
    }

    #[test]
    fn session_pat_roundtrip_json() {
        let session = Session::pat("kuayle_pat_Kk3mNp9Qr2Wx5Yv8");
        let json = serde_json::to_string(&session).unwrap();
        let restored: Session = serde_json::from_str(&json).unwrap();
        assert_eq!(restored.bearer_token(), session.bearer_token());
    }

    // ── MemoryStore ────────────────────────────────────────────────

    #[test]
    fn memory_store_load_nonexistent() {
        let store = MemoryStore::default();
        let result = store.load("work").unwrap();
        assert!(result.is_none());
    }

    #[test]
    fn memory_store_save_and_load() {
        let store = MemoryStore::default();
        let session = Session::pat("kuayle_pat_test");
        store.save("work", &session).unwrap();

        let loaded = store.load("work").unwrap().unwrap();
        assert_eq!(loaded.bearer_token(), "kuayle_pat_test");
    }

    #[test]
    fn memory_store_delete() {
        let store = MemoryStore::default();
        let session = Session::pat("kuayle_pat_test");
        store.save("work", &session).unwrap();
        store.delete("work").unwrap();

        assert!(store.load("work").unwrap().is_none());
    }

    #[test]
    fn memory_store_multiple_profiles() {
        let store = MemoryStore::default();
        store.save("work", &Session::pat("token_work")).unwrap();
        store
            .save("personal", &Session::pat("token_personal"))
            .unwrap();

        assert_eq!(
            store.load("work").unwrap().unwrap().bearer_token(),
            "token_work"
        );
        assert_eq!(
            store.load("personal").unwrap().unwrap().bearer_token(),
            "token_personal"
        );
    }

    #[test]
    fn memory_store_overwrite() {
        let store = MemoryStore::default();
        store.save("work", &Session::pat("old_token")).unwrap();
        store.save("work", &Session::pat("new_token")).unwrap();

        assert_eq!(
            store.load("work").unwrap().unwrap().bearer_token(),
            "new_token"
        );
    }
}
