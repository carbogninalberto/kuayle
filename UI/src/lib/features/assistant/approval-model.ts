import type { ApprovalModelChoice } from '$lib/api/conversations';
import type { TaskOptions } from '$lib/api/agent-tasks';
import { preferredServer, serversFor } from '../agents/model-choice.ts';

export interface ModelSelection {
	harness: string;
	model: string;
}

// The selection an approval card opens with: what the assistant proposed, else
// the configured default harness and the model its default server starts with.
export function initialSelection(
	options: Pick<TaskOptions, 'providers' | 'model_servers' | 'default_provider'>,
	proposed: { harness?: string; model?: string }
): ModelSelection {
	const harness = proposed.harness || options.default_provider || options.providers[0]?.id || '';
	const model = proposed.model || preferredServer(options.model_servers, harness)?.default_model || '';
	return { harness, model };
}

// Switching harness drops the old model, which belongs to another harness.
export function selectHarness(options: Pick<TaskOptions, 'model_servers'>, harness: string): ModelSelection {
	return { harness, model: preferredServer(options.model_servers, harness)?.default_model ?? '' };
}

// The models the servers that speak the harness offer; none means the agent's
// own sign-in decides.
export function modelsFor(servers: TaskOptions['model_servers'], harness: string): string[] {
	const models: string[] = [];
	for (const server of serversFor(servers, harness)) {
		for (const model of server.models.length ? server.models : [server.default_model]) {
			if (model && !models.includes(model)) models.push(model);
		}
	}
	return models;
}

// Only what the user changed is sent, so an untouched card keeps the backend's
// fallback order (workspace, project, team default).
export function choiceFrom(initial: ModelSelection, current: ModelSelection): ApprovalModelChoice {
	const choice: ApprovalModelChoice = {};
	if (current.harness !== initial.harness) choice.harness = current.harness;
	if (current.model !== initial.model || choice.harness) {
		if (current.model) choice.model = current.model;
	}
	return choice;
}
