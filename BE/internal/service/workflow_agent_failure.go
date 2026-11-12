package service

import (
	"context"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// AgentFailedBecause advances the task after its agent step's iteration
// failed for a reason worth telling: the step records it, and a workflow
// that ends here gives it as the task's reason instead of only "agent
// failed". The workflow still decides what the failure leads to.
func (w *WorkflowRunner) AgentFailedBecause(ctx context.Context, task domain.AgentTask, problem string) error {
	return w.agentFailedBecause(ctx, task, problem, "")
}

// AgentFailedForRun is AgentFailedBecause for a run whose class is known: a
// workflow that ends here says the class and one short line of the cause.
func (w *WorkflowRunner) AgentFailedForRun(ctx context.Context, task domain.AgentTask, problem string, class domain.FailureClass) error {
	return w.agentFailedBecause(ctx, task, problem, class)
}

func (w *WorkflowRunner) agentFailedBecause(ctx context.Context, task domain.AgentTask, problem string, class domain.FailureClass) error {
	graph, _, err := w.graph(ctx, task)
	if err != nil {
		return err
	}
	if node, ok := graph.Node(task.Cursor().Node); ok && node.Label != "" {
		problem = node.Label + ": " + problem
	}
	apply, err := w.plan(ctx, task, domain.WorkflowAgentFailed, problem, false)
	if err != nil {
		return err
	}
	if last := len(apply.Moves) - 1; apply.Entry.Kind == repository.WorkflowEnterFail && last >= 0 && !apply.Moves[last].Stop {
		// The reason names the problem once: the class's one-line cause is
		// the problem, and the step's detail keeps all of it.
		if class != "" {
			apply.FailureClass = class
			apply.Entry.Reason += ". " + domain.TaskFailureReason(class, problem)
		} else {
			apply.Entry.Reason += ". " + problem
		}
	}
	_, err = w.tasks.ApplyWorkflow(ctx, task, *apply)
	return err
}
