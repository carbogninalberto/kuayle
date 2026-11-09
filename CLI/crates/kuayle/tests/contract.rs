// Contract tests against real kuayle instance.
// 对真实 kuayle 实例的契约测试。
//
// These tests are skipped when the instance is unreachable,
// with the reason printed. They are NOT run in CI — only
// locally when the developer has the instance running.
// 实例不可达时这些测试会被跳过，并打印原因。
// 它们不在 CI 中运行——仅当开发者本地实例运行时执行。

/// Check if the kuayle instance is reachable. Returns Ok(()) or a skip reason.
/// 检查 kuayle 实例是否可达。返回 Ok(()) 或跳过原因。
fn check_instance() -> Result<(), String> {
    let url = std::env::var("KUAYLE_URL").unwrap_or_else(|_| "http://localhost:5173".to_string());
    match std::process::Command::new("curl")
        .args([
            "-s",
            "-o",
            "/dev/null",
            "-w",
            "%{http_code}",
            &format!("{url}/api/health"),
        ])
        .output()
    {
        Ok(out) => {
            let code = String::from_utf8_lossy(&out.stdout);
            if code.trim() == "200" {
                Ok(())
            } else {
                Err(format!(
                    "instance at {url} returned HTTP {code} (expected 200)"
                ))
            }
        }
        Err(e) => Err(format!("instance at {url} unreachable: {e}")),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn instance_is_reachable() {
        match check_instance() {
            Ok(()) => println!("Instance reachable — contract tests available"),
            Err(reason) => println!("SKIPPED: {reason}"),
        }
    }
}
