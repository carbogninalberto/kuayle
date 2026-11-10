// Wiremock integration tests for the SDK Client.
// SDK Client 的 wiremock 集成测试。
//
// Tests: basic GET/POST/DELETE, error mapping (all error codes),
// request headers (auth, user-agent).
// 测试：基本 GET/POST/DELETE、错误映射（全部错误码）、请求头（认证、user-agent）。

use kuayle_sdk::client::Client;
use kuayle_sdk::error::KuayleError;
use kuayle_sdk::types::user::UserResponse;
use serde_json::json;
use url::Url;
use wiremock::matchers::{header, method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

/// Helper: create a Client pointed at a mock server.
/// 辅助函数：创建指向 mock server 的 Client。
async fn test_client(server: &MockServer) -> Client {
    let base_url = Url::parse(&server.uri()).unwrap();
    Client::new(base_url, "kuayle_pat_test_token".into())
}

/// Helper: standard user JSON for mocking `/api/auth/me`.
/// 辅助函数：模拟 `/api/auth/me` 的标准用户 JSON。
fn user_json() -> serde_json::Value {
    json!({
        "id": "a0000000-0000-0000-0000-000000000001",
        "email": "alice@kuayle.dev",
        "name": "Alice Chen",
        "display_name": "Alice",
        "avatar_url": null,
        "is_sysadmin": true
    })
}

// ── GET tests ─────────────────────────────────────────────────────

#[tokio::test]
async fn get_deserializes_success_response() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .mount(&server)
        .await;

    let user: UserResponse = client.get("/api/auth/me").await.unwrap();
    assert_eq!(user.email, "alice@kuayle.dev");
    assert_eq!(user.name, "Alice Chen");
}

#[tokio::test]
async fn get_sends_auth_header() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .and(header("Authorization", "Bearer kuayle_pat_test_token"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .mount(&server)
        .await;

    let result: Result<UserResponse, _> = client.get("/api/auth/me").await;
    assert!(result.is_ok());
}

#[tokio::test]
async fn get_sends_user_agent() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .and(header(
            "User-Agent",
            concat!("kuayle-sdk/", env!("CARGO_PKG_VERSION")),
        ))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .mount(&server)
        .await;

    let result: Result<UserResponse, _> = client.get("/api/auth/me").await;
    assert!(result.is_ok());
}

// ── POST tests ────────────────────────────────────────────────────

#[tokio::test]
async fn post_sends_json_body() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("POST"))
        .and(path("/api/auth/login"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .mount(&server)
        .await;

    let body = json!({"email": "alice@kuayle.dev", "password": "secret123456"});
    let user: UserResponse = client.post("/api/auth/login", &body).await.unwrap();
    assert_eq!(user.email, "alice@kuayle.dev");
}

// ── Error mapping tests ───────────────────────────────────────────

#[tokio::test]
async fn error_401_unauthorized_maps_to_authentication() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(401).set_body_json(json!({
            "error": {
                "code": "UNAUTHORIZED",
                "message": "Authentication required"
            }
        })))
        .mount(&server)
        .await;

    let err = client
        .get::<UserResponse>("/api/auth/me")
        .await
        .unwrap_err();
    assert!(matches!(err, KuayleError::Authentication { .. }));
    assert_eq!(err.exit_code(), 2);
}

#[tokio::test]
async fn error_403_forbidden_maps_to_forbidden() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("GET"))
        .and(path("/api/workspaces/acme"))
        .respond_with(ResponseTemplate::new(403).set_body_json(json!({
            "error": {
                "code": "FORBIDDEN",
                "message": "Access denied"
            }
        })))
        .mount(&server)
        .await;

    let err = client
        .get::<serde_json::Value>("/api/workspaces/acme")
        .await
        .unwrap_err();
    assert!(matches!(err, KuayleError::Forbidden { .. }));
    assert_eq!(err.exit_code(), 5);
}

#[tokio::test]
async fn error_404_not_found_maps_to_not_found() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("GET"))
        .and(path("/api/workspaces/nonexistent"))
        .respond_with(ResponseTemplate::new(404).set_body_json(json!({
            "error": {
                "code": "NOT_FOUND",
                "message": "Workspace not found"
            }
        })))
        .mount(&server)
        .await;

    let err = client
        .get::<serde_json::Value>("/api/workspaces/nonexistent")
        .await
        .unwrap_err();
    assert!(matches!(err, KuayleError::NotFound { .. }));
    assert_eq!(err.exit_code(), 3);
}

#[tokio::test]
async fn error_400_validation_maps_to_validation() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("POST"))
        .and(path("/api/auth/login"))
        .respond_with(ResponseTemplate::new(400).set_body_json(json!({
            "error": {
                "code": "VALIDATION_ERROR",
                "message": "Request validation failed",
                "details": [
                    {"field": "Password", "message": "must be at least 12 characters"}
                ]
            }
        })))
        .mount(&server)
        .await;

    let body = json!({"email": "a@b.com", "password": "short"});
    let err: KuayleError = client
        .post::<_, serde_json::Value>("/api/auth/login", &body)
        .await
        .unwrap_err();
    assert!(matches!(err, KuayleError::Validation { .. }));
    assert_eq!(err.exit_code(), 4);
}

#[tokio::test]
async fn error_500_maps_to_server() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("GET"))
        .and(path("/api/crash"))
        .respond_with(ResponseTemplate::new(500).set_body_json(json!({
            "error": {
                "code": "INTERNAL_ERROR",
                "message": "something went wrong"
            }
        })))
        .mount(&server)
        .await;

    let err: KuayleError = client
        .get::<serde_json::Value>("/api/crash")
        .await
        .unwrap_err();
    assert!(matches!(err, KuayleError::Server { status: 500, .. }));
    assert_eq!(err.exit_code(), 7);
}

#[tokio::test]
async fn error_502_maps_to_server() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("GET"))
        .and(path("/api/unavailable"))
        .respond_with(ResponseTemplate::new(502).set_body_json(json!({
            "error": {
                "code": "BAD_GATEWAY",
                "message": "upstream error"
            }
        })))
        .mount(&server)
        .await;

    let err: KuayleError = client
        .get::<serde_json::Value>("/api/unavailable")
        .await
        .unwrap_err();
    assert!(matches!(err, KuayleError::Server { status: 502, .. }));
}

#[tokio::test]
async fn error_invalid_json_body_maps_to_api_error() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    // Return HTML instead of JSON.
    // 返回 HTML 而非 JSON。
    Mock::given(method("GET"))
        .and(path("/api/bad"))
        .respond_with(ResponseTemplate::new(404).set_body_string("<html>Not Found</html>"))
        .mount(&server)
        .await;

    let err: KuayleError = client
        .get::<serde_json::Value>("/api/bad")
        .await
        .unwrap_err();
    // The HTML body can't be parsed as error JSON, so we get Api with code "UNKNOWN".
    // HTML body 无法解析为错误 JSON，所以得到 code 为 "UNKNOWN" 的 Api。
    // Status 404 alone doesn't trigger NotFound — the code field drives the mapping.
    // 单独 status 404 不会触发 NotFound——映射由 code 字段驱动。
    assert!(matches!(err, KuayleError::Api { code, .. } if code == "UNKNOWN"));
}

// ── DELETE tests ──────────────────────────────────────────────────

#[tokio::test]
async fn delete_sends_request() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("DELETE"))
        .and(path("/api/tokens/some-id"))
        .and(header("Authorization", "Bearer kuayle_pat_test_token"))
        .respond_with(ResponseTemplate::new(204))
        .mount(&server)
        .await;

    // DELETE to /api/tokens/:id returns 204 No Content.
    // The client handles empty body by returning serde_json::Value::Null.
    // DELETE 到 /api/tokens/:id 返回 204 No Content。
    // 客户端将空 body 处理为 serde_json::Value::Null。
    let result: Result<serde_json::Value, _> = client.delete("/api/tokens/some-id").await;
    assert!(result.is_ok());
}

// ── URL construction ──────────────────────────────────────────────

#[tokio::test]
async fn get_workspaces_list() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("GET"))
        .and(path("/api/workspaces"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!([
            {
                "id": "w1",
                "name": "Acme",
                "slug": "acme",
                "logo_url": null,
                "owner_id": "u1",
                "owner": {
                    "id": "u1",
                    "email": "a@b.com",
                    "name": "Alice",
                    "avatar_url": null
                },
                "share_link_min_role": "admin",
                "current_user_role": "owner",
                "created_at": "2026-07-28T15:58:38.119837+08:00",
                "updated_at": "2026-07-28T15:58:38.119837+08:00"
            }
        ])))
        .mount(&server)
        .await;

    let workspaces: Vec<kuayle_sdk::types::workspace::WorkspaceResponse> =
        client.get("/api/workspaces").await.unwrap();
    assert_eq!(workspaces.len(), 1);
    assert_eq!(workspaces[0].slug, "acme");
}

// ── Retry tests ───────────────────────────────────────────────────
// 重试测试

#[tokio::test]
async fn retry_on_502_then_succeed() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    // First call returns 502, second call returns 200.
    // 第一次调用返回 502，第二次返回 200。
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(502).set_body_json(json!({
            "error": {"code": "BAD_GATEWAY", "message": "upstream error"}
        })))
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .expect(1)
        .mount(&server)
        .await;

    let user: UserResponse = client.get("/api/auth/me").await.unwrap();
    assert_eq!(user.email, "alice@kuayle.dev");
}

#[tokio::test]
async fn retry_on_503_then_succeed() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(503))
        .up_to_n_times(2)
        .expect(2)
        .mount(&server)
        .await;

    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .expect(1)
        .mount(&server)
        .await;

    let user: UserResponse = client.get("/api/auth/me").await.unwrap();
    assert_eq!(user.email, "alice@kuayle.dev");
}

#[tokio::test]
async fn exhaust_retries_on_502_returns_server_error() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    // All 4 calls (1 initial + 3 retries) return 502.
    // 全部 4 次调用（1 次初始 + 3 次重试）都返回 502。
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(502).set_body_json(json!({
            "error": {"code": "BAD_GATEWAY", "message": "upstream error"}
        })))
        .expect(4)
        .mount(&server)
        .await;

    let err: KuayleError = client
        .get::<UserResponse>("/api/auth/me")
        .await
        .unwrap_err();
    assert!(matches!(err, KuayleError::Server { status: 502, .. }));
}

#[tokio::test]
async fn post_does_not_retry() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    // POST is non-idempotent, should not retry on 502.
    // POST 非幂等，不应在 502 时重试。
    Mock::given(method("POST"))
        .and(path("/api/auth/login"))
        .respond_with(ResponseTemplate::new(502).set_body_json(json!({
            "error": {"code": "BAD_GATEWAY", "message": "upstream error"}
        })))
        .expect(1)
        .mount(&server)
        .await;

    let body = json!({"email": "a@b.com", "password": "secret123456"});
    let err: KuayleError = client
        .post::<_, serde_json::Value>("/api/auth/login", &body)
        .await
        .unwrap_err();
    assert!(matches!(err, KuayleError::Server { status: 502, .. }));
}

#[tokio::test]
async fn rate_limit_429_with_retry_after() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    // First call returns 429 with Retry-After: 0 (instant for test).
    // Second call succeeds.
    // 第一次调用返回 429 带 Retry-After: 0（测试中瞬时），第二次成功。
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(
            ResponseTemplate::new(429)
                .set_body_json(json!({
                    "error": {"code": "RATE_LIMITED", "message": "too many requests"}
                }))
                .insert_header("Retry-After", "0"),
        )
        .up_to_n_times(1)
        .expect(1)
        .mount(&server)
        .await;

    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(200).set_body_json(user_json()))
        .expect(1)
        .mount(&server)
        .await;

    let user: UserResponse = client.get("/api/auth/me").await.unwrap();
    assert_eq!(user.email, "alice@kuayle.dev");
}

#[tokio::test]
async fn rate_limit_exhausted_returns_rate_limited_error() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    // All calls return 429 (1 initial + 2 rate-limit retries = 3 total).
    // 所有调用返回 429（1 初始 + 2 限流重试 = 3 次）。
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(
            ResponseTemplate::new(429)
                .set_body_json(json!({
                    "error": {"code": "RATE_LIMITED", "message": "too many requests"}
                }))
                .insert_header("Retry-After", "0"),
        )
        .expect(3)
        .mount(&server)
        .await;

    let err: KuayleError = client
        .get::<UserResponse>("/api/auth/me")
        .await
        .unwrap_err();
    assert!(matches!(err, KuayleError::RateLimited { .. }));
    assert_eq!(err.exit_code(), 6);
}

#[tokio::test]
async fn non_retryable_500_does_not_retry() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    // 500 is not in the retryable set (502/503/504 only).
    // 500 不在可重试集合内（仅 502/503/504）。
    Mock::given(method("GET"))
        .and(path("/api/auth/me"))
        .respond_with(ResponseTemplate::new(500).set_body_json(json!({
            "error": {"code": "INTERNAL_ERROR", "message": "boom"}
        })))
        .expect(1)
        .mount(&server)
        .await;

    let err: KuayleError = client
        .get::<UserResponse>("/api/auth/me")
        .await
        .unwrap_err();
    assert!(matches!(err, KuayleError::Server { status: 500, .. }));
}
