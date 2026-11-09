// Golden snapshot tests for usage, --help, and table output.
// 黄金快照测试：usage、--help 和表格输出。
//
// On first run, insta creates snapshot files under tests/snapshots/.
// Accept with: cargo insta review --accept
// 首次运行时 insta 在 tests/snapshots/ 下创建快照文件。
// 使用 cargo insta review --accept 接受。

use assert_cmd::Command;
use insta::assert_snapshot;

#[test]
fn snapshot_usage() {
    let mut cmd = Command::cargo_bin("kuayle").unwrap();
    cmd.arg("usage");
    let output = cmd.assert().success();
    let stdout = String::from_utf8_lossy(&output.get_output().stdout);
    assert_snapshot!("usage", stdout);
}

#[test]
fn snapshot_help() {
    let mut cmd = Command::cargo_bin("kuayle").unwrap();
    cmd.arg("--help");
    let output = cmd.assert().success();
    let stdout = String::from_utf8_lossy(&output.get_output().stdout);
    assert_snapshot!("help", stdout);
}

#[test]
fn snapshot_issues_list_help() {
    let mut cmd = Command::cargo_bin("kuayle").unwrap();
    cmd.args(["issues", "list", "--help"]);
    let output = cmd.assert().success();
    let stdout = String::from_utf8_lossy(&output.get_output().stdout);
    assert_snapshot!("issues_list_help", stdout);
}
