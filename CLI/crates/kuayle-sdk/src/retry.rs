// Retry policy for transient failures.
// 瞬时失败的重试策略。

use rand::Rng;
use std::time::Duration;

/// Retry configuration for the SDK client.
/// SDK 客户端的重试配置。
#[derive(Debug, Clone)]
pub struct RetryPolicy {
    /// Maximum retries for network/5xx errors.
    /// 网络/5xx 错误的最大重试次数。
    pub max_retries: u32,
    /// Maximum retries for 429 rate-limit errors.
    /// 429 限流错误的最大重试次数。
    pub max_rate_limit_retries: u32,
    /// Base backoff duration (doubles each retry).
    /// 基础退避时长（每次重试翻倍）。
    pub base_backoff: Duration,
    /// Maximum backoff duration cap.
    /// 最大退避时长上限。
    pub max_backoff: Duration,
}

impl Default for RetryPolicy {
    fn default() -> Self {
        RetryPolicy {
            max_retries: 3,
            max_rate_limit_retries: 2,
            base_backoff: Duration::from_millis(500),
            max_backoff: Duration::from_secs(30),
        }
    }
}

impl RetryPolicy {
    /// Compute the backoff duration for retry attempt `n` (0-indexed).
    /// 计算第 `n` 次重试（从 0 开始）的退避时长。
    ///
    /// Uses exponential backoff with ±20% jitter.
    /// 使用带 ±20% 抖动的指数退避。
    pub fn backoff(&self, attempt: u32) -> Duration {
        let base = self.base_backoff.as_millis() as u64;
        let exponential = base * (1u64 << attempt.min(10));
        let capped = exponential.min(self.max_backoff.as_millis() as u64);

        // ±20% random jitter to avoid thundering herd.
        // ±20% 随机抖动，避免惊群效应。
        let jitter_range = (capped / 5) as i64;
        let mut rng = rand::rng();
        let jitter = if jitter_range > 0 {
            rng.random_range(-jitter_range..=jitter_range)
        } else {
            0
        };

        let ms = (capped as i64 + jitter).max(0) as u64;
        Duration::from_millis(ms)
    }

    /// Whether an HTTP status code is a server error worth retrying.
    /// HTTP 状态码是否值得重试的服务端错误。
    pub fn is_retryable_server_error(status: u16) -> bool {
        matches!(status, 502..=504)
    }

    /// Whether an HTTP status code signals rate-limiting.
    /// HTTP 状态码是否表示限流。
    pub fn is_rate_limit(status: u16) -> bool {
        status == 429
    }

    /// Whether the request method is inherently idempotent (safe to retry).
    /// 请求方法是否天然幂等（安全重试）。
    pub fn is_idempotent(method: &str) -> bool {
        matches!(method, "GET" | "HEAD" | "OPTIONS")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn backoff_increases_with_attempts() {
        let policy = RetryPolicy::default();
        // With random jitter, assert ranges rather than exact ordering.
        // 使用随机抖动，断言范围而非精确比较。
        let d0 = policy.backoff(0).as_millis();
        let d2 = policy.backoff(2).as_millis();
        // d0 base=500ms, d2 base=2000ms; even with ±20% jitter, d2 should be larger.
        // d0 基础 500ms，d2 基础 2000ms；即使有 ±20% 抖动，d2 也应该更大。
        assert!(d2 > d0, "d2={d2} should be > d0={d0}");
    }

    #[test]
    fn backoff_capped_at_max() {
        let policy = RetryPolicy::default();
        let max_ms = policy.max_backoff.as_millis() as u64;
        // With jitter, can go up to max + 20%. Assert cap applies.
        // 有抖动时最多到 max + 20%。断言上限生效。
        let d = policy.backoff(10).as_millis();
        let max_with_jitter = max_ms + max_ms / 5;
        assert!(
            d <= max_with_jitter as u128,
            "d={d} > max_with_jitter={max_with_jitter}"
        );
    }

    #[test]
    fn is_retryable_server_error_502_503_504() {
        assert!(RetryPolicy::is_retryable_server_error(502));
        assert!(RetryPolicy::is_retryable_server_error(503));
        assert!(RetryPolicy::is_retryable_server_error(504));
    }

    #[test]
    fn is_not_retryable_server_error_for_500_501() {
        assert!(!RetryPolicy::is_retryable_server_error(500));
        assert!(!RetryPolicy::is_retryable_server_error(501));
    }

    #[test]
    fn is_rate_limit_429() {
        assert!(RetryPolicy::is_rate_limit(429));
        assert!(!RetryPolicy::is_rate_limit(400));
    }

    #[test]
    fn get_is_idempotent() {
        assert!(RetryPolicy::is_idempotent("GET"));
        assert!(RetryPolicy::is_idempotent("HEAD"));
        assert!(!RetryPolicy::is_idempotent("POST"));
        assert!(!RetryPolicy::is_idempotent("DELETE"));
    }

    #[test]
    fn default_policy_values() {
        let p = RetryPolicy::default();
        assert_eq!(p.max_retries, 3);
        assert_eq!(p.max_rate_limit_retries, 2);
    }
}
