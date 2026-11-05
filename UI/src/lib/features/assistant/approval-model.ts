import type { ApprovalModelChoice } from '$lib/api/conversations';
import type { TaskOptions } from '$lib/api/agent-tasks';
import type { ModelServer } from '$lib/types/dev-machine';
import { serversFor } from '../agents/model-choice.ts';

// The coding agent an approval starts: its harness, the model server that runs
// the model ('' for none: the agent's own sign-in) and the model.
export interface ModelSelection {
	harness: string;
	server: string;
	model: string;
}

type Options = Pick<TaskOptions, 'providers' | 'model_servers' | 'default_provider'>;

// The models a server runs; one without a list runs its default model.
export function serverModels(server: ModelServer | undefined): string[] {
	if (!server) return [];
	return server.models.length ? server.models : [server.default_model];
}

// The server the backend runs a model on when none was picked, as
// serverForModel there: the preferred one, then the default, then the first
// other compatible server that lists it (a server with no list runs any model).
export function serverForModel(
	servers: ModelServer[] | undefined,
	harness: string,
	model: string,
	prefer?: string
): ModelServer | undefined {
	let found: ModelServer | undefined,
		rank = 0;
	for (const server of serversFor(servers, harness)) {
		if (server.models.length && !server.models.includes(model)) continue;
		const score = server.id === prefer ? 3 : server.is_default ? 2 : 1;
		if (score > rank) [found, rank] = [server, score];
	}
	return found;
}

// Without a model the backend uses the default server's model when that server
// speaks the harness, else no server at all.
function defaultFor(servers: ModelServer[] | undefined, harness: string): Omit<ModelSelection, 'harness'> {
	const server = serversFor(servers, harness).find((item) => item.is_default);
	return server ? { server: server.id, model: server.default_model } : { server: '', model: '' };
}

// The selection an approval card opens with: what the assistant proposed (on
// the server the backend resolves for it, the task's own first), else what the
// backend starts with the configured default harness. current marks a
// follow-up's existing model: an untouched card keeps the task's model and
// server, so it shows the server the task runs on (its own, else the current
// default) without resolving the model again.
export function initialSelection(
	options: Options,
	proposed: { harness?: string; model?: string; server?: string; current?: boolean }
): ModelSelection {
	const harness = proposed.harness || options.default_provider || options.providers[0]?.id || '';
	if (proposed.current) {
		const server = proposed.server || defaultFor(options.model_servers, harness).server;
		return { harness, server, model: proposed.model ?? '' };
	}
	if (!proposed.model) return { harness, ...defaultFor(options.model_servers, harness) };
	const server = serverForModel(options.model_servers, harness, proposed.model, proposed.server);
	return { harness, server: server?.id ?? '', model: proposed.model };
}

// Switching harness drops the old model, which belongs to another harness.
export function selectHarness(options: Pick<TaskOptions, 'model_servers'>, harness: string): ModelSelection {
	return { harness, ...defaultFor(options.model_servers, harness) };
}

// Switching server starts on its default model; '' is the agent's own sign-in.
export function selectServer(
	options: Pick<TaskOptions, 'model_servers'>,
	current: ModelSelection,
	server: string
): ModelSelection {
	const next = serversFor(options.model_servers, current.harness).find((item) => item.id === server);
	return { ...current, server: next?.id ?? '', model: next?.default_model ?? '' };
}

// Only what the user changed is sent, so an untouched card keeps the backend's
// fallback order (workspace, project, team default). A picked model carries its
// server, since two servers can list the same model name.
export function choiceFrom(initial: ModelSelection, current: ModelSelection): ApprovalModelChoice {
	const choice: ApprovalModelChoice = {};
	if (current.harness !== initial.harness) choice.harness = current.harness;
	if (choice.harness || current.model !== initial.model || current.server !== initial.server) {
		if (current.model) {
			choice.model = current.model;
			if (current.server) choice.model_server_id = current.server;
		}
	}
	return choice;
}
