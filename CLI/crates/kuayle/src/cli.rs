// CLI clap definitions.
// CLI clap 定义。

use clap::{Parser, Subcommand};

#[derive(Parser, Debug)]
#[command(name = "kuayle", version, about, long_about = None)]
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
    #[arg(long, global = true, env = "KUAYLE_NO_CACHE")]
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
    Subscribe {
        identifier: String,
    },
    Unsubscribe {
        identifier: String,
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

/// Asset actions: read asset info, upload file (upload is a placeholder).
#[derive(Subcommand, Debug, Clone)]
pub enum AssetAction {
    List,
    Read { id: String },
    Upload { file: String },
    Download { id: String, output: Option<String> },
}
