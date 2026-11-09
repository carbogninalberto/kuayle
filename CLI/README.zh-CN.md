# kuayle-cli

[English](README.md) | 中文

[kuayle](https://github.com/carbogninalberto/kuayle)(自托管、开源的 issue tracker)的命令行工具与 Rust SDK,为人类**和** coding agent 而设计。

灵感来自 [lineark](https://github.com/flipbit03/lineark)(Linear CLI),针对 kuayle 的 REST API 与自托管部署重新设计。

## 为什么选择 kuayle-cli?

- **Agent 优先,token 便宜。** 让 agent 调用 CLI,它通过 `kuayle usage` 在运行时发现全部命令——完整的命令参考**不到 1,000 token**,没有 MCP 工具描述吃掉上下文窗口。
- **结构化的失败。** 语义化退出码(2=认证、3=未找到、4=参数校验、5=权限不足、6=限流、7=网络),`--format json` 模式下错误也是 JSON,agent 可以按失败类型分支处理。
- **人类可读的标识符。** `--team Engineering`、`--assignee "Jane Doe"`、`--labels "Bug,P0"`——不需要 UUID。名称并发解析,带短期磁盘缓存。
- **自托管一等公民。** Profile 绑定实例 URL + workspace + 凭据,`--profile` 一键切换实例。

## 环境要求

需要支持 Personal Access Token 的 kuayle 服务端——见 [kuayle#51](https://github.com/carbogninalberto/kuayle/issues/51) / [kuayle#55](https://github.com/carbogninalberto/kuayle/pull/55)。在该 PR 合入上游之前,请从 PR 分支构建服务端。

## 安装

macOS / Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/uiYzzi/kuayle_cli/main/install.sh | sh
```

Windows(PowerShell):

```powershell
irm https://raw.githubusercontent.com/uiYzzi/kuayle_cli/main/install.ps1 | iex
```

或通过 cargo:`cargo install --git https://github.com/uiYzzi/kuayle_cli kuayle`

Shell 补全:`kuayle completion <bash|zsh|fish>`

## 认证

在 kuayle 中创建 PAT(**Settings → API Tokens**;`Developer` 预设覆盖 CLI 全部功能),然后:

```sh
kuayle auth login --token kuayle_pat_... --url https://kuayle.example.com --workspace acme
kuayle auth status
```

凭据存储在操作系统 keychain(降级为 `0600` 权限的文件)。通过 profile 管理多个实例:

```sh
kuayle auth login --token kuayle_pat_... --url https://kuayle.internal.example.com --profile work
kuayle --profile work issues list
```

CI/agent 场景只需要环境变量:

```sh
export KUAYLE_URL=https://kuayle.example.com KUAYLE_WORKSPACE=acme
kuayle auth login --token "$KUAYLE_TOKEN"
```

## 接入你的 AI agent

安装内置的 agent skill(兼容 [skills](https://skills.sh)):

```sh
npx skills add uiYzzi/kuayle_cli
```

或者把下面几行加入 agent 的上下文文件(`CLAUDE.md`、`AGENTS.md`、system prompt 等):

```
We track our tickets and projects in kuayle, a self-hosted issue tracker.
We use the `kuayle` CLI to communicate with it. Use your Bash tool to call the
`kuayle` executable. Run `kuayle usage` to see the full command reference
(global flags, name resolution, exit codes). Prefer `--format json` and branch
on exit codes: 2=re-auth needed, 3=not found, 4=invalid input, 5=forbidden,
6=rate limited, 7=network/server error.
```

## 功能一览

| 领域 | 命令 |
|------|----------|
| **认证** | `auth login`、`auth logout`、`auth status`、`whoami` |
| **Issues** | `list`、`read`、`create`、`update`、`delete`、`batch-update`、`batch-delete`、`subscribe`、`unsubscribe`、`history` |
| **Comments** | `list`、`create`、`resolve`、`reopen` |
| **Relations** | `list`、`create`、`delete` |
| **Labels** | `list`、`create`、`update`、`delete` |
| **Teams** | `list`、`read` · **Statuses** `list` |
| **Projects** | `list`、`read` · **Cycles** `list`、`read`、`burndown`、`velocity` |
| **Templates** | `list`、`read`、`create`、`update`、`delete` |
| **Views / Favorites / Notifications / Members** | `list`(只读) |
| **Assets** | `read`、`upload`、`download` |
| **其他** | `usage`、`completion`、`self update` |

每个命令都支持 `--help`。Issue 一律用 identifier(`KUA-123`)寻址,不需要 UUID。支持 `--parent` 指定父 issue 与 `sub-issues-list`/`sub-issues-create` 子任务命令。

```sh
kuayle issues create --title "Fix login redirect" --team Engineering \
  --assignee "Jane Doe" --labels "Bug,P0" --priority high
kuayle issues list --status in_progress --format json
kuayle comments create ENG-42 --body "Fixed in 3a7d5ee"
```

部分写操作(cycles、views、favorites、notifications)目前被服务端对 PAT 禁用,待 kuayle 补充权限码后开放——见 [PR #55](https://github.com/carbogninalberto/kuayle/pull/55)。

## SDK:kuayle-sdk

CLI 是 `kuayle-sdk` 之上的薄壳(本 workspace 内的 `kuayle-sdk` crate):

```rust
use kuayle_sdk::{Client, types::user::UserResponse};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let client = Client::new("https://kuayle.example.com".parse()?, "kuayle_pat_...".into());
    let me: UserResponse = client.get("/api/auth/me").await?;
    println!("{me:?}");
    Ok(())
}
```

特性:类型化错误枚举、offset 分页流、带抖动退避与 `Retry-After` 处理的重试、类型安全的 `IssueFilter` builder。

## 开发

```sh
cargo xtask check    # fmt + clippy + build + test
cargo test           # 单元 + wiremock 集成测试
KUAYLE_URL=http://localhost:5173 cargo test --test contract   # 真实实例契约测试(不可达时自动跳过)
```

## License

MIT
