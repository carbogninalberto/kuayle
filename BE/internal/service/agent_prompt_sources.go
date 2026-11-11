package service

import (
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// promptAssembly follows an attempt's prompt while Kuayle assembles it and
// remembers where each part came from (domain.PromptSource). Its parts, in
// order, are always exactly the prompt: every assembly step is recorded as
// the difference between the prompt before and after it, so a step that
// prepends, appends or shortens is attributed without knowing how it works.
type promptAssembly struct {
	// parts are the prompt, in order; files what reached the agent in its
	// instructions file.
	parts []domain.PromptSource
	files []domain.PromptSource
}

// newPromptAssembly starts from the task's stored prompt: the original
// request, then the section a workflow step or a follow-up added, with the
// plan a planning step handed over (if the section carries it) on its own.
func newPromptAssembly(prompt, plan string) *promptAssembly {
	a := &promptAssembly{}
	add := func(source, text string) {
		if text != "" {
			a.parts = append(a.parts, domain.PromptSource{Source: source, Channel: domain.PromptChannelPrompt, Text: text})
		}
	}
	base, section, found := strings.Cut(prompt, followUpMarker)
	add(domain.PromptSourceTask, base)
	if !found {
		return a
	}
	section = followUpMarker + section
	source := domain.PromptSourceFollowUp
	if strings.HasPrefix(section, workflowStepMarker) {
		source = domain.PromptSourceWorkflowStep
	}
	carried := ""
	if plan = neutralisePlan(plan); plan != "" {
		carried = domain.WorkflowPlanHeading + "\n" + plan
	}
	if at := strings.Index(section, carried); carried != "" && at >= 0 {
		add(source, section[:at])
		add(domain.PromptSourcePlan, carried)
		add(source, section[at+len(carried):])
		return a
	}
	add(source, section)
	return a
}

func (a *promptAssembly) text() string {
	var b strings.Builder
	for _, part := range a.parts {
		b.WriteString(part.Text)
	}
	return b.String()
}

// apply records that an assembly step turned the prompt into next and
// returns next. What the step kept at the start and the end of the prompt
// stays with its sources; what it put in between is the step's.
func (a *promptAssembly) apply(source, next string) string {
	current := a.text()
	if next == current {
		return next
	}
	part := func(text string) []domain.PromptSource {
		if text == "" {
			return nil
		}
		return []domain.PromptSource{{Source: source, Channel: domain.PromptChannelPrompt, Text: text}}
	}
	// A step that only adds (ahead of the prompt, after it, or both) keeps
	// every existing part whole.
	if at := strings.Index(next, current); at >= 0 && current != "" {
		parts := append(part(next[:at]), a.parts...)
		a.parts = mergePromptParts(append(parts, part(next[at+len(current):])...))
		return next
	}
	prefix := 0
	for prefix < len(current) && prefix < len(next) && current[prefix] == next[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(current)-prefix && suffix < len(next)-prefix && current[len(current)-1-suffix] == next[len(next)-1-suffix] {
		suffix++
	}
	var parts []domain.PromptSource
	keep := func(from, to int) {
		at := 0
		for _, part := range a.parts {
			start, end := max(from, at), min(to, at+len(part.Text))
			if start < end {
				kept := part
				kept.Text = part.Text[start-at : end-at]
				parts = append(parts, kept)
			}
			at += len(part.Text)
		}
	}
	keep(0, prefix)
	if added := next[prefix : len(next)-suffix]; added != "" {
		parts = append(parts, domain.PromptSource{Source: source, Channel: domain.PromptChannelPrompt, Text: added})
	}
	keep(len(current)-suffix, len(current))
	a.parts = mergePromptParts(parts)
	return next
}

// mergePromptParts joins neighbours of the same source.
func mergePromptParts(parts []domain.PromptSource) []domain.PromptSource {
	merged := parts[:0]
	for _, part := range parts {
		if last := len(merged) - 1; last >= 0 && merged[last].Source == part.Source && merged[last].Channel == part.Channel {
			merged[last].Text += part.Text
			continue
		}
		merged = append(merged, part)
	}
	return merged
}

// file records a part that reached the agent in its instructions file.
func (a *promptAssembly) file(source, text string) {
	if strings.TrimSpace(text) != "" {
		a.files = append(a.files, domain.PromptSource{Source: source, Channel: domain.PromptChannelInstructions, Text: text})
	}
}

// sources are the recorded parts: the prompt's first, in order.
func (a *promptAssembly) sources() []domain.PromptSource {
	return append(append([]domain.PromptSource{}, a.parts...), a.files...)
}

// Titles workflowInstructions gives the profile's and the step's sections of
// a workflow step's instructions.
const (
	workflowProfileTitle = "Agent profile "
	workflowStepTitle    = "Workflow step "
)

// applyInstructions records the configured instructions withInstructions put
// ahead of the task. A workflow step's instructions hold up to three parts
// (the project's, its profile's and the step's own, in that order), each
// attributed to its source.
func (a *promptAssembly) applyInstructions(next string) string {
	current := a.text()
	a.apply(domain.PromptSourceProject, next)
	added, ok := strings.CutSuffix(next, current)
	if !ok || !strings.HasPrefix(added, projectInstructionsHeader) || !strings.HasSuffix(added, projectInstructionsSeparator) || len(a.parts) == 0 || a.parts[0].Text != added {
		return next
	}
	body := added[len(projectInstructionsHeader) : len(added)-len(projectInstructionsSeparator)]
	// Where a titled section starts in body, or -1.
	find := func(title string, before int) int {
		if at := strings.LastIndex(body[:before], "\n\n"+title); at >= 0 {
			return at + 2
		}
		if strings.HasPrefix(body, title) && before > 0 {
			return 0
		}
		return -1
	}
	end := len(body)
	step := find(workflowStepTitle, end)
	if step >= 0 {
		end = step
	}
	profile := find(workflowProfileTitle, end)
	part := func(source, text string) domain.PromptSource {
		return domain.PromptSource{Source: source, Channel: domain.PromptChannelPrompt, Text: text}
	}
	projectEnd := len(body)
	if profile >= 0 {
		projectEnd = profile
	} else if step >= 0 {
		projectEnd = step
	}
	split := []domain.PromptSource{part(domain.PromptSourceProject, projectInstructionsHeader+body[:projectEnd])}
	if profile >= 0 {
		split = append(split, part(domain.PromptSourceProfile, body[profile:end]))
	}
	if step >= 0 {
		split = append(split, part(domain.PromptSourceWorkflowStep, body[step:]))
	}
	split = append(split, part(domain.PromptSourceProject, projectInstructionsSeparator))
	a.parts = append(split, a.parts[1:]...)
	return next
}
