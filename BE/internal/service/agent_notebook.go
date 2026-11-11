package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// Notebook returns the task's notebook to the task's creator. The notebook
// has no write API: agents update it through their outbox, Kuayle records
// what it knows, and a person's instruction arrives through the task's
// existing message paths (a live message, a retry, a follow-up).
func (s *AgentTaskService) Notebook(ctx context.Context, ws, id uuid.UUID) (*domain.TaskNotebook, error) {
	return s.Repo.Notebook(ctx, ws, id)
}

// runPromptSecrets lists the credential values to hide from an attempt's
// recorded prompt: the same values run output is redacted with.
type runPromptSecrets func(ctx context.Context, ws, machine uuid.UUID) ([]string, error)

// RunPrompt returns the prompt an attempt received, split by source, to the
// task's creator. It is debugging data: bounded when recorded, and redacted
// here with the machine's current and retained credentials.
func (s *AgentTaskService) RunPrompt(ctx context.Context, ws, id, run uuid.UUID) (*repository.AgentRunPrompt, error) {
	prompt, err := s.Repo.RunPrompt(ctx, ws, id, run)
	if err != nil {
		return nil, err
	}
	var secrets runPromptSecrets
	if s.machines != nil {
		secrets = s.machines.machineSecretValues
	}
	return redactRunPrompt(ctx, ws, prompt, secrets)
}

func redactRunPrompt(ctx context.Context, ws uuid.UUID, prompt *repository.AgentRunPrompt, secrets runPromptSecrets) (*repository.AgentRunPrompt, error) {
	if secrets == nil || len(prompt.Sources) == 0 {
		return prompt, nil
	}
	values, err := secrets(ctx, ws, prompt.MachineID)
	if err != nil {
		return nil, err
	}
	for i := range prompt.Sources {
		prompt.Sources[i].Text = redactText(prompt.Sources[i].Text, values)
	}
	return prompt, nil
}
