package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/importer"
)

const searchPageSize = 100

func attributionName(user importer.SourceUser) string {
	if strings.TrimSpace(user.DisplayName) != "" {
		return strings.TrimSpace(user.DisplayName)
	}
	if strings.TrimSpace(user.Email) != "" {
		return strings.TrimSpace(user.Email)
	}
	if user.ExternalID != "" {
		return user.ExternalID
	}
	return "Unknown"
}

// WithJQL limits which issues are imported. An empty JQL means all issues.
func (c *Client) WithJQL(jql string) *Client {
	if strings.TrimSpace(jql) == "" {
		c.jql = "order by created ASC"
	} else {
		c.jql = jql
	}
	return c
}

// discoverFields finds the sprint and Epic Link custom fields. It is
// best-effort: a restricted account simply leaves these unmapped rather than
// failing the whole import.
func (c *Client) discoverFields(ctx context.Context) {
	var fields []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Custom bool   `json:"custom"`
		Schema struct {
			Custom string `json:"custom"`
		} `json:"schema"`
	}
	if err := c.getJSON(ctx, c.apiPath("/field"), &fields); err != nil {
		return
	}
	for _, field := range fields {
		schema := strings.ToLower(field.Schema.Custom)
		name := strings.ToLower(field.Name)
		if c.sprintField == "" && (strings.Contains(schema, "gh-sprint") || strings.Contains(schema, "sprint") || name == "sprint") {
			c.sprintField = field.ID
		}
		if c.epicField == "" && !c.cloud && (strings.Contains(schema, "gh-epic-link") || name == "epic link") {
			c.epicField = field.ID
		}
	}
}

func (c *Client) search(ctx context.Context, page func(json.RawMessage) error) error {
	jql := c.jql
	if jql == "" {
		jql = "order by created ASC"
	}
	if c.cloud {
		token := ""
		for {
			body := map[string]any{
				"jql":        jql,
				"maxResults": searchPageSize,
				"fields":     []string{"*all"},
			}
			if token != "" {
				body["nextPageToken"] = token
			}
			var result struct {
				Issues        []json.RawMessage `json:"issues"`
				NextPageToken string            `json:"nextPageToken"`
			}
			if err := c.postJSON(ctx, c.apiPath("/search/jql"), body, &result); err != nil {
				return err
			}
			for _, raw := range result.Issues {
				if err := page(raw); err != nil {
					return err
				}
			}
			if result.NextPageToken == "" || len(result.Issues) == 0 {
				return nil
			}
			token = result.NextPageToken
		}
	}

	startAt := 0
	for {
		params := url.Values{}
		params.Set("jql", jql)
		params.Set("startAt", strconv.Itoa(startAt))
		params.Set("maxResults", strconv.Itoa(searchPageSize))
		params.Set("expand", "changelog")
		var result struct {
			Issues []json.RawMessage `json:"issues"`
			Total  int               `json:"total"`
		}
		if err := c.getJSON(ctx, c.apiPath("/search?"+params.Encode()), &result); err != nil {
			return err
		}
		for _, raw := range result.Issues {
			if err := page(raw); err != nil {
				return err
			}
		}
		startAt += len(result.Issues)
		if len(result.Issues) == 0 || (result.Total > 0 && startAt >= result.Total) {
			return nil
		}
	}
}

func (c *Client) comments(ctx context.Context, issueKey string) ([]jiraComment, error) {
	all := []jiraComment{}
	startAt := 0
	for {
		params := url.Values{}
		params.Set("startAt", strconv.Itoa(startAt))
		params.Set("maxResults", strconv.Itoa(searchPageSize))
		var result jiraComments
		if err := c.getJSON(ctx, c.apiPath("/issue/"+url.PathEscape(issueKey)+"/comment?"+params.Encode()), &result); err != nil {
			return nil, err
		}
		all = append(all, result.Comments...)
		startAt += len(result.Comments)
		if len(result.Comments) == 0 || (result.Total > 0 && startAt >= result.Total) {
			return all, nil
		}
	}
}

func (c *Client) changelog(ctx context.Context, issueID string) (jiraChangelog, error) {
	if !c.cloud {
		// Server/DC returns the changelog expanded in the search result.
		return jiraChangelog{}, nil
	}
	out := jiraChangelog{}
	startAt := 0
	for {
		params := url.Values{}
		params.Set("startAt", strconv.Itoa(startAt))
		params.Set("maxResults", strconv.Itoa(searchPageSize))
		var result struct {
			Values []jiraHistory `json:"values"`
			Total  int           `json:"total"`
		}
		if err := c.getJSON(ctx, c.apiPath("/issue/"+url.PathEscape(issueID)+"/changelog?"+params.Encode()), &result); err != nil {
			return out, err
		}
		out.Histories = append(out.Histories, result.Values...)
		out.Total += len(result.Values)
		startAt += len(result.Values)
		if len(result.Values) == 0 || (result.Total > 0 && startAt >= result.Total) {
			return out, nil
		}
	}
}

// Preview fetches the source and reports counts and values without writing
// anything outside the import job that requested it.
func (c *Client) Preview(ctx context.Context) (*importer.SourcePreview, error) {
	c.discoverFields(ctx)
	preview := &importer.SourcePreview{Counts: map[string]int{}}
	statuses, priorities, labels := map[string]bool{}, map[string]bool{}, map[string]bool{}
	users := map[string]importer.SourceUser{}
	projects := map[string]importer.SourceProject{}
	unmapped := map[string]bool{}
	addUnmapped := func(text string) { unmapped[text] = true }

	err := c.search(ctx, func(raw json.RawMessage) error {
		var issue jiraIssue
		if err := json.Unmarshal(raw, &issue); err != nil {
			return err
		}
		f := issue.Fields
		preview.Counts["issues"]++
		if f.IssueType != nil && (f.IssueType.Subtask || strings.EqualFold(f.IssueType.Name, "sub-task")) {
			preview.Counts["sub_issues"]++
		}
		if f.Status != nil && f.Status.Name != "" {
			statuses[f.Status.Name] = true
		}
		if f.Priority != nil && f.Priority.Name != "" {
			priorities[f.Priority.Name] = true
		}
		for _, user := range []*jiraUser{f.Reporter, f.Assignee} {
			if user != nil {
				source := *user.source()
				key := source.ExternalID + "|" + source.Email
				users[key] = source
				if source.Email == "" {
					addUnmapped("user without email: " + attributionName(source))
				}
			}
		}
		if f.Project != nil {
			projects[f.Project.ID] = importer.SourceProject{ExternalID: f.Project.ID, Key: f.Project.Key, Name: f.Project.Name}
		}
		for _, label := range f.Labels {
			labels[label] = true
		}
		for _, component := range f.Components {
			labels[component.Name] = true
		}
		if f.Comment != nil {
			preview.Counts["comments"] += f.Comment.Total
		}
		preview.Counts["attachments"] += len(f.Attachment)
		for _, link := range f.Issuelinks {
			if link.OutwardIssue != nil || link.InwardIssue != nil {
				preview.Counts["links"]++
			}
		}
		if len(sprintValues(f, c.sprintField)) > 0 {
			preview.Counts["sprints"]++
		}
		if epicExternalID(f, c.epicField) != "" {
			preview.Counts["epics"]++
		}
		for key := range f.Custom {
			if key != c.sprintField && key != c.epicField {
				addUnmapped("custom field: " + key)
			}
		}
		_, notes := DescriptionToMarkdown(f.Description)
		for _, note := range notes {
			addUnmapped(note)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for status := range statuses {
		preview.Statuses = append(preview.Statuses, status)
	}
	for priority := range priorities {
		preview.Priorities = append(preview.Priorities, priority)
	}
	for label := range labels {
		preview.Labels = append(preview.Labels, label)
	}
	for _, user := range users {
		preview.Users = append(preview.Users, user)
	}
	for _, project := range projects {
		preview.Projects = append(preview.Projects, project)
	}
	for unmappedField := range unmapped {
		preview.UnmappedFields = append(preview.UnmappedFields, unmappedField)
	}
	sort.Strings(preview.Statuses)
	sort.Strings(preview.Priorities)
	sort.Strings(preview.Labels)
	sort.Strings(preview.UnmappedFields)
	sort.Slice(preview.Users, func(i, j int) bool { return preview.Users[i].ExternalID < preview.Users[j].ExternalID })
	sort.Slice(preview.Projects, func(i, j int) bool { return preview.Projects[i].ExternalID < preview.Projects[j].ExternalID })
	return preview, nil
}

// Fetch streams each fully enriched issue to emit.
func (c *Client) Fetch(ctx context.Context, emit importer.EmitFunc) error {
	c.discoverFields(ctx)
	cfg := transformConfig{SprintField: c.sprintField, EpicField: c.epicField, Site: c.site}
	return c.search(ctx, func(raw json.RawMessage) error {
		var probe jiraIssue
		if err := json.Unmarshal(raw, &probe); err != nil {
			return err
		}
		comments, err := c.comments(ctx, probe.Key)
		if err != nil {
			return fmt.Errorf("comments for issue %s: %w", probe.Key, err)
		}
		changelog, err := c.changelog(ctx, probe.ID)
		if err != nil {
			return fmt.Errorf("changelog for issue %s: %w", probe.Key, err)
		}
		if probe.Changelog.Histories != nil {
			changelog = probe.Changelog
		}
		issue, _, err := transformIssue(raw, comments, changelog, cfg)
		if err != nil {
			return err
		}
		return emit(issue)
	})
}
