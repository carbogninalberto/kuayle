package domain

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// AgentProfileSkill is a library skill as a coding agent receives it.
type AgentProfileSkill struct {
	Name        string `db:"name"`
	Description string `db:"description"`
	Body        string `db:"body"`
}

// AgentProfileBundle is what an agent step's profile gives its coding agent
// besides the prompt: its Markdown instructions and its skills.
type AgentProfileBundle struct {
	Name         string
	Role         string
	Instructions string
	Skills       []AgentProfileSkill
}

// AgentProfileFile is one file Kuayle writes into the agent's home before the
// harness starts. Paths are absolute and always under agentHome.
type AgentProfileFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

const agentHome = "/home/kuayle"

// Where each harness reads user-level instructions and skills. Harnesses
// missing here get the instructions in the prompt instead and no skills.
var profileFileLayout = map[string]struct{ instructions, skills string }{
	"claude-code": {".claude/CLAUDE.md", ".claude/skills"},
	"codex":       {".codex/AGENTS.md", ".codex/skills"},
	"opencode":    {".config/opencode/AGENTS.md", ".config/opencode/skill"},
}

// ProfileFilesSupported reports whether the harness reads profile files, so
// its profile instructions can leave the prompt.
func ProfileFilesSupported(harness string) bool {
	_, ok := profileFileLayout[harness]
	return ok
}

var skillSlugInvalid = regexp.MustCompile(`[^a-z0-9]+`)

// SkillSlug turns a library skill name into the SKILL.md name and directory
// harnesses expect: lowercase letters, digits and single hyphens.
func SkillSlug(name string) string {
	slug := strings.Trim(skillSlugInvalid.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(slug) > 64 {
		slug = strings.TrimRight(slug[:64], "-")
	}
	if slug == "" {
		slug = "skill"
	}
	return slug
}

// singleLineYAML quotes a front matter value as a JSON string, which YAML
// reads as a double-quoted scalar.
func singleLineYAML(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	return fmt.Sprintf("%q", value)
}

// ProfileFiles lays a profile's instructions and skills out for the harness.
// Skill names that collide after slugging get a numeric suffix.
func ProfileFiles(harness string, bundle AgentProfileBundle) []AgentProfileFile {
	layout, ok := profileFileLayout[harness]
	if !ok {
		return nil
	}
	var files []AgentProfileFile
	if instructions := strings.TrimSpace(bundle.Instructions); instructions != "" {
		header := fmt.Sprintf("# Agent profile: %s (%s)\n\nYou run as this Kuayle agent profile. Follow these instructions for the whole task.\n\n", bundle.Name, strings.ReplaceAll(bundle.Role, "_", " "))
		files = append(files, AgentProfileFile{Path: path.Join(agentHome, layout.instructions), Content: header + instructions + "\n"})
	}
	used := map[string]bool{}
	for _, skill := range bundle.Skills {
		base := SkillSlug(skill.Name)
		slug := base
		for n := 2; used[slug]; n++ {
			slug = fmt.Sprintf("%s-%d", strings.TrimRight(base[:min(len(base), 60)], "-"), n)
		}
		used[slug] = true
		content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", slug, singleLineYAML(skill.Description), strings.TrimSpace(skill.Body))
		files = append(files, AgentProfileFile{Path: path.Join(agentHome, layout.skills, slug, "SKILL.md"), Content: content})
	}
	return files
}

// ValidProfileFilePath reports whether a path is one ProfileFiles produces: an
// absolute, clean path inside a harness's instructions file or skills folder.
func ValidProfileFilePath(p string) bool {
	if p != path.Clean(p) || strings.ContainsAny(p, "\x00\n\r") {
		return false
	}
	for _, layout := range profileFileLayout {
		if p == path.Join(agentHome, layout.instructions) {
			return true
		}
		dir := path.Join(agentHome, layout.skills) + "/"
		if rest, ok := strings.CutPrefix(p, dir); ok {
			slug, file, found := strings.Cut(rest, "/")
			return found && file == "SKILL.md" && slug == SkillSlug(slug)
		}
	}
	return false
}
