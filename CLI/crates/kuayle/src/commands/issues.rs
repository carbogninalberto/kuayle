// Issue commands with name→ID resolution via Resolver.
// 带 Resolver name→ID 解析的 issue 命令。

use futures_util::StreamExt;
use kuayle_sdk::filter::{IssueFilter, Priority};
use kuayle_sdk::resources::issues::Issues;
use kuayle_sdk::types::issue::{CreateIssueRequest, IssueResponse, UpdateIssueRequest};

use crate::cli::{Cli, IssueAction};
use crate::output::{self, is_json_output};
use crate::resolve::{ResolveKind, Resolver};
use kuayle_sdk::types::user::UserResponse;

pub async fn handle(action: &IssueAction, cli: &Cli) {
    match action {
        IssueAction::List {
            status,
            priority,
            assignee,
            label,
            team,
            project,
            cycle,
            search,
            all,
        } => {
            cmd_list(
                cli,
                status.as_deref(),
                *priority,
                assignee.as_deref(),
                label.as_deref(),
                team.as_deref(),
                project.as_deref(),
                cycle.as_deref(),
                search.as_deref(),
                *all,
            )
            .await
        }
        IssueAction::Read { identifier } => cmd_read(cli, identifier).await,
        IssueAction::Create {
            title,
            description,
            priority,
            team,
            assignee,
            labels,
            project,
            cycle,
            parent,
        } => {
            cmd_create(
                cli,
                title,
                description.as_deref(),
                *priority,
                team.as_deref(),
                assignee.as_ref(),
                labels.as_ref(),
                project.as_deref(),
                cycle.as_deref(),
                parent.as_deref(),
            )
            .await
        }
        IssueAction::Update {
            identifier,
            title,
            description,
            status,
            priority,
            assignee,
            labels,
            parent,
        } => {
            cmd_update(
                cli,
                identifier,
                title.as_deref(),
                description.as_deref(),
                status.as_deref(),
                *priority,
                assignee.as_ref(),
                labels.as_ref(),
                parent.as_deref(),
            )
            .await
        }
        IssueAction::Delete { identifier } => cmd_delete(cli, identifier).await,
        IssueAction::BatchUpdate {
            identifiers,
            status,
            priority,
        } => cmd_batch_update(cli, identifiers, status.as_deref(), *priority).await,
        IssueAction::BatchDelete { identifiers } => cmd_batch_delete(cli, identifiers).await,
        IssueAction::Subscribe { identifier } => cmd_subscribe(cli, identifier).await,
        IssueAction::Unsubscribe { identifier } => cmd_unsubscribe(cli, identifier).await,
        IssueAction::History { identifier } => cmd_history(cli, identifier).await,
        IssueAction::SubIssuesList { identifier } => cmd_sub_issues_list(cli, identifier).await,
        IssueAction::SubIssuesCreate {
            identifier,
            title,
            description,
            priority,
        } => cmd_sub_issues_create(cli, identifier, title, description.as_deref(), *priority).await,
    }
}

async fn setup(cli: &Cli) -> (Issues, Resolver, bool) {
    let is_json = is_json_output(cli);
    let (client, _url) = match crate::commands::resolve_client(cli).await {
        Ok(c) => c,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };
    let ws = cli.workspace.as_deref().unwrap_or("acme");
    let issues = Issues::new(&client, ws);
    let resolver = Resolver::new(client, ws, cli.no_cache);
    (issues, resolver, is_json)
}

// ── list ──────────────────────────────────────────────────────────

#[allow(clippy::too_many_arguments)]
async fn cmd_list(
    cli: &Cli,
    status: Option<&str>,
    priority: Option<i32>,
    assignee: Option<&str>,
    label: Option<&str>,
    team: Option<&str>,
    project: Option<&str>,
    cycle: Option<&str>,
    search: Option<&str>,
    all: bool,
) {
    let (issues, resolver, is_json) = setup(cli).await;

    let (assignee_id, label_id, team_id, project_id, cycle_id) = tokio::join!(
        resolve_filter_param(&resolver, ResolveKind::Members, cli, assignee, is_json),
        resolve_filter_param(&resolver, ResolveKind::Labels, cli, label, is_json),
        resolve_filter_param(&resolver, ResolveKind::Teams, cli, team, is_json),
        resolve_filter_param(&resolver, ResolveKind::Projects, cli, project, is_json),
        resolve_filter_param(&resolver, ResolveKind::Cycles, cli, cycle, is_json),
    );

    let mut filter = IssueFilter::new();
    if let Some(s) = status {
        filter = filter.status(s);
    }
    if let Some(p) = priority {
        filter = filter.priority(priority_from_int(p));
    }
    if let Some(a) = assignee_id {
        filter = filter.assignee(&a);
    }
    if let Some(l) = label_id {
        filter = filter.label(&l);
    }
    if let Some(t) = team_id {
        filter = filter.team(&t);
    }
    if let Some(p) = project_id {
        filter = filter.project(&p);
    }
    if let Some(c) = cycle_id {
        filter = filter.cycle(&c);
    }
    if let Some(q) = search {
        filter = filter.search(q);
    }

    let mut stream = issues.list(filter);
    let mut items: Vec<IssueResponse> = Vec::new();
    let mut has_more = false;
    while let Some(result) = stream.next().await {
        match result {
            Ok(item) => items.push(item),
            Err(e) => {
                output::print_error(&e, is_json);
                std::process::exit(e.exit_code());
            }
        }
        if !all && items.len() >= 100 {
            has_more = true;
            break;
        }
    }
    if is_json {
        println!(
            "{}",
            serde_json::to_string_pretty(&items).unwrap_or_default()
        );
    } else {
        for issue in &items {
            println!(
                "{:<12} {:<60} {:<12}",
                issue.identifier,
                truncate(&issue.title, 58),
                priority_label(issue.priority)
            );
        }
        if has_more {
            println!("… and more (use --all or --page N)");
        }
        println!("\n{} issue(s)", items.len());
    }
}

// ── read ──────────────────────────────────────────────────────────

async fn cmd_read(cli: &Cli, identifier: &str) {
    let (issues, _, is_json) = setup(cli).await;
    match issues.read(identifier).await {
        Ok(issue) => {
            if is_json {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&issue).unwrap_or_default()
                );
            } else {
                println!("Identifier:  {}", issue.identifier);
                println!("Title:       {}", issue.title);
                if let Some(ref d) = issue.description {
                    println!("Description: {}", d);
                }
                println!("Status:      {}", issue.status);
                println!("Priority:    {}", priority_label(issue.priority));
                if let Some(ref a) = issue.assignee {
                    println!("Assignee:    {} ({})", a.display_name, a.email);
                }
                if !issue.labels.is_empty() {
                    let names: Vec<&str> = issue.labels.iter().map(|l| l.name.as_str()).collect();
                    println!("Labels:      {}", names.join(", "));
                }
                if let Some(ref c) = issue.creator {
                    println!("Creator:     {} ({})", c.display_name, c.email);
                }
                println!("Created:     {}", issue.created_at);
                println!("Updated:     {}", issue.updated_at);
                println!("Subscribed:  {}", issue.is_subscribed);
            }
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

// ── create (with name resolution) ─────────────────────────────────

#[allow(clippy::too_many_arguments)]
async fn cmd_create(
    cli: &Cli,
    title: &str,
    description: Option<&str>,
    priority: Option<i32>,
    team: Option<&str>,
    assignee: Option<&Vec<String>>,
    labels: Option<&Vec<String>>,
    project: Option<&str>,
    cycle: Option<&str>,
    parent: Option<&str>,
) {
    let (issues, resolver, is_json) = setup(cli).await;

    // Resolve team first (needed for status resolution later).
    // 先解析 team（后续 status 解析需要用到）。
    let team_id = if let Some(t) = team {
        match resolver.resolve(ResolveKind::Teams, t).await {
            Ok(id) => Some(id),
            Err(e) => output::print_string_error(&e, 3, is_json),
        }
    } else {
        None
    };

    // Resolve everything else in parallel via tokio::join!.
    // 通过 tokio::join! 并发解析其余所有名称。
    let (assignee_result, labels_result, project_result, cycle_result) = tokio::join!(
        resolve_many(&resolver, ResolveKind::Members, assignee),
        resolve_many(&resolver, ResolveKind::Labels, labels),
        resolve_one(&resolver, ResolveKind::Projects, project),
        resolve_one(&resolver, ResolveKind::Cycles, cycle),
    );

    // Handle resolution errors.
    // 处理解析错误。
    let assignee_ids = match assignee_result {
        Ok(ids) => ids,
        Err(e) => output::print_string_error(&e, 3, is_json),
    };
    let label_ids = match labels_result {
        Ok(ids) => ids,
        Err(e) => output::print_string_error(&e, 3, is_json),
    };
    let project_id = match project_result {
        Ok(id) => id,
        Err(e) => output::print_string_error(&e, 3, is_json),
    };
    let cycle_id = match cycle_result {
        Ok(id) => id,
        Err(e) => output::print_string_error(&e, 3, is_json),
    };

    // Resolve parent issue identifier to UUID.
    // 将父 issue identifier 解析为 UUID。
    let parent_id = if let Some(parent_identifier) = parent {
        Some(resolve_parent_identifier(&issues, parent_identifier, is_json).await)
    } else {
        None
    };

    let req = CreateIssueRequest {
        title: title.to_string(),
        description: description.map(|d| d.to_string()),
        priority,
        team_id,
        assignee_ids,
        label_ids,
        project_id,
        cycle_id,
        parent_id,
        ..Default::default()
    };

    match issues.create(&req).await {
        Ok(issue) => {
            if is_json {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&issue).unwrap_or_default()
                );
            } else {
                println!("✓ Created {}: {}", issue.identifier, issue.title);
            }
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

// ── update (with name resolution) ─────────────────────────────────

#[allow(clippy::too_many_arguments)]
async fn cmd_update(
    cli: &Cli,
    identifier: &str,
    title: Option<&str>,
    description: Option<&str>,
    status: Option<&str>,
    priority: Option<i32>,
    assignee: Option<&Vec<String>>,
    labels: Option<&Vec<String>>,
    parent: Option<&str>,
) {
    let (issues, resolver, is_json) = setup(cli).await;

    // Resolve assignee and labels in parallel.
    // 并发解析 assignee 和 labels。
    let (assignee_result, labels_result) = tokio::join!(
        resolve_many(&resolver, ResolveKind::Members, assignee),
        resolve_many(&resolver, ResolveKind::Labels, labels),
    );

    let assignee_ids = match assignee_result {
        Ok(ids) => ids,
        Err(e) => output::print_string_error(&e, 3, is_json),
    };
    let label_ids = match labels_result {
        Ok(ids) => ids,
        Err(e) => output::print_string_error(&e, 3, is_json),
    };

    // Status resolution: try custom statuses first if team is known, then built-in.
    // Status 解析：如果已知 team，先尝试自定义状态，再尝试内置枚举。
    let resolved_status = status.map(|s| resolve_status_builtin(s, is_json));

    // Resolve parent issue identifier to UUID.
    // 将父 issue identifier 解析为 UUID。
    let parent_id = if let Some(parent_identifier) = parent {
        Some(resolve_parent_identifier(&issues, parent_identifier, is_json).await)
    } else {
        None
    };

    let req = UpdateIssueRequest {
        title: title.map(|s| s.to_string()),
        description: description.map(|s| s.to_string()),
        status: resolved_status,
        priority,
        assignee_ids,
        label_ids,
        parent_id,
        ..Default::default()
    };

    match issues.update(identifier, &req).await {
        Ok(issue) => {
            if is_json {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&issue).unwrap_or_default()
                );
            } else {
                println!("✓ Updated {}", issue.identifier);
            }
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

// ── delete / batch / sub/unsub / history ──────────────────────────

async fn cmd_delete(cli: &Cli, identifier: &str) {
    let (issues, _, is_json) = setup(cli).await;
    match issues.delete(identifier).await {
        Ok(_) => println!("✓ Deleted {identifier}"),
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

async fn cmd_batch_update(
    cli: &Cli,
    identifiers: &[String],
    status: Option<&str>,
    priority: Option<i32>,
) {
    let (issues, _, is_json) = setup(cli).await;
    let req = kuayle_sdk::types::issue::BatchUpdateRequest {
        issue_identifiers: identifiers.to_vec(),
        update: UpdateIssueRequest {
            status: status.map(|s| s.to_string()),
            priority,
            ..Default::default()
        },
    };
    match issues.batch_update(&req).await {
        Ok(_) => println!("✓ Batch updated {} issues", identifiers.len()),
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

async fn cmd_batch_delete(cli: &Cli, identifiers: &[String]) {
    let (issues, _, is_json) = setup(cli).await;
    match issues.batch_delete(identifiers).await {
        Ok(_) => println!("✓ Batch deleted {} issues", identifiers.len()),
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

async fn cmd_subscribe(cli: &Cli, identifier: &str) {
    let (issues, _, is_json) = setup(cli).await;
    match issues.subscribe(identifier).await {
        Ok(_) => println!("✓ Subscribed to {identifier}"),
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

async fn cmd_unsubscribe(cli: &Cli, identifier: &str) {
    let (issues, _, is_json) = setup(cli).await;
    match issues.unsubscribe(identifier).await {
        Ok(_) => println!("✓ Unsubscribed from {identifier}"),
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

async fn cmd_history(cli: &Cli, identifier: &str) {
    let (issues, _, is_json) = setup(cli).await;
    match issues.history(identifier).await {
        Ok(history) => println!(
            "{}",
            serde_json::to_string_pretty(&history).unwrap_or_default()
        ),
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

// ── sub-issues ─────────────────────────────────────────────────────

async fn cmd_sub_issues_list(cli: &Cli, identifier: &str) {
    let is_json = is_json_output(cli);
    let (client, _url) = match crate::commands::resolve_client(cli).await {
        Ok(c) => c,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };
    let ws = cli.workspace.as_deref().unwrap_or("acme");
    let path = format!("/api/workspaces/{ws}/issues/{identifier}/sub-issues");
    // Note: sub-issues endpoint returns a plain array, not ListResponse.
    // 注意：sub-issues 端点返回纯数组，而非 ListResponse。
    let items: Vec<IssueResponse> = match client.get(&path).await {
        Ok(v) => v,
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    };
    if is_json {
        println!(
            "{}",
            serde_json::to_string_pretty(&items).unwrap_or_default()
        );
    } else {
        if items.is_empty() {
            println!("No sub-issues found.");
            return;
        }
        for issue in &items {
            println!(
                "{:<12} {:<60} {:<12}",
                issue.identifier,
                truncate(&issue.title, 58),
                priority_label(issue.priority)
            );
        }
        println!("\n{} sub-issue(s)", items.len());
    }
}

async fn cmd_sub_issues_create(
    cli: &Cli,
    identifier: &str,
    title: &str,
    description: Option<&str>,
    priority: Option<i32>,
) {
    let is_json = is_json_output(cli);
    let (client, _url) = match crate::commands::resolve_client(cli).await {
        Ok(c) => c,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };
    let ws = cli.workspace.as_deref().unwrap_or("acme");
    let path = format!("/api/workspaces/{}/issues/{}/sub-issues", ws, identifier);
    let req = CreateIssueRequest {
        title: title.to_string(),
        description: description.map(|d| d.to_string()),
        priority,
        ..Default::default()
    };
    match client.post::<_, IssueResponse>(&path, &req).await {
        Ok(issue) => {
            if is_json {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&issue).unwrap_or_default()
                );
            } else {
                println!("✓ Created sub-issue {}: {}", issue.identifier, issue.title);
            }
        }
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

// ── resolution helpers ────────────────────────────────────────────

/// Resolve a single filter parameter to UUID, with "me" alias support.
async fn resolve_filter_param(
    resolver: &Resolver,
    kind: ResolveKind,
    cli: &Cli,
    value: Option<&str>,
    is_json: bool,
) -> Option<String> {
    let value = value?;
    // "me" alias for assignee / "me" 别名用于 assignee
    if kind == ResolveKind::Members && value == "me" {
        let (client, _url) = match crate::commands::resolve_client(cli).await {
            Ok(c) => c,
            Err(e) => output::print_string_error(&e, 2, is_json),
        };
        let user: UserResponse = match client.get("/api/auth/me").await {
            Ok(u) => u,
            Err(e) => {
                output::print_error(&e, is_json);
                std::process::exit(e.exit_code());
            }
        };
        return Some(user.id);
    }
    // UUID passthrough / UUID 直通
    if value.contains('-') && value.len() >= 32 {
        return Some(value.to_string());
    }
    // Resolve via resolver / 通过 Resolver 解析
    match resolver.resolve(kind, value).await {
        Ok(id) => Some(id),
        Err(e) => output::print_string_error(&e, 3, is_json),
    }
}

/// Resolve a single optional name to an ID. UUIDs pass through.
/// 将单个可选名称解析为 ID。UUID 直通。
async fn resolve_one(
    resolver: &Resolver,
    kind: ResolveKind,
    name: Option<&str>,
) -> Result<Option<String>, String> {
    match name {
        None => Ok(None),
        Some(n) => resolver.resolve(kind, n).await.map(Some),
    }
}

/// Resolve multiple optional names to IDs. UUIDs/identifiers pass through.
/// 将多个可选名称解析为 ID。UUID/identifier 直通。
async fn resolve_many(
    resolver: &Resolver,
    kind: ResolveKind,
    names: Option<&Vec<String>>,
) -> Result<Option<Vec<String>>, String> {
    match names {
        None => Ok(None),
        Some(list) => {
            // Check if any are UUIDs — if all are UUIDs, skip API calls.
            // 检查是否全是 UUID — 如果全是 UUID，跳过 API 调用。
            let all_uuids = list.iter().all(|n| n.contains('-') && n.len() >= 32);
            if all_uuids {
                return Ok(Some(list.clone()));
            }
            let mut ids = Vec::with_capacity(list.len());
            for name in list {
                ids.push(resolver.resolve(kind, name).await?);
            }
            Ok(Some(ids))
        }
    }
}

/// Resolve a parent issue identifier (e.g. "ENG-25") to its UUID.
/// The backend requires `parent_id` to be a UUID.
/// 将父 issue identifier（如 "ENG-25"）解析为其 UUID。
/// 后端要求 `parent_id` 是 UUID。
async fn resolve_parent_identifier(issues: &Issues, identifier: &str, is_json: bool) -> String {
    // If already looks like a UUID, use directly.
    // 如果看起来已经是 UUID，直接使用。
    if identifier.contains('-') && identifier.len() >= 32 {
        return identifier.to_string();
    }
    // Otherwise, read the parent issue to get its UUID.
    // 否则，读取父 issue 获取其 UUID。
    match issues.read(identifier).await {
        Ok(issue) => issue.id,
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    }
}

/// Resolve a built-in status value (fast path, no API call).
/// 解析内置 status 值（快速路径，无 API 调用）。
///
/// For custom team statuses, the caller must resolve via `/teams/{id}/statuses` separately.
/// 对于自定义团队状态，调用方需通过 `/teams/{id}/statuses` 单独解析。
fn resolve_status_builtin(status: &str, _is_json: bool) -> String {
    let lower = status.to_lowercase();
    if matches!(
        lower.as_str(),
        "backlog" | "todo" | "in_progress" | "in_review" | "done" | "cancelled"
    ) {
        return lower;
    }
    // Not a built-in status — pass through as status_id UUID or custom name.
    // 非内置状态 — 作为 status_id UUID 或自定义名称透传。
    status.to_string()
}

// ── display helpers ───────────────────────────────────────────────

fn truncate(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        s.to_string()
    } else {
        // Truncate by chars, not bytes — slicing at an arbitrary byte index
        // panics inside multi-byte UTF-8 characters (e.g. CJK titles).
        // 按字符数截断而非字节数——按字节下标切片会在多字节 UTF-8 字符(如中文标题)内部 panic。
        let kept: String = s.chars().take(max - 1).collect();
        format!("{kept}…")
    }
}

fn priority_label(p: i32) -> &'static str {
    match p {
        1 => "Urgent",
        2 => "High",
        3 => "Medium",
        4 => "Low",
        _ => "None",
    }
}

fn priority_from_int(p: i32) -> Priority {
    match p {
        1 => Priority::Urgent,
        2 => Priority::High,
        3 => Priority::Medium,
        4 => Priority::Low,
        _ => Priority::None,
    }
}

#[cfg(test)]
mod tests {
    use super::truncate;

    #[test]
    fn truncate_handles_multibyte_utf8() {
        // CJK characters are 3 bytes each; byte-based slicing panics here.
        // 每个中文字符占 3 字节,按字节切片会在这里 panic。
        let s = "Wave 3: 飞书连接器拆分";
        assert_eq!(truncate(s, 12), "Wave 3: 飞书连…");
        // Short strings pass through unchanged. / 短字符串原样返回。
        assert_eq!(truncate("短", 5), "短");
        // Exactly at limit passes through. / 正好达到上限原样返回。
        assert_eq!(truncate("abcde", 5), "abcde");
    }
}
