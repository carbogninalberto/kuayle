// Resource registry and CRUD engine (§6.2).
// 资源注册表与 CRUD 引擎 (§6.2)。
#![allow(dead_code)] // Engine wiring in progress
                     //
                     // Declarative resource specs drive CLI command generation
                     // and usage docs. Each resource declares its REST path,
                     // ID kind, name resolution strategy, and capabilities.
                     // 声明式 resource spec 驱动 CLI 命令生成和 usage 文档。
                     // 每个资源声明其 REST 路径、ID 类型、名称解析策略和操作能力。

use bitflags::bitflags;
use serde_json::Value;

/// Type alias for table row extraction / 表格行提取的类型别名
pub type RowFn = fn(&Value) -> Vec<String>;
/// Type alias for detail printing / 详情打印的类型别名
pub type DetailFn = fn(&Value);
/// Type alias for header generation / 表头生成的类型别名  
pub type HeadersFn = fn() -> Vec<&'static str>;

/// What kind of ID does a resource use for read/update/delete?
/// 资源在 read/update/delete 时使用什么类型的 ID？
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum IdKind {
    /// Standard UUID / 标准 UUID
    Uuid,
    /// Issue identifier like "ENG-25" / issue identifier 如 "ENG-25"
    IssueIdentifier,
    /// Cycle number (integer) / cycle 编号（整数）
    CycleNumber,
    /// Team-scoped status UUID / team 范围的 status UUID
    TeamStatusUuid,
}

bitflags! {
    /// Which CRUD operations a resource supports.
    /// 资源支持哪些 CRUD 操作。
    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub struct Capabilities: u8 {
        const LIST   = 0b00001;
        const READ   = 0b00010;
        const CREATE = 0b00100;
        const UPDATE = 0b01000;
        const DELETE = 0b10000;
        /// All five standard CRUD operations.
        /// 全部五种标准 CRUD 操作。
        const CRUD   = Self::LIST.bits() | Self::READ.bits() | Self::CREATE.bits() | Self::UPDATE.bits() | Self::DELETE.bits();
        /// Read-only / 只读
        const READ_ONLY = Self::LIST.bits() | Self::READ.bits();
    }
}

/// A registered resource in the CLI.
/// CLI 中注册的资源。
pub struct ResourceSpec {
    /// Resource name (e.g. "teams", "projects") / 资源名称
    pub name: &'static str,
    /// Short description for --help / --help 中的简短描述
    pub about: &'static str,
    /// REST path template with {ws} placeholder / 含 {ws} 占位符的 REST 路径模板
    pub path: &'static str,
    /// What kind of ID does this resource use? / 此资源使用什么类型的 ID？
    pub id_kind: IdKind,
    /// Capabilities supported / 支持的操作能力
    pub capabilities: Capabilities,
    /// Table headers for list output / list 输出的表头
    pub headers_fn: HeadersFn,
    /// Extract a table row from a JSON item / 从 JSON item 提取表格行
    pub row_fn: RowFn,
    /// Print detail view for a single item / 打印单个项目的详情视图
    pub detail_fn: DetailFn,
}

impl ResourceSpec {
    pub fn build_path(&self, workspace: &str) -> String {
        self.path.replace("{ws}", workspace)
    }
    pub fn build_item_path(&self, workspace: &str, id: &str) -> String {
        format!("{}/{}", self.build_path(workspace), id)
    }
    pub fn can_list(&self) -> bool {
        self.capabilities.contains(Capabilities::LIST)
    }
    pub fn can_read(&self) -> bool {
        self.capabilities.contains(Capabilities::READ)
    }
    pub fn can_create(&self) -> bool {
        self.capabilities.contains(Capabilities::CREATE)
    }
    pub fn can_update(&self) -> bool {
        self.capabilities.contains(Capabilities::UPDATE)
    }
    pub fn can_delete(&self) -> bool {
        self.capabilities.contains(Capabilities::DELETE)
    }
}

// ── Shared formatting helpers for ResourceSpec ────────────────────

fn s(item: &Value, key: &str) -> String {
    item[key].as_str().unwrap_or("-").to_string()
}

fn teams_headers() -> Vec<&'static str> {
    vec!["NAME", "KEY", "DESCRIPTION"]
}
fn teams_row(item: &Value) -> Vec<String> {
    vec![
        s(item, "name"),
        s(item, "key"),
        truncate_str(item, "description", 40),
    ]
}
fn teams_detail(item: &Value) {
    println!("Name:        {}", s(item, "name"));
    println!("Key:         {}", s(item, "key"));
    if let Some(d) = item["description"].as_str() {
        println!("Description: {d}");
    }
    println!("ID:          {}", s(item, "id"));
}

fn projects_headers() -> Vec<&'static str> {
    vec!["NAME", "STATUS", "PROGRESS"]
}
fn projects_row(item: &Value) -> Vec<String> {
    let name = s(item, "name");
    let status = s(item, "status");
    let progress = item
        .get("progress")
        .map(|p| {
            let t = p["total"].as_u64().unwrap_or(0);
            let c = p["completed"].as_u64().unwrap_or(0);
            format!("{}/{}", c, t)
        })
        .unwrap_or_default();
    vec![name, status, progress]
}
fn projects_detail(item: &Value) {
    println!("Name:        {}", s(item, "name"));
    println!("Status:      {}", s(item, "status"));
    if let Some(d) = item["description"].as_str() {
        println!("Description: {d}");
    }
    if let Some(p) = item.get("progress") {
        println!(
            "Progress:    {}/{} completed",
            p["completed"].as_u64().unwrap_or(0),
            p["total"].as_u64().unwrap_or(0)
        );
    }
    println!("ID:          {}", s(item, "id"));
}

fn cycles_headers() -> Vec<&'static str> {
    vec!["NAME", "NUMBER", "STATUS"]
}
fn cycles_row(item: &Value) -> Vec<String> {
    vec![
        s(item, "name"),
        item["number"].to_string(),
        s(item, "status"),
    ]
}
fn cycles_detail(item: &Value) {
    println!("Name:        {}", s(item, "name"));
    println!("Number:      {}", item["number"]);
    println!("Status:      {}", s(item, "status"));
    if let Some(d) = item["description"].as_str() {
        println!("Description: {d}");
    }
    println!("ID:          {}", s(item, "id"));
}

fn templates_headers() -> Vec<&'static str> {
    vec!["NAME", "TITLE"]
}
fn templates_row(item: &Value) -> Vec<String> {
    vec![s(item, "name"), s(item, "title")]
}
fn templates_detail(item: &Value) {
    println!("Name:        {}", s(item, "name"));
    println!("Title:       {}", s(item, "title"));
    if let Some(d) = item["description"].as_str() {
        println!("Description: {d}");
    }
    println!("ID:          {}", s(item, "id"));
}

fn views_headers() -> Vec<&'static str> {
    vec!["NAME", "DESCRIPTION"]
}
fn views_row(item: &Value) -> Vec<String> {
    vec![s(item, "name"), truncate_str(item, "description", 50)]
}
fn views_detail(item: &Value) {
    println!("Name:        {}", s(item, "name"));
    if let Some(d) = item["description"].as_str() {
        println!("Description: {d}");
    }
    println!("ID:          {}", s(item, "id"));
}

fn members_headers() -> Vec<&'static str> {
    vec!["NAME", "EMAIL", "ROLE"]
}
fn members_row(item: &Value) -> Vec<String> {
    vec![s(item, "name"), s(item, "email"), s(item, "role")]
}
fn members_detail(item: &Value) {
    println!("Name:  {}", s(item, "name"));
    println!("Email: {}", s(item, "email"));
    println!("Role:  {}", s(item, "role"));
}

fn favorites_headers() -> Vec<&'static str> {
    vec!["TYPE", "ID"]
}
fn favorites_row(item: &Value) -> Vec<String> {
    vec![s(item, "favoritable_type"), s(item, "favoritable_id")]
}
fn favorites_detail(item: &Value) {
    println!("Type: {}", s(item, "favoritable_type"));
    println!("ID:   {}", s(item, "favoritable_id"));
}

fn notifications_headers() -> Vec<&'static str> {
    vec!["TYPE", "TITLE"]
}
fn notifications_row(item: &Value) -> Vec<String> {
    vec![s(item, "type"), truncate_str(item, "title", 60)]
}
fn notifications_detail(item: &Value) {
    println!("Type:  {}", s(item, "type"));
    println!("Title: {}", s(item, "title"));
    if let Some(b) = item["body"].as_str() {
        println!("Body:  {b}");
    }
}

fn assets_headers() -> Vec<&'static str> {
    vec!["FILENAME", "TYPE", "SIZE"]
}
fn assets_row(item: &Value) -> Vec<String> {
    let size = item["size"].as_u64().unwrap_or(0);
    vec![
        s(item, "filename"),
        s(item, "content_type"),
        format!("{}", size),
    ]
}
fn assets_detail(item: &Value) {
    println!("Filename:     {}", s(item, "filename"));
    println!("Content-Type: {}", s(item, "content_type"));
    println!("Size:         {}", item["size"]);
    if let Some(u) = item["url"].as_str() {
        println!("URL:          {u}");
    }
}

fn truncate_str(item: &Value, key: &str, max: usize) -> String {
    let s = item[key].as_str().unwrap_or("-");
    if s.chars().count() <= max {
        s.to_string()
    } else {
        // Truncate by chars, not bytes — byte slicing panics inside multi-byte UTF-8.
        // 按字符数截断而非字节数——按字节切片会在多字节 UTF-8 字符内部 panic。
        let kept: String = s.chars().take(max - 1).collect();
        format!("{kept}…")
    }
}

/// The static registry of all kuayle CLI resources.
/// kuayle CLI 所有资源的静态注册表。
///
/// Each entry drives CLI subcommand generation automatically.
/// 每个条目自动驱动 CLI 子命令生成。
pub static RESOURCES: &[ResourceSpec] = &[
    // ── Teams ──────────────────────────────────────────────────
    ResourceSpec {
        name: "teams",
        about: "Manage teams",
        path: "/api/workspaces/{ws}/teams",
        id_kind: IdKind::Uuid,
        capabilities: Capabilities::READ_ONLY,
        headers_fn: teams_headers,
        row_fn: teams_row,
        detail_fn: teams_detail,
    },
    ResourceSpec {
        name: "projects",
        about: "Manage projects",
        path: "/api/workspaces/{ws}/projects",
        id_kind: IdKind::Uuid,
        capabilities: Capabilities::READ_ONLY,
        headers_fn: projects_headers,
        row_fn: projects_row,
        detail_fn: projects_detail,
    },
    ResourceSpec {
        name: "cycles",
        about: "Manage cycles (read-only with PAT)",
        path: "/api/workspaces/{ws}/teams/{team}/cycles",
        id_kind: IdKind::CycleNumber,
        capabilities: Capabilities::READ_ONLY,
        headers_fn: cycles_headers,
        row_fn: cycles_row,
        detail_fn: cycles_detail,
    },
    ResourceSpec {
        name: "templates",
        about: "Manage issue templates",
        path: "/api/workspaces/{ws}/issue-templates",
        id_kind: IdKind::Uuid,
        capabilities: Capabilities::CRUD,
        headers_fn: templates_headers,
        row_fn: templates_row,
        detail_fn: templates_detail,
    },
    ResourceSpec {
        name: "views",
        about: "Manage views (read-only with PAT)",
        path: "/api/workspaces/{ws}/views",
        id_kind: IdKind::Uuid,
        capabilities: Capabilities::READ_ONLY,
        headers_fn: views_headers,
        row_fn: views_row,
        detail_fn: views_detail,
    },
    ResourceSpec {
        name: "members",
        about: "Manage workspace members",
        path: "/api/workspaces/{ws}/members",
        id_kind: IdKind::Uuid,
        capabilities: Capabilities::READ_ONLY,
        headers_fn: members_headers,
        row_fn: members_row,
        detail_fn: members_detail,
    },
    ResourceSpec {
        name: "favorites",
        about: "Manage favorites (read-only with PAT)",
        path: "/api/workspaces/{ws}/favorites",
        id_kind: IdKind::Uuid,
        capabilities: Capabilities::READ_ONLY,
        headers_fn: favorites_headers,
        row_fn: favorites_row,
        detail_fn: favorites_detail,
    },
    ResourceSpec {
        name: "notifications",
        about: "Manage notifications (read-only with PAT)",
        path: "/api/notifications",
        id_kind: IdKind::Uuid,
        capabilities: Capabilities::READ_ONLY,
        headers_fn: notifications_headers,
        row_fn: notifications_row,
        detail_fn: notifications_detail,
    },
    ResourceSpec {
        name: "assets",
        about: "Manage uploaded assets",
        path: "/api/workspaces/{ws}/assets",
        id_kind: IdKind::Uuid,
        capabilities: Capabilities::from_bits_truncate(
            Capabilities::LIST.bits() | Capabilities::READ.bits() | Capabilities::CREATE.bits(),
        ),
        headers_fn: assets_headers,
        row_fn: assets_row,
        detail_fn: assets_detail,
    },
];

#[cfg(test)]
mod tests {
    use super::truncate_str;

    #[test]
    fn truncate_str_handles_multibyte_utf8() {
        let v = serde_json::json!({"title": "Wave 5: 前端通用化设计"});
        assert_eq!(truncate_str(&v, "title", 12), "Wave 5: 前端通…");
        let short = serde_json::json!({"title": "短"});
        assert_eq!(truncate_str(&short, "title", 5), "短");
    }
}
