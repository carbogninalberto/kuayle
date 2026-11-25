package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// The lead of a pipeline task in its tick (KUA-301). With a lead the tick
// does not derive the phase: the task is in the phase the lead last moved
// it to, Kuayle runs the platform actions that phase calls for, and the
// required checks of that phase and every one before it still guard the
// merge, whatever the lead moved. What the lead needs to decide reaches it
// as facts: the checks' states when they change, and what the phase waits
// on when nobody works (the tick's stall, sent to the lead).

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
	if t.p.Lead == nil {
		return nil
	}
	for _, name := range []string{"checks", "advisory"} {
		var last map[string]string
		_ = json.Unmarshal(t.st.Facts[name], &last)
		states := map[string]string{}
		var lines []string
		for _, phase := range t.p.Phases {
			for _, check := range phase.Checks {
				if check.Required != (name == "checks") {
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
		body := "Every " + map[bool]string{true: "required", false: "advisory"}[name == "checks"] + " check holds."
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
