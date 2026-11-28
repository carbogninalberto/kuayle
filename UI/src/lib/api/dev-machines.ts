import { api } from './client';
import type { AgentQueueItem } from './agent-tasks';
import type { PaginatedResponse } from '$lib/types/common';
import type {
	AgentProvider,
	AppPort,
	AppPortList,
	BulkPermanentDeleteDevMachinesInput,
	BulkPermanentDeleteDevMachinesResult,
	CreateDevMachineInput,
	CreateTerminalSessionInput,
	DevMachine,
	DevMachineTerminalSession,
	DevMachineEvent,
	DevMachineLogChunk,
	DevMachineService,
	DevMachinePolicy,
	DevMachineCheckout,
	DevMachineScopeSetting,
	DevMachineRepositoryDefaults,
	EnvironmentStatus,
	LaunchMachinePortResponse,
	MigrationAction,
	MigrationActionInput,
	MigrationActionList,
	ResourceBucket,
	TerminalSessionLaunchResponse,
	ModelServer,
	ModelServerInput,
	AgentModelCatalog,
	AgentModelDefault,
	AgentModelDefaultInput
} from '$lib/types/dev-machine';

const base = (slug: string) => `/api/workspaces/${slug}`;

export function listDevMachines(
	slug: string,
	issueId?: string,
	page = 1,
	perPage = 50
): Promise<PaginatedResponse<DevMachine>> {
	const query = new URLSearchParams({ page: String(page), per_page: String(perPage) });
	if (issueId) query.set('issue_id', issueId);
	return api.get(`${base(slug)}/dev-machines?${query}`);
}

export function createDevMachine(slug: string, input: CreateDevMachineInput): Promise<DevMachine> {
	return api.post(`${base(slug)}/dev-machines`, input);
}

export function getMachineNameSuggestion(slug: string): Promise<{ name: string; available: boolean }> {
	return api.get(`${base(slug)}/dev-machine-names/suggestion`);
}

export function checkMachineName(slug: string, name: string): Promise<{ name: string; available: boolean }> {
	return api.get(`${base(slug)}/dev-machine-names/availability?name=${encodeURIComponent(name)}`);
}

export function getDevMachine(slug: string, machineId: string): Promise<DevMachine> {
	return api.get(`${base(slug)}/dev-machines/${machineId}`);
}

export function updateDevMachine(
	slug: string,
	machineId: string,
	input: {
		cpu_millis?: number;
		memory_mb?: number;
		disk_gb?: number;
		/** Overrides how many active agent tasks share this machine. */
		max_tasks?: number;
		/** Restores the workspace policy's task limit. */
		clear_max_tasks?: boolean;
	}
): Promise<DevMachine> {
	return api.patch(`${base(slug)}/dev-machines/${machineId}`, input);
}

export function deleteDevMachine(slug: string, machineId: string): Promise<void> {
	return api.delete(`${base(slug)}/dev-machines/${machineId}`);
}

export function bulkPermanentDeleteDevMachines(
	slug: string,
	input: BulkPermanentDeleteDevMachinesInput
): Promise<BulkPermanentDeleteDevMachinesResult> {
	return api.post(`${base(slug)}/dev-machines/bulk/permanent-delete`, input);
}

export function startDevMachine(slug: string, machineId: string): Promise<unknown> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/start`);
}

export function stopDevMachine(slug: string, machineId: string): Promise<unknown> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/stop`);
}

export function pauseDevMachine(slug: string, machineId: string): Promise<unknown> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/pause`);
}

export function teardownDevMachine(slug: string, machineId: string): Promise<unknown> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/teardown`);
}

export function listMachineServices(slug: string, machineId: string): Promise<DevMachineService[]> {
	return api.get(`${base(slug)}/dev-machines/${machineId}/services`);
}

export function listMachineEvents(slug: string, machineId: string, afterId = 0): Promise<DevMachineEvent[]> {
	return api.get(`${base(slug)}/dev-machines/${machineId}/events?after_id=${afterId}`);
}

export function listMachineLogs(slug: string, machineId: string, afterId = 0): Promise<DevMachineLogChunk[]> {
	return api.get(`${base(slug)}/dev-machines/${machineId}/logs?after_id=${afterId}`);
}

export type ResourceUsageWindow = '1h' | '6h' | '24h';

/**
 * Every time bucket of the window, oldest first (each created_at is the start
 * of its bucket). A bucket without samples has sample_count 0 and null
 * values, so the caller can show the gap.
 */
export function listResourceHistory(slug: string, machineId: string, window: ResourceUsageWindow): Promise<ResourceBucket[]> {
	return api.get(`${base(slug)}/dev-machines/${machineId}/resource-usage?window=${window}`);
}

/** The cached environment checks of the running developer container. */
export function getEnvironmentStatus(slug: string, machineId: string): Promise<EnvironmentStatus> {
	return api.get(`${base(slug)}/dev-machines/${machineId}/environment-status`);
}

/**
 * Queues "sudo apt-get update" in the developer container (machine creator,
 * sudo machines only). The result arrives on later getEnvironmentStatus reads.
 */
export function refreshPackageIndex(slug: string, machineId: string): Promise<EnvironmentStatus> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/environment-status/package-index/refresh`);
}

export function listMachinePorts(slug: string, machineId: string): Promise<AppPortList> {
	return api.get(`${base(slug)}/dev-machines/${machineId}/ports`);
}

export function exposeMachinePort(
	slug: string,
	machineId: string,
	input: { port: number; label?: string }
): Promise<AppPort> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/ports`, input);
}

export function renameMachinePort(
	slug: string,
	machineId: string,
	portId: string,
	label: string | null
): Promise<AppPort> {
	return api.patch(`${base(slug)}/dev-machines/${machineId}/ports/${portId}`, { label });
}

export function removeMachinePort(slug: string, machineId: string, portId: string): Promise<void> {
	return api.delete(`${base(slug)}/dev-machines/${machineId}/ports/${portId}`);
}

// The machine's named migration actions; the stored connection secrets never
// travel with them.
export function listMachineMigrationActions(slug: string, machineId: string): Promise<MigrationActionList> {
	return api.get(`${base(slug)}/dev-machines/${machineId}/migration-actions`);
}

export function createMachineMigrationAction(
	slug: string,
	machineId: string,
	input: MigrationActionInput
): Promise<MigrationAction> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/migration-actions`, input);
}

export function updateMachineMigrationAction(
	slug: string,
	machineId: string,
	actionId: string,
	input: MigrationActionInput
): Promise<MigrationAction> {
	return api.put(`${base(slug)}/dev-machines/${machineId}/migration-actions/${actionId}`, input);
}

export function deleteMachineMigrationAction(slug: string, machineId: string, actionId: string): Promise<void> {
	return api.delete(`${base(slug)}/dev-machines/${machineId}/migration-actions/${actionId}`);
}

function launchMachinePort(slug: string, machineId: string, portId: string): Promise<LaunchMachinePortResponse> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/ports/${portId}/launch`);
}

// Retries while the port's host is starting (202) until it is ready or the
// deadline passes. The launch URL carries a one-time ticket for that host.
export async function launchMachinePortWhenReady(
	slug: string,
	machineId: string,
	portId: string,
	options: { timeoutMs?: number; onStarting?: () => void } = {}
): Promise<string> {
	const deadline = Date.now() + (options.timeoutMs ?? 60_000);
	let response = await launchMachinePort(slug, machineId, portId);
	while (response.status !== 'ready' && Date.now() < deadline) {
		options.onStarting?.();
		const wait = Math.min(Math.max(response.retry_after_seconds ?? 2, 1), 10) * 1000;
		await new Promise((resolve) => setTimeout(resolve, wait));
		response = await launchMachinePort(slug, machineId, portId);
	}
	if (response.status !== 'ready' || !response.launch_url) throw new Error('App port is not ready');
	return response.launch_url;
}

export async function resumePausedMachine(
	slug: string,
	machineId: string,
	options: ResumeRetryOptions = {}
): Promise<DevMachine> {
	const deadline = Date.now() + (options.timeoutMs ?? 120_000);
	let machine = await getDevMachine(slug, machineId);
	if (machine.status === 'running' && machine.desired_status === 'running') return machine;
	if (machine.status !== 'paused' || (machine.desired_status !== 'paused' && machine.desired_status !== 'running')) {
		throw new Error(`Dev Machine is ${machine.status}`);
	}
	options.onStatus?.('resuming', machine);
	if (machine.desired_status !== 'running') await startDevMachine(slug, machineId);
	machine = await waitForLaunchRetry(slug, machineId, 2, deadline, options.onStatus);
	return machine;
}

export function listMachineCheckouts(slug: string, machineId: string): Promise<DevMachineCheckout[]> {
	return api.get(`${base(slug)}/dev-machines/${machineId}/checkouts`);
}

export function checkoutIssue(slug: string, machineId: string, issueId: string): Promise<DevMachineCheckout> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/checkouts`, { issue_id: issueId });
}

async function ensureIssueCheckout(slug: string, machineId: string, issueId: string): Promise<DevMachineCheckout> {
	const existing = (await listMachineCheckouts(slug, machineId)).find((item) => item.issue_id === issueId);
	return existing ?? checkoutIssue(slug, machineId, issueId);
}

export async function ensureIssueCheckoutReady(
	slug: string,
	machineId: string,
	issueId: string,
	options: CheckoutReadyOptions = {}
): Promise<DevMachineCheckout> {
	const deadline = Date.now() + (options.timeoutMs ?? 120_000);
	let checkout = await ensureIssueCheckout(slug, machineId, issueId);
	while (checkout.status !== 'ready') {
		if (checkout.status === 'failed') throw new Error(checkout.last_error || 'Issue checkout failed');
		if (checkout.status === 'retired') throw new Error('Issue checkout is unavailable');
		options.onStatus?.(checkout);
		const delay = options.pollIntervalMs ?? 2_000;
		if (Date.now() + delay > deadline) throw new Error('Issue checkout is still preparing');
		await sleep(delay);
		const next = (await listMachineCheckouts(slug, machineId)).find(
			(item) => item.id === checkout.id || item.issue_id === issueId
		);
		if (!next) throw new Error('Issue checkout is unavailable');
		checkout = next;
	}
	return checkout;
}

/** The repository an issue or project inherits, and the repositories it may use instead. */
export function getDevMachineRepositoryDefaults(
	slug: string,
	scope: { issueId?: string; projectId?: string } = {}
): Promise<DevMachineRepositoryDefaults> {
	const query = new URLSearchParams();
	if (scope.issueId) query.set('issue_id', scope.issueId);
	if (scope.projectId) query.set('project_id', scope.projectId);
	const suffix = query.size ? `?${query}` : '';
	return api.get(`${base(slug)}/dev-machine-repository-defaults${suffix}`);
}

export function getDevMachineScopeSetting(
	slug: string,
	scopeType: 'workspace' | 'team' | 'project' | 'issue',
	scopeId?: string
): Promise<DevMachineScopeSetting> {
	const query = new URLSearchParams({ scope_type: scopeType });
	if (scopeId) query.set('scope_id', scopeId);
	return api.get(`${base(slug)}/dev-machine-scope-setting?${query}`);
}

export function updateDevMachineScopeSetting(
	slug: string,
	input: {
		scope_type: 'workspace' | 'team' | 'project' | 'issue';
		scope_id?: string;
		github_repo_id?: string;
		base_branch?: string;
		agent_provider?: string;
		agent_instructions?: string;
		compose_file?: string;
		preview_command?: string[];
		preview_port?: number;
		pull_request_template?: string;
	}
): Promise<DevMachineScopeSetting> {
	return api.put(`${base(slug)}/dev-machine-scope-setting`, input);
}

export function deleteDevMachineScopeSetting(
	slug: string,
	scopeType: 'workspace' | 'team' | 'project' | 'issue',
	scopeId?: string
): Promise<void> {
	const query = new URLSearchParams({ scope_type: scopeType });
	if (scopeId) query.set('scope_id', scopeId);
	return api.delete(`${base(slug)}/dev-machine-scope-setting?${query}`);
}

function createTerminalSession(
	slug: string,
	machineId: string,
	input: CreateTerminalSessionInput
): Promise<TerminalSessionLaunchResponse> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/terminal-sessions`, input);
}

export function createTerminalSessionWithResume(
	slug: string,
	machineId: string,
	input: CreateTerminalSessionInput,
	options: ResumeRetryOptions = {}
): Promise<TerminalSessionLaunchResponse> {
	return launchTerminalWithResume(slug, machineId, () => createTerminalSession(slug, machineId, input), options);
}

/** Repeats a terminal launch while its machine resumes or finishes starting. */
export async function launchTerminalWithResume(
	slug: string,
	machineId: string,
	launch: () => Promise<TerminalSessionLaunchResponse>,
	options: ResumeRetryOptions = {}
): Promise<TerminalSessionLaunchResponse> {
	const deadline = Date.now() + (options.timeoutMs ?? 120_000);
	let response = await launch();
	while (response.status === 'resuming' || response.status === 'pending') {
		options.onStatus?.(response.status);
		await waitForLaunchRetry(slug, machineId, response.retry_after_seconds, deadline, options.onStatus);
		response = await launch();
	}
	if (response.status !== 'ready') {
		throw new Error('Terminal session is not ready');
	}
	return response;
}

export function closeTerminalSession(
	slug: string,
	machineId: string,
	sessionId: string
): Promise<DevMachineTerminalSession> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/terminal-sessions/${sessionId}/close`);
}

export function listAgentProviders(slug: string): Promise<AgentProvider[]> {
	return api.get(`${base(slug)}/dev-machine-providers`);
}

export const agentModelDefaults = {
	list: (slug: string) => api.get<AgentModelDefault[]>(`${base(slug)}/agent-model-defaults`),
	save: (slug: string, provider: string, input: AgentModelDefaultInput) =>
		api.put<AgentModelDefault>(`${base(slug)}/agent-model-defaults/${encodeURIComponent(provider)}`, input)
};

// The models a profile or task can choose for a coding agent.
export function agentModelCatalog(slug: string, harness: string, refresh = false): Promise<AgentModelCatalog> {
	const query = new URLSearchParams({ harness, ...(refresh ? { refresh: 'true' } : {}) });
	return api.get(`${base(slug)}/agent-model-catalog?${query}`, { cache: 'no-store' });
}

export const modelServers = {
	list: (slug: string) => api.get<ModelServer[]>(`${base(slug)}/agent-model-servers`, { cache: 'no-store' }),
	create: (slug: string, input: ModelServerInput) => api.post<ModelServer>(`${base(slug)}/agent-model-servers`, input),
	update: (slug: string, id: string, input: ModelServerInput) =>
		api.put<ModelServer>(`${base(slug)}/agent-model-servers/${encodeURIComponent(id)}`, input),
	remove: (slug: string, id: string) => api.delete<void>(`${base(slug)}/agent-model-servers/${encodeURIComponent(id)}`),
	// Lists the server's models; a saved server's key is used when api_key is omitted.
	discover: (
		slug: string,
		input: { server_id?: string; openai_base_url?: string; anthropic_base_url?: string; api_key?: string }
	) => api.post<Pick<ModelServer, 'models' | 'model_names'>>(`${base(slug)}/agent-model-servers/discover`, input)
};

// The agent tasks that used this machine, active first; session-only and
// visible only to the machine's owner.
export function listMachineTasks(slug: string, machineId: string): Promise<AgentQueueItem[]> {
	return api.get(`${base(slug)}/dev-machines/${encodeURIComponent(machineId)}/tasks`);
}

export function getDevMachinePolicy(slug: string): Promise<DevMachinePolicy> {
	return api.get(`${base(slug)}/dev-machine-policy`);
}

export function updateDevMachinePolicy(
	slug: string,
	policy: Omit<DevMachinePolicy, 'workspace_id' | 'disk_quota_mode' | 'size_options'>
): Promise<DevMachinePolicy> {
	return api.patch(`${base(slug)}/dev-machine-policy`, policy);
}

interface ResumeRetryOptions {
	timeoutMs?: number;
	onStatus?: (status: 'resuming' | 'pending', machine?: DevMachine) => void;
}

interface CheckoutReadyOptions {
	timeoutMs?: number;
	pollIntervalMs?: number;
	onStatus?: (checkout: DevMachineCheckout) => void;
}

function retryDelayMs(seconds?: number): number {
	const retrySeconds = Number.isFinite(seconds) && seconds ? seconds : 2;
	return Math.min(Math.max(retrySeconds, 1), 10) * 1000;
}

async function waitForLaunchRetry(
	slug: string,
	machineId: string,
	retryAfterSeconds: number | undefined,
	deadline: number,
	onStatus?: ResumeRetryOptions['onStatus']
): Promise<DevMachine> {
	let delay = retryDelayMs(retryAfterSeconds);
	while (Date.now() + delay <= deadline) {
		await sleep(delay);
		const machine = await getDevMachine(slug, machineId);
		if (machine.status === 'running' && machine.desired_status === 'running') return machine;
		if (machine.status === 'paused' || machine.desired_status === 'running') {
			onStatus?.(machine.status === 'paused' ? 'resuming' : 'pending', machine);
			delay = 2000;
			continue;
		}
		if (
			machine.status === 'destroyed' ||
			machine.status === 'expired' ||
			machine.status === 'stopped' ||
			machine.status === 'failed'
		) {
			throw new Error(`Dev Machine is ${machine.status}`);
		}
		onStatus?.('pending', machine);
		delay = 2000;
	}
	throw new Error('Timed out waiting for Dev Machine to resume');
}

function sleep(ms: number) {
	return new Promise((resolve) => setTimeout(resolve, ms));
}

export type MergeMethod = 'merge' | 'squash' | 'rebase';

/** The merge method chosen in the development settings, by repository full name. */
export function getMergeMethods(slug: string): Promise<Record<string, MergeMethod>> {
	return api.get(`${base(slug)}/dev-machine-merge-methods`);
}

/** Sets a repository's merge method; an empty method restores the default. */
export function setMergeMethod(slug: string, repository: string, method: MergeMethod | ''): Promise<unknown> {
	return api.put(`${base(slug)}/dev-machine-merge-methods`, { repository, merge_method: method });
}
