package jira

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/importer"
)

// Minimal Jira JSON shapes. Only fields the importer consumes are modelled;
// unknown fields are ignored, except customfield_* which is captured for
// sprint/epic/other-field discovery.
type jiraNamed struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type jiraKeyed struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Self string `json:"self"`
}

type jiraIssueType struct {
	Name    string `json:"name"`
	Subtask bool   `json:"subtask"`
}

type jiraProject struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type jiraUser struct {
	AccountID    string `json:"accountId"`
	Name         string `json:"name"`
	EmailAddress string `json:"emailAddress"`
	DisplayName  string `json:"displayName"`
}

func (u *jiraUser) source() *importer.SourceUser {
	if u == nil {
		return nil
	}
	id := u.AccountID
	if id == "" {
		id = u.Name
	}
	return &importer.SourceUser{ExternalID: id, Email: u.EmailAddress, DisplayName: u.DisplayName}
}

type jiraAttachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
	Content  string `json:"content"`
}

type jiraIssueLink struct {
	ID           string     `json:"id"`
	Type         jiraNamed  `json:"type"`
	OutwardIssue *jiraKeyed `json:"outwardIssue"`
	InwardIssue  *jiraKeyed `json:"inwardIssue"`
}

type jiraComment struct {
	ID      string          `json:"id"`
	Author  *jiraUser       `json:"author"`
	Body    json.RawMessage `json:"body"`
	Created string          `json:"created"`
	Updated string          `json:"updated"`
}

type jiraComments struct {
	Comments []jiraComment `json:"comments"`
	Total    int           `json:"total"`
	StartAt  int           `json:"startAt"`
}

type jiraChangelog struct {
	Histories []jiraHistory `json:"histories"`
	Total     int           `json:"total"`
}

type jiraHistory struct {
	ID      string            `json:"id"`
	Created string            `json:"created"`
	Author  *jiraUser         `json:"author"`
	Items   []jiraHistoryItem `json:"items"`
}

type jiraHistoryItem struct {
	Field      string `json:"field"`
	FromString string `json:"fromString"`
	ToString   string `json:"toString"`
}

type jiraFields struct {
	Summary     string           `json:"summary"`
	Description json.RawMessage  `json:"description"`
	Status      *jiraNamed       `json:"status"`
	Priority    *jiraNamed       `json:"priority"`
	IssueType   *jiraIssueType   `json:"issuetype"`
	Project     *jiraProject     `json:"project"`
	Parent      *jiraKeyed       `json:"parent"`
	Issuelinks  []jiraIssueLink  `json:"issuelinks"`
	Comment     *jiraComments    `json:"comment"`
	Attachment  []jiraAttachment `json:"attachment"`
	Labels      []string         `json:"labels"`
	Components  []jiraNamed      `json:"components"`
	Created     string           `json:"created"`
	Updated     string           `json:"updated"`
	Reporter    *jiraUser        `json:"reporter"`
	Assignee    *jiraUser        `json:"assignee"`
	Custom      map[string]json.RawMessage
}

func (f *jiraFields) UnmarshalJSON(data []byte) error {
	type plain jiraFields
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*f = jiraFields(p)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.Custom = map[string]json.RawMessage{}
	for key, value := range raw {
		if strings.HasPrefix(key, "customfield_") {
			f.Custom[key] = value
		}
	}
	return nil
}

type jiraIssue struct {
	ID        string        `json:"id"`
	Key       string        `json:"key"`
	Self      string        `json:"self"`
	Fields    jiraFields    `json:"fields"`
	Changelog jiraChangelog `json:"changelog"`
}

type transformConfig struct {
	SprintField string
	EpicField   string
	Site        string
}

var jiraTimeLayouts = []string{
	"2006-01-02T15:04:05.000-0700",
	"2006-01-02T15:04:05-0700",
	"2006-01-02T15:04:05.000Z0700",
	time.RFC3339,
	time.RFC3339Nano,
}

func parseJiraTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range jiraTimeLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

// transformIssue converts one Jira issue into the neutral model. The returned
// notes describe fields that could not be represented and are surfaced in the
// per-item report / preview.
func transformIssue(raw json.RawMessage, comments []jiraComment, changelog jiraChangelog, cfg transformConfig) (importer.SourceIssue, []string, error) {
	var issue jiraIssue
	if err := json.Unmarshal(raw, &issue); err != nil {
		return importer.SourceIssue{}, nil, err
	}
	f := issue.Fields
	notes := []string{}

	description, adfNotes := DescriptionToMarkdown(f.Description)
	notes = append(notes, adfNotes...)

	source := importer.SourceIssue{
		ExternalID:  issue.ID,
		ExternalKey: issue.Key,
		Title:       f.Summary,
		Description: referenceLine(issue) + description,
		CreatedAt:   parseJiraTime(f.Created),
		UpdatedAt:   parseJiraTime(f.Updated),
		Assignee:    f.Assignee.source(),
		Reporter:    f.Reporter.source(),
		SprintIDs:   []string{},
		Labels:      []string{},
	}
	if f.Status != nil {
		source.Status = f.Status.Name
	}
	if f.Priority != nil {
		source.Priority = f.Priority.Name
	}
	if f.Project != nil {
		source.ProjectExternalID = f.Project.ID
	}
	if f.IssueType != nil && (f.IssueType.Subtask || strings.EqualFold(f.IssueType.Name, "sub-task") || strings.EqualFold(f.IssueType.Name, "subtask")) {
		if f.Parent != nil {
			source.ParentExternalID = f.Parent.ID
		}
	}
	// Epics: Cloud exposes a parent link, Data Center a named custom field.
	if epicID := epicExternalID(f, cfg.EpicField); epicID != "" {
		source.EpicExternalID = epicID
	}
	source.SprintIDs = sprintValues(f, cfg.SprintField)

	seen := map[string]bool{}
	for _, label := range f.Labels {
		if label != "" && !seen[label] {
			seen[label] = true
			source.Labels = append(source.Labels, label)
		}
	}
	for _, component := range f.Components {
		if component.Name != "" && !seen[component.Name] {
			seen[component.Name] = true
			source.Labels = append(source.Labels, component.Name)
		}
	}
	sort.Strings(source.Labels)

	for _, link := range f.Issuelinks {
		if link.OutwardIssue != nil && link.OutwardIssue.ID != "" {
			source.Relations = append(source.Relations, importer.SourceRelation{
				ExternalID:       link.ID + ":out",
				SourceExternalID: issue.ID,
				TargetExternalID: link.OutwardIssue.ID,
				Type:             relationType(link.Type.Name, false),
			})
		}
		if link.InwardIssue != nil && link.InwardIssue.ID != "" {
			source.Relations = append(source.Relations, importer.SourceRelation{
				ExternalID:       link.ID + ":in",
				SourceExternalID: issue.ID,
				TargetExternalID: link.InwardIssue.ID,
				Type:             relationType(link.Type.Name, true),
			})
		}
	}

	for _, attachment := range f.Attachment {
		source.Attachments = append(source.Attachments, importer.SourceAttachment{
			ExternalID:  attachment.ID,
			Filename:    attachment.Filename,
			ContentType: attachment.MimeType,
			Size:        attachment.Size,
			URL:         attachment.Content,
		})
	}

	for _, comment := range comments {
		body, commentNotes := DescriptionToMarkdown(comment.Body)
		for _, note := range commentNotes {
			notes = append(notes, "comment "+comment.ID+": "+note)
		}
		source.Comments = append(source.Comments, importer.SourceComment{
			ExternalID: comment.ID,
			Author:     *comment.Author.sourceOrEmpty(),
			Body:       body,
			CreatedAt:  parseJiraTime(comment.Created),
			UpdatedAt:  parseJiraTime(comment.Updated),
		})
	}

	if history := historySummary(changelog); history != "" {
		source.HistoryMarkdown = history
	}
	return source, notes, nil
}

func (u *jiraUser) sourceOrEmpty() *importer.SourceUser {
	if u == nil {
		return &importer.SourceUser{DisplayName: "Unknown"}
	}
	return u.source()
}

func referenceLine(issue jiraIssue) string {
	url := issue.Self
	if url == "" {
		return "> Imported from Jira " + issue.Key + "\n\n"
	}
	return fmt.Sprintf("> Imported from Jira [%s](%s)\n\n", issue.Key, url)
}

func relationType(name string, inward bool) domain.IssueRelationType {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "blocks", "is blocked by":
		if inward {
			return domain.RelationBlockedBy
		}
		return domain.RelationBlocking
	case "duplicate", "duplicates", "is duplicated by":
		return domain.RelationDuplicate
	default:
		return domain.RelationRelated
	}
}

func epicExternalID(f jiraFields, epicField string) string {
	if f.Parent != nil {
		if f.Parent.ID != "" {
			return f.Parent.ID
		}
		return f.Parent.Key
	}
	if epicField == "" {
		return ""
	}
	raw, ok := f.Custom[epicField]
	if !ok || len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var keyed jiraKeyed
	if err := json.Unmarshal(raw, &keyed); err == nil && keyed.ID != "" {
		return keyed.ID
	}
	return ""
}

func sprintValues(f jiraFields, sprintField string) []string {
	if sprintField == "" {
		return nil
	}
	raw, ok := f.Custom[sprintField]
	if !ok || len(raw) == 0 {
		return nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		var text string
		if err := json.Unmarshal(value, &text); err == nil && text != "" && !seen[text] {
			seen[text] = true
			out = append(out, text)
			continue
		}
		var obj struct {
			ID   any    `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(value, &obj); err == nil && obj.Name != "" && !seen[obj.Name] {
			seen[obj.Name] = true
			out = append(out, obj.Name)
		}
	}
	return out
}

func historySummary(changelog jiraChangelog) string {
	if len(changelog.Histories) == 0 {
		return ""
	}
	var buf strings.Builder
	buf.WriteString("## History\n")
	for _, entry := range changelog.Histories {
		author := "Unknown"
		if entry.Author != nil && entry.Author.DisplayName != "" {
			author = entry.Author.DisplayName
		}
		for _, item := range entry.Items {
			line := fmt.Sprintf("- %s %s changed %s", parseJiraTime(entry.Created).Format("2006-01-02"), author, item.Field)
			if item.FromString != "" || item.ToString != "" {
				line += fmt.Sprintf(": %s -> %s", displayValue(item.FromString), displayValue(item.ToString))
			}
			buf.WriteString(line + "\n")
		}
	}
	return strings.TrimSpace(buf.String())
}

func displayValue(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}
