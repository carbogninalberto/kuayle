// Error types for kuayle-sdk.
// kuayle-sdk 的错误类型。
//
// Structured errors with semantic exit codes so agents can branch
// on failure modes without parsing human-readable text.
// 结构化错误，带语义退出码，agent 无需解析人类可读文本即可分支处理失败模式。

use serde::{Deserialize, Serialize};
use std::time::Duration;
use thiserror::Error;

/// A single field-level validation error from the kuayle API.
/// kuayle API 返回的单个字段级校验错误。
#[derive(Debug, Clone, Deserialize, Serialize, PartialEq)]
pub struct FieldError {
    pub field: String,
    pub message: String,
}

/// The error envelope returned by kuayle on non-2xx responses.
/// kuayle 在非 2xx 响应时返回的错误信封。
#[derive(Debug, Clone, Deserialize)]
pub struct ErrorBody {
    pub error: ErrorPayload,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ErrorPayload {
    pub code: String,
    pub message: String,
    #[serde(default)]
    pub details: Vec<FieldError>,
}

/// Structured error type covering all kuayle API failure modes.
/// 覆盖所有 kuayle API 失败模式的结构化错误类型。
#[derive(Debug, Error)]
pub enum KuayleError {
    /// Authentication failed (token expired or invalid).
    /// 认证失败（token 过期或无效）。
    #[error("authentication failed: {message}")]
    Authentication { message: String },

    /// Permission denied for the requested resource.
    /// 请求资源的权限不足。
    #[error("permission denied: {message}")]
    Forbidden { message: String },

    /// The requested resource was not found.
    /// 请求的资源未找到。
    #[error("not found: {message}")]
    NotFound { message: String },

    /// Request validation failed, with per-field details.
    /// 请求校验失败，附带各字段详情。
    #[error("validation failed")]
    Validation { details: Vec<FieldError> },

    /// Rate limited; the server told us when to retry.
    /// 被限流；服务端告知了重试时间。
    #[error("rate limited, retry after {retry_after:?}")]
    RateLimited { retry_after: Option<Duration> },

    /// A server-side error (5xx without retry success).
    /// 服务端错误（5xx 且重试未成功）。
    #[error("server error {status}: {message}")]
    Server { status: u16, message: String },

    /// An API error with a known code that doesn't fit the above.
    /// 带有已知错误码但不属于上述类别的 API 错误。
    #[error("api error {code}: {message}")]
    Api { code: String, message: String },

    /// A transport-level error (network, TLS, DNS, etc.).
    /// 传输层错误（网络、TLS、DNS 等）。
    #[error(transparent)]
    Transport(#[from] reqwest::Error),
}

/// Maps an HTTP status code and kuayle error code to the appropriate variant.
/// 将 HTTP 状态码和 kuayle 错误码映射到对应的变体。
///
/// The error code from the API takes precedence over HTTP status
/// for determining the variant, per the design doc mapping rules.
/// API 返回的错误码优先于 HTTP 状态码来决定变体，依据设计文档的映射规则。
impl KuayleError {
    /// Build a `KuayleError` from an HTTP status and parsed error body.
    /// 从 HTTP 状态码和解析出的错误体构造 `KuayleError`。
    pub fn from_response(status: u16, body: ErrorBody) -> Self {
        let ErrorPayload {
            code,
            message,
            details,
        } = body.error;

        match code.as_str() {
            "UNAUTHORIZED" => KuayleError::Authentication { message },
            "FORBIDDEN" => KuayleError::Forbidden { message },
            "NOT_FOUND" => KuayleError::NotFound { message },
            "VALIDATION_ERROR" => KuayleError::Validation { details },
            _ => {
                // Fallback: use HTTP status for server errors
                // 兜底：用 HTTP 状态码判断服务端错误
                if status >= 500 {
                    KuayleError::Server { status, message }
                } else {
                    KuayleError::Api { code, message }
                }
            }
        }
    }

    /// Semantic exit code for CLI consumption.
    /// 供 CLI 消费的语义退出码。
    ///
    /// | Code | Meaning |
    /// |------|---------|
    /// | 1    | Generic / unclassified error |
    /// | 2    | Authentication failure (run `kuayle auth login`) |
    /// | 3    | Resource not found |
    /// | 4    | Validation error |
    /// | 5    | Permission denied |
    /// | 6    | Rate limited |
    /// | 7    | Network / server unreachable |
    pub fn exit_code(&self) -> i32 {
        match self {
            KuayleError::Authentication { .. } => 2,
            KuayleError::Forbidden { .. } => 5,
            KuayleError::NotFound { .. } => 3,
            KuayleError::Validation { .. } => 4,
            KuayleError::RateLimited { .. } => 6,
            KuayleError::Server { .. } => 7,
            KuayleError::Api { .. } => 1,
            KuayleError::Transport(_) => 7,
        }
    }

    /// Machine-readable error kind (snake_case).
    /// 机器可读的错误类别（snake_case）。
    pub fn kind(&self) -> &'static str {
        match self {
            KuayleError::Authentication { .. } => "authentication",
            KuayleError::Forbidden { .. } => "forbidden",
            KuayleError::NotFound { .. } => "not_found",
            KuayleError::Validation { .. } => "validation",
            KuayleError::RateLimited { .. } => "rate_limited",
            KuayleError::Server { .. } => "server_error",
            KuayleError::Api { .. } => "api_error",
            KuayleError::Transport(_) => "transport_error",
        }
    }

    /// Serialize as the JSON error envelope for `--format json`.
    /// 序列化为 `--format json` 的 JSON 错误信封。
    ///
    /// Output: `{"error": {"kind": "...", "message": "..."}}`
    /// 输出：`{"error": {"kind": "...", "message": "..."}}`
    pub fn to_json_error(&self) -> String {
        let kind = self.kind();
        let message = self.to_string();
        let details: Option<&[FieldError]> = match self {
            KuayleError::Validation { details } if !details.is_empty() => Some(details),
            _ => None,
        };

        let mut obj = serde_json::json!({
            "error": {
                "kind": kind,
                "message": message,
            }
        });

        if let Some(d) = details {
            obj["error"]["details"] = serde_json::to_value(d).unwrap_or_default();
        }

        serde_json::to_string(&obj)
            .unwrap_or_else(|_| format!(r#"{{"error":{{"kind":"{kind}","message":"{message}"}}}}"#))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // ── Exit code mapping ──────────────────────────────────────────
    // 退出码映射

    #[test]
    fn exit_code_authentication_is_2() {
        let err = KuayleError::Authentication {
            message: "expired".into(),
        };
        assert_eq!(err.exit_code(), 2);
    }

    #[test]
    fn exit_code_forbidden_is_5() {
        let err = KuayleError::Forbidden {
            message: "nope".into(),
        };
        assert_eq!(err.exit_code(), 5);
    }

    #[test]
    fn exit_code_not_found_is_3() {
        let err = KuayleError::NotFound {
            message: "gone".into(),
        };
        assert_eq!(err.exit_code(), 3);
    }

    #[test]
    fn exit_code_validation_is_4() {
        let err = KuayleError::Validation { details: vec![] };
        assert_eq!(err.exit_code(), 4);
    }

    #[test]
    fn exit_code_rate_limited_is_6() {
        let err = KuayleError::RateLimited { retry_after: None };
        assert_eq!(err.exit_code(), 6);
    }

    #[test]
    fn exit_code_server_is_7() {
        let err = KuayleError::Server {
            status: 502,
            message: "bad gateway".into(),
        };
        assert_eq!(err.exit_code(), 7);
    }

    #[test]
    fn exit_code_api_is_1() {
        let err = KuayleError::Api {
            code: "SOMETHING_ELSE".into(),
            message: "unknown".into(),
        };
        assert_eq!(err.exit_code(), 1);
    }

    // ── Error body deserialization ─────────────────────────────────
    // 错误体反序列化

    #[test]
    fn deserialize_unauthorized_error() {
        let json = r#"{"error":{"code":"UNAUTHORIZED","message":"Authentication required"}}"#;
        let body: ErrorBody = serde_json::from_str(json).unwrap();
        assert_eq!(body.error.code, "UNAUTHORIZED");
        assert_eq!(body.error.message, "Authentication required");
        assert!(body.error.details.is_empty());
    }

    #[test]
    fn deserialize_validation_error_with_details() {
        let json = r#"{
            "error": {
                "code": "VALIDATION_ERROR",
                "message": "Request validation failed",
                "details": [
                    {"field": "Password", "message": "must be at least 12 characters"},
                    {"field": "Email", "message": "invalid format"}
                ]
            }
        }"#;
        let body: ErrorBody = serde_json::from_str(json).unwrap();
        assert_eq!(body.error.code, "VALIDATION_ERROR");
        assert_eq!(body.error.details.len(), 2);
        assert_eq!(body.error.details[0].field, "Password");
        assert_eq!(body.error.details[1].field, "Email");
    }

    #[test]
    fn deserialize_not_found_error() {
        let json = r#"{"error":{"code":"NOT_FOUND","message":"Workspace not found"}}"#;
        let body: ErrorBody = serde_json::from_str(json).unwrap();
        assert_eq!(body.error.code, "NOT_FOUND");
    }

    // ── from_response mapping ──────────────────────────────────────
    // from_response 映射

    #[test]
    fn map_unauthorized_to_authentication() {
        let body = ErrorBody {
            error: ErrorPayload {
                code: "UNAUTHORIZED".into(),
                message: "bad token".into(),
                details: vec![],
            },
        };
        let err = KuayleError::from_response(401, body);
        assert!(matches!(err, KuayleError::Authentication { .. }));
        assert_eq!(err.exit_code(), 2);
    }

    #[test]
    fn map_forbidden_to_forbidden() {
        let body = ErrorBody {
            error: ErrorPayload {
                code: "FORBIDDEN".into(),
                message: "no access".into(),
                details: vec![],
            },
        };
        let err = KuayleError::from_response(403, body);
        assert!(matches!(err, KuayleError::Forbidden { .. }));
        assert_eq!(err.exit_code(), 5);
    }

    #[test]
    fn map_not_found_to_not_found() {
        let body = ErrorBody {
            error: ErrorPayload {
                code: "NOT_FOUND".into(),
                message: "missing".into(),
                details: vec![],
            },
        };
        let err = KuayleError::from_response(404, body);
        assert!(matches!(err, KuayleError::NotFound { .. }));
        assert_eq!(err.exit_code(), 3);
    }

    #[test]
    fn map_validation_error_to_validation() {
        let body = ErrorBody {
            error: ErrorPayload {
                code: "VALIDATION_ERROR".into(),
                message: "bad input".into(),
                details: vec![FieldError {
                    field: "title".into(),
                    message: "required".into(),
                }],
            },
        };
        let err = KuayleError::from_response(400, body);
        assert!(matches!(err, KuayleError::Validation { .. }));
        if let KuayleError::Validation { details } = &err {
            assert_eq!(details.len(), 1);
            assert_eq!(details[0].field, "title");
        }
    }

    #[test]
    fn map_unknown_code_with_500_status_to_server() {
        let body = ErrorBody {
            error: ErrorPayload {
                code: "INTERNAL_ERROR".into(),
                message: "something broke".into(),
                details: vec![],
            },
        };
        let err = KuayleError::from_response(500, body);
        assert!(matches!(err, KuayleError::Server { status: 500, .. }));
        assert_eq!(err.exit_code(), 7);
    }

    #[test]
    fn map_unknown_code_with_400_status_to_api() {
        let body = ErrorBody {
            error: ErrorPayload {
                code: "INVALID_CREDENTIALS".into(),
                message: "wrong password".into(),
                details: vec![],
            },
        };
        let err = KuayleError::from_response(400, body);
        assert!(matches!(err, KuayleError::Api { .. }));
        assert_eq!(err.exit_code(), 1);
    }

    #[test]
    fn rate_limited_error_to_string_contains_retry_after() {
        let err = KuayleError::RateLimited {
            retry_after: Some(Duration::from_secs(30)),
        };
        let s = err.to_string();
        assert!(s.contains("30s"));
    }

    // ── Display formatting ─────────────────────────────────────────
    // Display 格式化

    #[test]
    fn authentication_display_includes_message() {
        let err = KuayleError::Authentication {
            message: "Token expired".into(),
        };
        assert!(err.to_string().contains("Token expired"));
    }

    #[test]
    fn not_found_display_includes_message() {
        let err = KuayleError::NotFound {
            message: "Issue KUA-99 not found".into(),
        };
        assert!(err.to_string().contains("KUA-99"));
    }

    // ── kind() and to_json_error() ─────────────────────────────────

    #[test]
    fn kind_authentication() {
        let err = KuayleError::Authentication {
            message: "x".into(),
        };
        assert_eq!(err.kind(), "authentication");
    }

    #[test]
    fn kind_forbidden() {
        let err = KuayleError::Forbidden {
            message: "x".into(),
        };
        assert_eq!(err.kind(), "forbidden");
    }

    #[test]
    fn kind_not_found() {
        let err = KuayleError::NotFound {
            message: "x".into(),
        };
        assert_eq!(err.kind(), "not_found");
    }

    #[test]
    fn kind_validation() {
        let err = KuayleError::Validation { details: vec![] };
        assert_eq!(err.kind(), "validation");
    }

    #[test]
    fn kind_rate_limited() {
        let err = KuayleError::RateLimited { retry_after: None };
        assert_eq!(err.kind(), "rate_limited");
    }

    #[test]
    fn to_json_error_contains_kind_and_message() {
        let err = KuayleError::Authentication {
            message: "bad token".into(),
        };
        let json = err.to_json_error();
        assert!(json.contains(r#""kind":"authentication""#));
        assert!(json.contains("bad token"));
    }

    #[test]
    fn to_json_error_validation_includes_details() {
        let err = KuayleError::Validation {
            details: vec![FieldError {
                field: "title".into(),
                message: "required".into(),
            }],
        };
        let json = err.to_json_error();
        assert!(json.contains(r#""kind":"validation""#));
        assert!(json.contains("title"));
        assert!(json.contains("required"));
    }

    #[test]
    fn to_json_error_is_valid_json() {
        let err = KuayleError::NotFound {
            message: "gone".into(),
        };
        let json = err.to_json_error();
        let parsed: serde_json::Value = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed["error"]["kind"], "not_found");
    }
}
