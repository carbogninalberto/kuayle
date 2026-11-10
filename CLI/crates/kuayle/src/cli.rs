// CLI clap definitions.
// CLI clap 定义。

use clap::{Args, Parser, Subcommand};

#[derive(Parser, Debug)]
#[command(name = "kuayle", version, about, long_about = None, arg_required_else_help = true)]
pub struct Cli {
    #[arg(long, global = true, env = "KUAYLE_PROFILE")]
    pub profile: Option<String>,
    #[arg(long, global = true, env = "KUAYLE_URL")]
    pub url: Option<String>,
    #[arg(long, global = true, env = "KUAYLE_WORKSPACE")]
    pub workspace: Option<String>,
    #[arg(long, global = true, default_value = "auto")]
    pub format: String,
    /// Disable resolve disk cache
    #[arg(
        long,
        global = true,
        env = "KUAYLE_NO_CACHE",
        value_parser = clap::builder::FalseyValueParser::new()
    )]
    pub no_cache: bool,
    #[command(subcommand)]
    pub command: Command,
}

#[derive(Subcommand, Debug, Clone)]
pub enum Command {
    Auth {
        #[command(subcommand)]
        action: AuthAction,
    },
    Whoami,
    Workspaces {
        #[command(subcommand)]
        action: WorkspaceAction,
    },
    Issues {
        #[command(subcommand)]
        action: IssueAction,
    },
    Comments {
        #[command(subcommand)]
        action: CommentAction,
    },
    Relations {
        #[command(subcommand)]
        action: RelationAction,
    },
    Labels {
        #[command(subcommand)]
        action: LabelAction,
    },
    Teams {
        #[command(subcommand)]
        action: TeamAction,
    },
    Statuses {
        #[command(subcommand)]
        action: StatusesAction,
    },
    Projects {
        #[command(subcommand)]
        action: ProjectAction,
    },
    Cycles {
        #[command(subcommand)]
        action: CycleAction,
    },
    Templates {
        #[command(subcommand)]
        action: TemplateAction,
    },
    Views {
        #[command(subcommand)]
        action: ViewAction,
    },
    Members {
        #[command(subcommand)]
        action: MemberAction,
    },
    Favorites {
        #[command(subcommand)]
        action: FavoriteAction,
    },
    Notifications {
        #[command(subcommand)]
        action: NotificationAction,
    },
    Assets {
        #[command(subcommand)]
        action: AssetAction,
    },
    /// Team and project wiki pages: read, write, tree, history, restore
    Wiki {
        #[command(subcommand)]
        action: WikiAction,
    },
    /// A wiki's daily registry: append entries, read the feed and overviews
    Registry {
        #[command(subcommand)]
        action: RegistryAction,
    },
    /// Agent tasks: the queue, one task, its history and runs (read-only)
    AgentTasks {
        #[command(subcommand)]
        action: AgentTaskAction,
    },
    /// Agent workflows and profiles (read-only)
    Workflows {
        #[command(subcommand)]
        action: WorkflowAction,
    },
    /// Show command reference
    Usage,
    /// Generate shell completion script
    Completion {
        shell: String,
    },
    /// Check for updates or update the binary
    SelfUpdate,
}

#[derive(Subcommand, Debug, Clone)]
pub enum AuthAction {
    Login {
        #[arg(long)]
        token: Option<String>,
    },
    Logout,
    Status,
}

#[derive(Subcommand, Debug, Clone)]
pub enum WorkspaceAction {
    List,
}

#[derive(Subcommand, Debug, Clone)]
pub enum IssueAction {
    List {
        #[arg(long)]
        status: Option<String>,
        #[arg(long)]
        priority: Option<i32>,
        /// Assignee email, name, UUID, or "me"
        #[arg(long)]
        assignee: Option<String>,
        #[arg(long)]
        label: Option<String>,
        #[arg(long)]
        team: Option<String>,
        #[arg(long)]
        project: Option<String>,
        #[arg(long)]
        cycle: Option<String>,
        #[arg(long)]
        search: Option<String>,
        #[arg(long)]
        all: bool,
    },
    Read {
        identifier: String,
    },
    Create {
        #[arg(long)]
        title: String,
        #[arg(long)]
        description: Option<String>,
        #[arg(long)]
        priority: Option<i32>,
        #[arg(long)]
        team: Option<String>,
        #[arg(long)]
        assignee: Option<Vec<String>>,
        #[arg(long)]
        labels: Option<Vec<String>>,
        #[arg(long)]
        project: Option<String>,
        #[arg(long)]
        cycle: Option<String>,
        /// Parent issue identifier (e.g. ENG-25)
        #[arg(long)]
        parent: Option<String>,
    },
    Update {
        identifier: String,
        #[arg(long)]
        title: Option<String>,
        #[arg(long)]
        description: Option<String>,
        #[arg(long)]
        status: Option<String>,
        #[arg(long)]
        priority: Option<i32>,
        #[arg(long)]
        assignee: Option<Vec<String>>,
        #[arg(long)]
        labels: Option<Vec<String>>,
        /// Parent issue identifier
        #[arg(long)]
        parent: Option<String>,
    },
    Delete {
        identifier: String,
    },
    BatchUpdate {
        identifiers: Vec<String>,
        #[arg(long)]
        status: Option<String>,
        #[arg(long)]
        priority: Option<i32>,
    },
    BatchDelete {
        identifiers: Vec<String>,
    },
    History {
        identifier: String,
    },
    /// List sub-issues
    SubIssuesList {
        identifier: String,
    },
    /// Create a sub-issue
    SubIssuesCreate {
        identifier: String,
        #[arg(long)]
        title: String,
        #[arg(long)]
        description: Option<String>,
        #[arg(long)]
        priority: Option<i32>,
    },
}

#[derive(Subcommand, Debug, Clone)]
pub enum CommentAction {
    List {
        issue: String,
    },
    Create {
        issue: String,
        #[arg(long)]
        body: String,
    },
    Resolve {
        issue: String,
        id: String,
    },
    Reopen {
        issue: String,
        id: String,
    },
}

#[derive(Subcommand, Debug, Clone)]
pub enum RelationAction {
    List {
        issue: String,
    },
    Create {
        issue: String,
        #[arg(long)]
        related: String,
        #[arg(long, default_value = "related")]
        r#type: String,
    },
    Delete {
        issue: String,
        id: String,
    },
}

#[derive(Subcommand, Debug, Clone)]
pub enum LabelAction {
    List,
    Create {
        #[arg(long)]
        name: String,
        #[arg(long)]
        color: Option<String>,
    },
    Update {
        id: String,
        #[arg(long)]
        name: Option<String>,
        #[arg(long)]
        color: Option<String>,
    },
    Delete {
        id: String,
    },
}

// ── M3 resource action enums ───────────────────────────────────────
// M3 资源 action 枚举

/// Team actions: list teams, read a single team.
#[derive(Subcommand, Debug, Clone)]
pub enum TeamAction {
    List,
    Read { id: String },
}

/// Statuses actions: list statuses for a team.
#[derive(Subcommand, Debug, Clone)]
pub enum StatusesAction {
    List { team: String },
}

/// Project actions: list projects, read a single project.
#[derive(Subcommand, Debug, Clone)]
pub enum ProjectAction {
    List,
    Read { id: String },
}

/// Cycle actions: list, read, burndown, velocity (read-only).
#[derive(Subcommand, Debug, Clone)]
pub enum CycleAction {
    List {
        /// Team ID or key (required — cycles are scoped to a team).
        #[arg(long = "team")]
        team: String,
    },
    Read {
        #[arg(long = "team")]
        team: String,
        id: String,
    },
    Burndown {
        #[arg(long = "team")]
        team: String,
        id: String,
    },
    Velocity {
        #[arg(long = "team")]
        team: String,
    },
}

/// Template actions: full CRUD for issue templates.
#[derive(Subcommand, Debug, Clone)]
pub enum TemplateAction {
    List,
    Read {
        id: String,
    },
    Create {
        #[arg(long)]
        name: String,
        #[arg(long)]
        title: String,
        #[arg(long)]
        description: Option<String>,
    },
    Update {
        id: String,
        #[arg(long)]
        name: Option<String>,
        #[arg(long)]
        title: Option<String>,
        #[arg(long)]
        description: Option<String>,
    },
    Delete {
        id: String,
    },
}

/// View actions: list views, read a single view (read-only).
#[derive(Subcommand, Debug, Clone)]
pub enum ViewAction {
    List,
    Read { id: String },
}

/// Member actions: list workspace members (read-only).
#[derive(Subcommand, Debug, Clone)]
pub enum MemberAction {
    List,
}

/// Favorite actions: list workspace favorites (read-only).
#[derive(Subcommand, Debug, Clone)]
pub enum FavoriteAction {
    List,
}

/// Notification actions: list user notifications (read-only, user-scoped).
#[derive(Subcommand, Debug, Clone)]
pub enum NotificationAction {
    List,
}

/// Asset actions: upload a file, download one by ID.
#[derive(Subcommand, Debug, Clone)]
pub enum AssetAction {
    /// Upload a file (max 10 MB); prints its asset ID and URL
    Upload { file: String },
    /// Download an asset to a file (default ./asset-<id>), or "-" for stdout
    Download { id: String, output: Option<String> },
}

/// Which wiki: exactly one of --team (name, key or UUID) or --project
/// (name or UUID).
#[derive(Args, Debug, Clone)]
#[group(required = true, multiple = false)]
pub struct WikiScopeArgs {
    /// Team name, key or UUID
    #[arg(long)]
    pub team: Option<String>,
    /// Project name or UUID
    #[arg(long)]
    pub project: Option<String>,
}

/// Page content: inline, or from a file ("-" reads stdin). HTML or Markdown.
#[derive(Args, Debug, Clone)]
pub struct WikiContentArgs {
    /// Page content (HTML or Markdown)
    #[arg(long, conflicts_with = "file")]
    pub content: Option<String>,
    /// Read the content from a file, or "-" for stdin
    #[arg(long)]
    pub file: Option<String>,
}

#[derive(Subcommand, Debug, Clone)]
pub enum WikiAction {
    /// List pages (registry days are hidden unless --registry)
    List {
        #[command(flatten)]
        scope: WikiScopeArgs,
        /// Also list registry day pages
        #[arg(long)]
        registry: bool,
    },
    /// Show the page tree (folders and nesting)
    Tree {
        #[command(flatten)]
        scope: WikiScopeArgs,
    },
    /// Read a page
    Read {
        page: String,
        #[command(flatten)]
        scope: WikiScopeArgs,
    },
    /// Create a page or folder
    Create {
        #[command(flatten)]
        scope: WikiScopeArgs,
        #[arg(long)]
        title: String,
        #[command(flatten)]
        body: WikiContentArgs,
        /// Parent page or folder ID
        #[arg(long)]
        parent: Option<String>,
        /// Create a folder instead of a page
        #[arg(long)]
        folder: bool,
    },
    /// Update a page's title and/or content
    Update {
        page: String,
        #[command(flatten)]
        scope: WikiScopeArgs,
        #[arg(long)]
        title: Option<String>,
        #[command(flatten)]
        body: WikiContentArgs,
        /// Version the edit is based on (default: the current version)
        #[arg(long)]
        version: Option<i64>,
    },
    /// Delete a page
    Delete {
        page: String,
        #[command(flatten)]
        scope: WikiScopeArgs,
    },
    /// Move a page under a parent (or top level) at a position
    Move {
        page: String,
        #[command(flatten)]
        scope: WikiScopeArgs,
        /// New parent page or folder ID (omit for the top level)
        #[arg(long)]
        parent: Option<String>,
        /// Position among the new siblings, from 0
        #[arg(long, default_value_t = 0)]
        index: i64,
    },
    /// List a page's revisions, newest first
    History {
        page: String,
        #[command(flatten)]
        scope: WikiScopeArgs,
    },
    /// Restore a page to a revision from its history
    Restore {
        page: String,
        #[command(flatten)]
        scope: WikiScopeArgs,
        /// Revision version to restore (see `wiki history`)
        #[arg(long)]
        version: i64,
        /// Restore the state just before that revision
        #[arg(long)]
        before: bool,
    },
}

#[derive(Subcommand, Debug, Clone)]
pub enum RegistryAction {
    /// Show whether the registry is on; --enable or --disable to change it
    Settings {
        #[command(flatten)]
        scope: WikiScopeArgs,
        #[arg(long, conflicts_with = "disable")]
        enable: bool,
        #[arg(long)]
        disable: bool,
    },
    /// Add an entry to today's (UTC) registry page
    Append {
        #[command(flatten)]
        scope: WikiScopeArgs,
        /// Entry text
        text: String,
    },
    /// Show registry days, newest first
    Feed {
        #[command(flatten)]
        scope: WikiScopeArgs,
        /// Only days before this date (YYYY-MM-DD)
        #[arg(long)]
        before: Option<String>,
        /// Number of days, 1-31
        #[arg(long)]
        limit: Option<u32>,
    },
    /// List AI overviews of the registry, newest first
    Overview {
        #[command(flatten)]
        scope: WikiScopeArgs,
        /// Only overviews generated before this RFC 3339 time
        #[arg(long)]
        before: Option<String>,
        /// Number of overviews, 1-50
        #[arg(long)]
        limit: Option<u32>,
    },
    /// Generate an AI overview for a period (at most 31 days)
    GenerateOverview {
        #[command(flatten)]
        scope: WikiScopeArgs,
        /// First day (YYYY-MM-DD)
        #[arg(long)]
        from: String,
        /// Last day, inclusive (YYYY-MM-DD)
        #[arg(long)]
        to: String,
    },
}

#[derive(Subcommand, Debug, Clone)]
pub enum AgentTaskAction {
    /// List the workspace's agent task queue
    List,
    /// Show one agent task
    Read { id: String },
    /// Show a task's state transitions
    History {
        id: String,
        /// Only transitions after this sequence number
        #[arg(long)]
        after: Option<i64>,
    },
    /// List a task's runs
    Runs { id: String },
}

#[derive(Subcommand, Debug, Clone)]
pub enum WorkflowAction {
    /// List workflows and the workspace defaults
    List,
    /// Show one workflow, including its graph
    Read { id: String },
    /// List agent profiles
    Profiles,
}
