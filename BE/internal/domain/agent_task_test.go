package domain

import "testing"

func TestAgentTaskTransitions(t *testing.T) {
	for _, edge := range [][2]string{{"queued", "preparing"}, {"preparing", "running"}, {"running", "waiting_input"}, {"waiting_input", "running"}, {"running", "validating"}, {"validating", "running"}, {"validating", "publishing"}, {"publishing", "awaiting_review"}, {"awaiting_review", "completed"}, {"awaiting_review", "queued"}, {"blocked", "queued"}, {"failed", "queued"}, {"running", "cancelling"}, {"cancelling", "cancelled"}, {"cancelling", "failed"}} {
		if !ValidAgentTaskTransition(edge[0], edge[1]) {
			t.Errorf("expected transition %s -> %s", edge[0], edge[1])
		}
	}
	for _, edge := range [][2]string{{"queued", "completed"}, {"running", "completed"}, {"validating", "completed"}, {"running", "cancelled"}, {"running", "failed"}, {"completed", "running"}, {"cancelled", "running"}, {"cancelling", "running"}, {"unknown", "queued"}, {"running", "running"}} {
		if ValidAgentTaskTransition(edge[0], edge[1]) {
			t.Errorf("unexpected transition %s -> %s", edge[0], edge[1])
		}
	}
}
