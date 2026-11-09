// Type-safe IssueFilter builder for kuayle issue queries.
// kuayle issue 查询的类型安全 filter builder。
//
// Translates method calls into query-string parameters matching
// the backend `dto.IssueFilterParams` struct.
// 将方法调用翻译为与后端 `dto.IssueFilterParams` 匹配的 query 参数。

use serde::Serialize;

/// Builder for constructing issue list filter parameters.
/// 构造 issue 列表过滤参数的 builder。
///
/// Every setter maps to a known backend filter field, giving
/// compile-time safety against typos (unlike `json!()`).
/// 每个 setter 映射到已知的后端过滤字段，编译期防止拼写错误。
#[derive(Debug, Clone, Default, Serialize)]
pub struct IssueFilter {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub status_type: Option<StatusCategory>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub priority: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub assignee: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub creator: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub team: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub project: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub cycle: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub label: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub search: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub due_before: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub due_after: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub triaged: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub sub_issues: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub parent_id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub group_by: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub sort: Option<IssueSort>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub order: Option<Order>,
}

/// Built-in issue status categories.
/// 内置 issue 状态类别。
#[derive(Debug, Clone, Copy, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum StatusCategory {
    Backlog,
    Todo,
    InProgress,
    InReview,
    Done,
    Cancelled,
}

/// Priority levels (matching backend int values).
/// 优先级级别（匹配后端 int 值）。
#[derive(Debug, Clone, Copy, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum Priority {
    /// 0 = No priority / 无优先级
    None = 0,
    /// 1 = Urgent / 紧急
    Urgent = 1,
    /// 2 = High / 高
    High = 2,
    /// 3 = Medium / 中
    Medium = 3,
    /// 4 = Low / 低
    Low = 4,
}

/// Sort field for issue listing.
/// issue 列表的排序字段。
#[derive(Debug, Clone, Copy, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum IssueSort {
    CreatedAt,
    UpdatedAt,
    Priority,
    SortOrder,
    #[serde(rename = "status")]
    Status,
}

/// Sort order.
/// 排序方向。
#[derive(Debug, Clone, Copy, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum Order {
    Asc,
    Desc,
}

impl IssueFilter {
    /// Create a new empty filter.
    /// 创建新的空 filter。
    pub fn new() -> Self {
        IssueFilter::default()
    }

    /// Filter by status value (built-in or custom status name/slug).
    /// 按 status 值过滤（内置或自定义状态名称/slug）。
    pub fn status(mut self, s: impl Into<String>) -> Self {
        self.status = Some(s.into());
        self
    }

    /// Filter by status category enum.
    /// 按状态类别枚举过滤。
    pub fn status_type(mut self, s: StatusCategory) -> Self {
        self.status_type = Some(s);
        self
    }

    /// Filter by priority (0-4 or enum).
    /// 按优先级过滤（0-4 或枚举）。
    pub fn priority(mut self, p: impl Into<Priority>) -> Self {
        self.priority = Some(p.into() as i32);
        self
    }

    /// Filter by assignee user ID (UUID).
    /// 按指派人用户 ID 过滤（UUID）。
    pub fn assignee(mut self, id: impl Into<String>) -> Self {
        self.assignee = Some(id.into());
        self
    }

    /// Filter by creator user ID (UUID).
    /// 按创建者用户 ID 过滤（UUID）。
    pub fn creator(mut self, id: impl Into<String>) -> Self {
        self.creator = Some(id.into());
        self
    }

    /// Filter by team ID (UUID).
    /// 按团队 ID 过滤（UUID）。
    pub fn team(mut self, id: impl Into<String>) -> Self {
        self.team = Some(id.into());
        self
    }

    /// Filter by project ID (UUID).
    /// 按项目 ID 过滤（UUID）。
    pub fn project(mut self, id: impl Into<String>) -> Self {
        self.project = Some(id.into());
        self
    }

    /// Filter by cycle ID (UUID).
    /// 按周期 ID 过滤（UUID）。
    pub fn cycle(mut self, id: impl Into<String>) -> Self {
        self.cycle = Some(id.into());
        self
    }

    /// Filter by label ID (UUID) or name.
    /// 按标签 ID（UUID）或名称过滤。
    pub fn label(mut self, id: impl Into<String>) -> Self {
        self.label = Some(id.into());
        self
    }

    /// Full-text search across title and description.
    /// 全文搜索标题和描述。
    pub fn search(mut self, q: impl Into<String>) -> Self {
        self.search = Some(q.into());
        self
    }

    /// Filter issues due before a date (ISO 8601).
    /// 过滤截止日期之前的 issue（ISO 8601）。
    pub fn due_before(mut self, date: impl Into<String>) -> Self {
        self.due_before = Some(date.into());
        self
    }

    /// Filter issues due after a date (ISO 8601).
    /// 过滤截止日期之后的 issue（ISO 8601）。
    pub fn due_after(mut self, date: impl Into<String>) -> Self {
        self.due_after = Some(date.into());
        self
    }

    /// Filter by triage status.
    /// 按 triage 状态过滤。
    pub fn triaged(mut self, t: bool) -> Self {
        self.triaged = Some(t);
        self
    }

    /// Include sub-issues in results.
    /// 在结果中包含子 issue。
    pub fn sub_issues(mut self, s: bool) -> Self {
        self.sub_issues = Some(s);
        self
    }

    /// Filter by parent issue ID (UUID).
    /// 按父 issue ID 过滤（UUID）。
    pub fn parent_id(mut self, id: impl Into<String>) -> Self {
        self.parent_id = Some(id.into());
        self
    }

    /// Group results by a field.
    /// 按字段分组结果。
    pub fn group_by(mut self, field: impl Into<String>) -> Self {
        self.group_by = Some(field.into());
        self
    }

    /// Sort results by a field.
    /// 按字段排序结果。
    pub fn sort(mut self, sort: IssueSort, order: Order) -> Self {
        self.sort = Some(sort);
        self.order = Some(order);
        self
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_filter_serializes_no_fields() {
        let filter = IssueFilter::new();
        let json = serde_json::to_value(&filter).unwrap();
        assert_eq!(json, serde_json::json!({}));
    }

    #[test]
    fn status_filter_serializes() {
        let filter = IssueFilter::new().status("in_progress");
        let v = serde_json::to_value(&filter).unwrap();
        assert_eq!(v["status"], "in_progress");
    }

    #[test]
    fn priority_filter_serializes() {
        let filter = IssueFilter::new().priority(Priority::Urgent);
        let v = serde_json::to_value(&filter).unwrap();
        assert_eq!(v["priority"], 1);
    }

    #[test]
    fn full_filter_chaining() {
        let filter = IssueFilter::new()
            .status_type(StatusCategory::InProgress)
            .priority(Priority::High)
            .assignee("u1")
            .search("crash")
            .sort(IssueSort::UpdatedAt, Order::Desc);

        let v = serde_json::to_value(&filter).unwrap();
        assert_eq!(v["status_type"], "in_progress");
        assert_eq!(v["priority"], 2);
        assert_eq!(v["assignee"], "u1");
        assert_eq!(v["search"], "crash");
        assert_eq!(v["sort"], "updated_at");
        assert_eq!(v["order"], "desc");
    }

    #[test]
    fn filter_label_and_team() {
        let filter = IssueFilter::new()
            .label("d0000000-0000-0000-0000-000000000001")
            .team("c0000000-0000-0000-0000-000000000001")
            .project("e0000000-0000-0000-0000-000000000001");
        let v = serde_json::to_value(&filter).unwrap();
        assert_eq!(v["label"], "d0000000-0000-0000-0000-000000000001");
        assert_eq!(v["team"], "c0000000-0000-0000-0000-000000000001");
    }

    #[test]
    fn priority_enum_values() {
        assert_eq!(Priority::None as i32, 0);
        assert_eq!(Priority::Urgent as i32, 1);
        assert_eq!(Priority::High as i32, 2);
        assert_eq!(Priority::Medium as i32, 3);
        assert_eq!(Priority::Low as i32, 4);
    }
}
