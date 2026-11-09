// xtask: Cargo task runner for kuayle-cli development.
// xtask: kuayle-cli 开发的 Cargo 任务运行器。
//
// Usage: cargo xtask <task>
// Tasks:
//   check  — run fmt, clippy, and test
//   Usage：cargo xtask <task>
// 任务：
//   check  — 运行 fmt、clippy 和 test

use std::process::Command;

fn main() {
    let task = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "check".to_string());

    match task.as_str() {
        "check" => run_check(),
        _ => {
            eprintln!("Unknown task: {task}");
            eprintln!("未知任务：{task}");
            eprintln!("Available: check");
            eprintln!("可用：check");
            std::process::exit(1);
        }
    }
}

fn run_check() {
    let tasks = [
        ("fmt", vec!["fmt", "--all", "--", "--check"]),
        ("clippy", vec!["clippy", "--all-targets", "--all-features"]),
        ("test", vec!["test", "--all-targets"]),
    ];

    let mut failed = false;

    for (name, args) in &tasks {
        println!("=== cargo {name} ===");
        let status = Command::new("cargo")
            .args(args)
            .status()
            .unwrap_or_else(|e| {
                eprintln!("Failed to run cargo {name}: {e}");
                std::process::exit(1);
            });

        if !status.success() {
            eprintln!("✗ cargo {name} failed");
            failed = true;
        } else {
            println!("✓ cargo {name} passed\n");
        }
    }

    if failed {
        std::process::exit(1);
    }

    println!("All checks passed!");
    println!("所有检查通过！");
}
