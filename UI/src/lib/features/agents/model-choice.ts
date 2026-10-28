import type { ModelServer } from '$lib/types/dev-machine';

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
