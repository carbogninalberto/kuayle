package service

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// The lead of a pipeline task in its tick (KUA-301, KUA-302). With a lead
// the tick does not derive the phase: the task is in the phase the lead
// last moved it to, Kuayle runs the platform actions that phase calls for,
// and the required checks of that phase and every one before it still guard
// the merge, whatever the lead moved. What the lead needs to decide reaches
// it as posts addressed to it (tellLead): the checks' states when they
// change, a digest of each member's turn that ended, the budget passing a
// mark, and what the phase waits on when nobody works (the tick's stall).
// Which of them wake it is domain.LeadWakes; a pass starts at most one
// turn of the lead, which reads everything that came since its last one.

// tickLead is what Kuayle last told the lead: the run of each role's turn
// it digested, and the budget mark it passed (for the budget extensions
// granted then; each extension starts the marks again).
type tickLead struct {
	Digested   map[string]string `json:"digested,omitempty"`
	Mark       int               `json:"mark,omitempty"`
	Extensions int               `json:"extensions,omitempty"`
}

// budgetMarks are the shares of the budget whose passing wakes the lead.
var budgetMarks = []int{50, 80, 100}

// tellLead posts what the lead hears from Kuayle in a pass.
func (t *tickPass) tellLead(ctx context.Context) error {
	if t.p.Lead == nil {
		return nil
	}
	if err := t.postChecks(ctx); err != nil {
		return err
	}
	if err := t.postDigests(ctx); err != nil {
		return err
	}
	return t.postBudget(ctx)
}

// factPhase is the check a lead's phase holds when every required check up
// to it holds and it is not the last: the lead owes the next move.
const factPhase = "phase"

// leadPhase is the phase the lead last moved the task to (the first one
// until it moves it), with the required checks of it and of every phase
// before it that do not hold and were not waived for the head. When they
// all hold before the last phase, the lead's next move is unmet.
func (t *tickPass) leadPhase() (int, []UnmetCheck) {
	index := max(slices.IndexFunc(t.p.Phases, func(phase domain.PipelinePhase) bool { return phase.ID == t.current }), 0)
	var checks []FactCheck
	for _, phase := range t.p.Phases[:index+1] {
		checks = append(checks, t.required(phase)...)
	}
	unmet := unmetChecks(t.facts, checks)
	if len(unmet) == 0 && index < len(t.p.Phases)-1 {
		phase, next := t.p.Phases[index], t.p.Phases[index+1]
		unmet = []UnmetCheck{{FactCheck: FactCheck{ID: factPhase + ":" + phase.ID, Fact: factPhase}, State: CheckUnmet,
			Reason: fmt.Sprintf("Every required check up to %s holds. Next step: move the task on (kuayle-say --to @all --phase %s \"why\"), or to another phase.", phase.Label, next.ID)}}
	}
	return index, unmet
}

// postChecks tells the lead the state of the pipeline's checks when it
// changes: a "checks" fact for the required ones and an "advisory" fact for
// the others, each a map from check ID to met, unmet, waiting or waived,
// with the reasons of those that do not hold. A check that cannot be read
// keeps the state last posted.
func (t *tickPass) postChecks(ctx context.Context) error {
	for _, name := range []string{domain.LeadFactChecks, domain.LeadFactAdvisory} {
		var last map[string]string
		_ = json.Unmarshal(t.st.Facts[name], &last)
		states := map[string]string{}
		var lines []string
		for _, phase := range t.p.Phases {
			for _, check := range phase.Checks {
				if check.Required != (name == domain.LeadFactChecks) {
					continue
				}
				state, reason := t.facts.judge(FactCheck{ID: check.ID, Fact: check.Fact, Role: check.Role})
				if waived, ok := t.st.Waivers[check.ID]; ok && waived == t.facts.HeadSHA && state != "" {
					state, reason = "waived", "The owner waived it for this head."
				}
				switch state {
				case CheckUnknown:
					if previous, ok := last[check.ID]; ok {
						states[check.ID] = previous
					}
					continue
				case "":
					state = "met"
				default:
					lines = append(lines, fmt.Sprintf("- %s (%s): %s. %s", check.ID, phase.Label, state, oneLine(reason, 600)))
				}
				states[check.ID] = state
			}
		}
		if len(states) == 0 {
			continue
		}
		raw, err := json.Marshal(states)
		if err != nil {
			return err
		}
		if sameJSON(t.st.Facts[name], raw) {
			continue
		}
		body := "Every " + map[bool]string{true: "required", false: "advisory"}[name == domain.LeadFactChecks] + " check holds."
		if len(lines) > 0 {
			body = strings.Join(lines, "\n")
		}
		if err = t.s.kuaylePost(ctx, t.task, domain.TaskRoomPost{Kind: domain.RoomFact, ToRoles: []string{domain.PipelineLeadRole}, Body: truncateUTF8Bytes(body, domain.RoomMaxBodyBytes),
			Data: domain.TaskRoomData{Fact: name, Value: raw}, DedupeKey: fmt.Sprintf("fact:%d:%s:%x", t.st.Pass, name, sha256.Sum256(raw))}); err != nil {
			return err
		}
		if t.st.Facts == nil {
			t.st.Facts = map[string]json.RawMessage{}
		}
		t.st.Facts[name] = raw
	}
	return nil
}

// postDigests tells the lead how each member's turn that ended went, once
// per turn: the writer's (the root task's last run, the pass runs only
// while no writer works) and every other role's but its own, with the
// verdict it gave and the first lines of its final message.
func (t *tickPass) postDigests(ctx context.Context) error {
	ended := map[string]domain.AgentTask{t.writer.Role: t.task}
	for role, task := range t.members {
		if role != domain.PipelineLeadRole && domain.MemberState(task.State) == domain.MemberIdle {
			ended[role] = task
		}
	}
	var verdicts map[string]domain.TaskVerdict
	for _, role := range slices.Sorted(maps.Keys(ended)) {
		task := ended[role]
		if task.RunID == uuid.Nil || t.st.Lead.Digested[role] == task.RunID.String() {
			continue
		}
		run, err := t.s.Repo.WorkflowRun(ctx, task)
		if err != nil {
			return err
		}
		status, final := run.Status, run.Summary
		if status == "" || task.State == "failed" || task.State == "cancelled" {
			// A member's task that ended before or without its run says why.
			status = cmp.Or(map[string]string{"failed": "failed", "cancelled": "cancelled"}[task.State], status)
			if task.ID != t.task.ID {
				final = task.Reason
			}
		}
		verdict, commit := "", ""
		if status == string(domain.DevMachineAgentRunStatusSucceeded) && task.ID != t.task.ID {
			if verdicts == nil {
				if verdicts, err = t.s.Repo.LatestVerdicts(ctx, t.task, ""); err != nil {
					return err
				}
			}
			if v, ok := verdicts[role]; ok && v.RunID != nil && *v.RunID == task.RunID {
				verdict, commit = v.Verdict, v.CommitSHA
			}
		}
		if status != "" {
			if err = t.s.kuaylePost(ctx, t.task, domain.TaskRoomPost{Kind: domain.RoomSummary, ToRoles: []string{domain.PipelineLeadRole},
				Body: domain.TurnDigest(role, status, final, verdict, commit), Data: domain.TaskRoomData{Role: role, Verdict: verdict, Commit: commit},
				DedupeKey: "digest:" + task.RunID.String()}); err != nil {
				return err
			}
		}
		if t.st.Lead.Digested == nil {
			t.st.Lead.Digested = map[string]string{}
		}
		t.st.Lead.Digested[role] = task.RunID.String()
	}
	return nil
}

// budgetUsed is the share of the budget used, in percent: the larger of the
// turns charged and the working time, each against its allowance since the
// last extension a person granted.
func (t *tickPass) budgetUsed() int {
	b, cursor := t.p.Budget, t.task.Cursor()
	used := 0
	if allowance := b.Workflow().IterationAllowance(cursor); allowance > 0 {
		used = (cursor.Iterations + t.turns) * 100 / allowance
	}
	if from := cmp.Or(cursor.BudgetFrom, cursor.StartedAt); b.MaxMinutes > 0 && !from.IsZero() {
		until := t.now
		if !cursor.HeldSince.IsZero() {
			until = cursor.HeldSince
		}
		used = max(used, int(until.Sub(from)*100/(time.Duration(b.MaxMinutes)*time.Minute)))
	}
	return used
}

// postBudget tells the lead when the budget passes a mark it was not told
// yet: 50, 80 and 100 percent, again after each extension.
func (t *tickPass) postBudget(ctx context.Context) error {
	cursor := t.task.Cursor()
	if t.st.Lead.Extensions != cursor.Extensions {
		t.st.Lead.Mark, t.st.Lead.Extensions = 0, cursor.Extensions
	}
	used := t.budgetUsed()
	mark := 0
	for _, m := range budgetMarks {
		if used >= m {
			mark = m
		}
	}
	if mark <= t.st.Lead.Mark {
		return nil
	}
	b := t.p.Budget
	minutes := 0
	if from := cmp.Or(cursor.BudgetFrom, cursor.StartedAt); !from.IsZero() {
		minutes = int(t.now.Sub(from).Minutes())
	}
	value, err := json.Marshal(map[string]int{"percent": mark, "turns": cursor.Iterations + t.turns, "max_turns": b.Workflow().IterationAllowance(cursor), "minutes": minutes, "max_minutes": b.MaxMinutes})
	if err != nil {
		return err
	}
	body := fmt.Sprintf("The task has used %d%% of its budget", mark)
	if mark == 100 {
		body = "The task has used its whole budget"
	}
	var parts []string
	if b.MaxTurns > 0 {
		parts = append(parts, fmt.Sprintf("%d of %d turns", cursor.Iterations+t.turns, b.Workflow().IterationAllowance(cursor)))
	}
	if b.MaxMinutes > 0 {
		parts = append(parts, fmt.Sprintf("%d of %d minutes of work", minutes, b.MaxMinutes))
	}
	body += ": " + strings.Join(parts, ", ") + "."
	if mark == 100 {
		body += " Kuayle starts no more turns: the owner continues or stops the task."
	}
	if err = t.s.kuaylePost(ctx, t.task, domain.TaskRoomPost{Kind: domain.RoomFact, ToRoles: []string{domain.PipelineLeadRole}, Body: body,
		Data: domain.TaskRoomData{Fact: domain.LeadFactBudget, Value: value}, DedupeKey: fmt.Sprintf("budget:%d:%d", cursor.Extensions, mark)}); err != nil {
		return err
	}
	t.st.Lead.Mark = mark
	return nil
}

// budgetWake is when the time budget next needs a pass: its end, and with a
// lead the next mark it was not told yet; nil while a person is awaited or
// there is no time budget.
func (t *tickPass) budgetWake() *time.Time {
	cursor := t.task.Cursor()
	deadline := (domain.WorkflowGraph{Budget: t.p.Budget.Workflow()}).BudgetDeadline(cursor)
	if deadline == nil || t.p.Lead == nil {
		return deadline
	}
	told := t.st.Lead.Mark
	if t.st.Lead.Extensions != cursor.Extensions {
		told = 0
	}
	from := cmp.Or(cursor.BudgetFrom, cursor.StartedAt)
	for _, mark := range budgetMarks {
		if at := from.Add(time.Duration(t.p.Budget.MaxMinutes) * time.Minute * time.Duration(mark) / 100); mark > told && at.Before(*deadline) {
			return &at
		}
	}
	return deadline
}
