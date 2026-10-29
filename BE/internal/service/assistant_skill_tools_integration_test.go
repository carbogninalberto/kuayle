package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func withSkills(t *testing.T, f toolFixture) *repository.AssistantSkillRepository {
	t.Helper()
	skills := repository.NewAssistantSkillRepository(f.db)
	f.service.tools.SetSkills(skills)
	f.service.tools.SetSettings(repository.NewAssistantToolSettingsRepository(f.db))
	return skills
}

func setRole(t *testing.T, f toolFixture, role string) {
	t.Helper()
	_, err := f.db.Exec(`UPDATE workspace_members SET role=$3 WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user, role)
	require.NoError(t, err)
}

func addSkill(t *testing.T, f toolFixture, skills *repository.AssistantSkillRepository, name, status string, team *uuid.UUID) *domain.AssistantSkill {
	t.Helper()
	setRole(t, f, "admin")
	defer setRole(t, f, "member")
	skill, err := skills.Create(f.ctx, f.ws, f.user, domain.AssistantSkillInput{TeamID: team, Name: name, Description: "Use when " + strings.ToLower(name), Body: "## " + name + "\n1. Tag the release\n2. Announce it", Status: status, AuthorKind: "user"})
	require.NoError(t, err)
	return skill
}

func toolCallRow(t *testing.T, f toolFixture, message uuid.UUID) (string, string, map[string]any) {
	t.Helper()
	var status string
	var failure *string
	var output []byte
	require.NoError(t, f.db.QueryRow(`SELECT status,error,output FROM conversation_tool_calls WHERE message_id=$1`, message).Scan(&status, &failure, &output))
	result := map[string]any{}
	if output != nil {
		require.NoError(t, json.Unmarshal(output, &result))
	}
	if failure == nil {
		return status, "", result
	}
	return status, *failure, result
}

// The system prompt lists only active, enabled skills the creator can see, by
// name and description; the tool switches turn the listing off.
func TestSkillsPromptListsActiveVisibleSkills(t *testing.T) {
	f := newToolFixture(t)
	skills := withSkills(t, f)
	a := f.attempt(t, "How do we release?")
	prompt, err := f.service.skillsPrompt(f.ctx, a)
	require.NoError(t, err)
	require.NotContains(t, prompt, "Available skills", "no skills, no list")
	require.Contains(t, prompt, "propose_skill")

	addSkill(t, f, skills, "How we release", "active", nil)
	addSkill(t, f, skills, "Draft idea", "draft", nil)
	disabled := addSkill(t, f, skills, "Old process", "active", nil)
	setRole(t, f, "admin")
	_, err = skills.SetEnabled(f.ctx, f.ws, disabled.ID, f.user, false)
	require.NoError(t, err)
	setRole(t, f, "member")
	private := uuid.New()
	_, err = f.db.Exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Secret','SEC',true)`, private, f.ws)
	require.NoError(t, err)
	addSkill(t, f, skills, "Secret release", "active", &private)

	prompt, err = f.service.skillsPrompt(f.ctx, a)
	require.NoError(t, err)
	require.Contains(t, prompt, "- How we release: Use when how we release")
	require.NotContains(t, prompt, "1. Tag the release", "bodies are loaded on demand only")
	for _, hidden := range []string{"Draft idea", "Old process", "Secret release"} {
		require.NotContains(t, prompt, hidden)
	}

	disableAssistantTool(t, f, "load_skill")
	prompt, err = f.service.skillsPrompt(f.ctx, a)
	require.NoError(t, err)
	require.NotContains(t, prompt, "How we release", "without load_skill the list is not offered")
	disableAssistantTool(t, f, "propose_skill")
	prompt, err = f.service.skillsPrompt(f.ctx, a)
	require.NoError(t, err)
	require.Empty(t, prompt)
}

// load_skill hands the fenced body to the model as workspace guidance and
// stores only a summary on the tool call.
func TestAssistantLoadsSkillBodyAsWorkspaceGuidance(t *testing.T) {
	f := newToolFixture(t)
	skills := withSkills(t, f)
	addSkill(t, f, skills, "How we release", "active", nil)
	a := f.attempt(t, "Release it")
	provider, requests := scriptedProvider(t, toolCallStream("load_skill", `{"name":"how WE release"}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Release"}}, func(string) error { return nil }))

	offered := offeredToolNames((*requests)[0])
	require.Contains(t, offered, "load_skill")
	require.Contains(t, offered, "propose_skill")
	history := (*requests)[1]["messages"].([]any)
	content := history[len(history)-1].(map[string]any)["content"].(string)
	require.NotContains(t, content, "untrusted_data")
	var message struct {
		Skill struct {
			Name, Source, Guidance string
			Version                int
		} `json:"workspace_skill"`
	}
	require.NoError(t, json.Unmarshal([]byte(content), &message))
	require.Equal(t, "How we release", message.Skill.Name)
	require.Equal(t, 1, message.Skill.Version)
	require.Contains(t, message.Skill.Source, "Workspace-authored guidance")
	require.Equal(t, "```markdown\n## How we release\n1. Tag the release\n2. Announce it\n```", message.Skill.Guidance)

	status, _, output := toolCallRow(t, f, a.Message.ID)
	require.Equal(t, "completed", status)
	require.Equal(t, "How we release", output["name"])
	require.NotContains(t, output, "body", "the stored result is a summary")
}

// Missing, draft and invisible skills are not loaded.
func TestLoadSkillRefusesSkillsTheAssistantCannotList(t *testing.T) {
	f := newToolFixture(t)
	skills := withSkills(t, f)
	addSkill(t, f, skills, "Draft idea", "draft", nil)
	a := f.attempt(t, "Use the draft")
	output, _, err := f.service.tools.runSkill(f.ctx, a, "load_skill", `{"name":"Draft idea"}`)
	require.Nil(t, output)
	require.ErrorContains(t, err, "no active skill named")
	disableAssistantTool(t, f, "load_skill")
	provider, _ := scriptedProvider(t, toolCallStream("load_skill", `{"name":"Draft idea"}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Use"}}, func(string) error { return nil }))
	status, failure, _ := toolCallRow(t, f, a.Message.ID)
	require.Equal(t, "failed", status)
	require.Equal(t, "load_skill is disabled in this workspace", failure)
}

// propose_skill only drafts: a new skill is a draft, and an edit of an active
// skill is a draft version that leaves the active one in effect. The card
// carries the full text; members cannot propose.
func TestProposeSkillSavesDraftsOnly(t *testing.T) {
	f := newToolFixture(t)
	skills := withSkills(t, f)
	a := f.attempt(t, "Make a skill from what we just did")
	arguments := `{"name":"Hotfix","description":"Use when shipping a hotfix","body":"1. Branch from main\n2. Ship","team_key":"eng"}`
	output, _, err := f.service.tools.runSkill(f.ctx, a, "propose_skill", arguments)
	require.Nil(t, output)
	require.ErrorContains(t, err, "only workspace owners and admins")

	setRole(t, f, "admin")
	provider, requests := scriptedProvider(t, toolCallStream("propose_skill", arguments), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Make a skill"}}, func(string) error { return nil }))
	status, _, card := toolCallRow(t, f, a.Message.ID)
	require.Equal(t, "completed", status)
	require.Equal(t, "1. Branch from main\n2. Ship", card["body"], "the card shows the full text")
	require.Equal(t, "draft", card["status"])
	require.Equal(t, "ENG", card["team_key"])
	require.Nil(t, card["active_version"])
	history := (*requests)[1]["messages"].([]any)
	receipt := history[len(history)-1].(map[string]any)["content"].(string)
	require.Contains(t, receipt, `"status":"draft"`)
	require.NotContains(t, receipt, "Branch from main", "the model gets a short receipt")

	listed, err := skills.List(f.ctx, f.ws)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "draft", listed[0].Status)
	require.Equal(t, "assistant", listed[0].AuthorKind)
	require.Equal(t, "ENG", *listed[0].TeamKey)

	active := addSkill(t, f, skills, "How we release", "active", nil)
	setRole(t, f, "admin")
	card2, receipt2, err := f.service.tools.runSkill(f.ctx, a, "propose_skill", `{"name":"How we release","description":"Use when releasing","body":"Assistant rewrite"}`)
	require.NoError(t, err)
	require.Contains(t, receipt2, "stays in effect")
	require.Equal(t, 2, card2.(map[string]any)["version"])
	require.Equal(t, 1, *card2.(map[string]any)["active_version"].(*int))
	current, err := skills.Get(f.ctx, f.ws, active.ID)
	require.NoError(t, err)
	require.Equal(t, "active", current.Status)
	require.Equal(t, 1, current.CurrentVersion)
	require.Contains(t, current.Body, "Tag the release", "the active version is unchanged")
	require.Equal(t, 2, *current.PendingVersion)

	_, _, err = f.service.tools.runSkill(f.ctx, a, "propose_skill", `{"name":"Bad","description":"two\nlines","body":"x"}`)
	require.ErrorContains(t, err, "description must be")
	_, _, err = f.service.tools.runSkill(f.ctx, a, "propose_skill", `{"name":"Elsewhere","description":"d","body":"x","team_key":"NOPE"}`)
	require.ErrorContains(t, err, "not available")
}

// The skill tools join the capabilities list from the registry once wired.
func TestSkillToolsAppearInCapabilities(t *testing.T) {
	f := newToolFixture(t)
	f.service.tools.SetSettings(repository.NewAssistantToolSettingsRepository(f.db))
	listed, err := f.service.Capabilities(f.ctx, f.ws)
	require.NoError(t, err)
	byName := map[string]AssistantCapability{}
	for _, tool := range listed.Tools {
		byName[tool.Name] = tool
	}
	require.False(t, byName["load_skill"].Available, "unavailable until skills are wired")
	withSkills(t, f)
	listed, err = f.service.Capabilities(f.ctx, f.ws)
	require.NoError(t, err)
	for _, tool := range listed.Tools {
		byName[tool.Name] = tool
	}
	require.True(t, byName["load_skill"].Available)
	require.True(t, byName["propose_skill"].Available)
	require.True(t, byName["propose_skill"].ChangesData)
	require.False(t, byName["propose_skill"].RequiresApproval, "drafts are activated in Settings, not approved in chat")
}

func TestSkillFenceOutlastsBackticksInTheBody(t *testing.T) {
	require.Equal(t, "```markdown\nplain\n```", skillFence("plain"))
	require.Equal(t, "`````markdown\n````go\nx\n````\n`````", skillFence("````go\nx\n````"))
}
