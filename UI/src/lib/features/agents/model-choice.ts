import type { AgentModelDefaultInput, ModelServer, ThinkingLevel } from '$lib/types/dev-machine';

// Model servers a harness can use, from the protocols each server speaks.
export function serversFor(servers: ModelServer[] | undefined, provider: string): ModelServer[] {
	return (servers ?? []).filter((server) => server.harnesses.includes(provider));
}

// The server a new run starts with: the workspace default when it fits the
// harness, else the first compatible one. Undefined means the agent's own sign-in.
export function preferredServer(servers: ModelServer[] | undefined, provider: string): ModelServer | undefined {
	const compatible = serversFor(servers, provider);
	return compatible.find((server) => server.is_default) ?? compatible[0];
}

// The default model server that runs the agent when a task names no server;
// its own default model and thinking level then apply instead of the agent's
// sign-in default.
export function overridingServer(servers: ModelServer[] | undefined, provider: string): ModelServer | undefined {
	return serversFor(servers, provider).find((server) => server.is_default);
}

// The effort choice that leaves the effort to the agent itself.
export const AGENT_EFFORT = 'agent';

export function effortChoice(thinking: string): string {
	return thinking || AGENT_EFFORT;
}

export function modelDefaultInput(model: string, effort: string): AgentModelDefaultInput {
	return { model: model.trim(), thinking: effort === AGENT_EFFORT ? '' : (effort as ThinkingLevel) };
}
