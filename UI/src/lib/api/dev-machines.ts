import { api } from './client';
import type { PaginatedResponse } from '$lib/types/common';
import type {
	AgentProvider,
	AgentRun,
	AgentRunTrace,
	BulkPermanentDeleteDevMachinesInput,
	CreateAgentRunInput,
	CreateDevMachineInput,
	CreateTerminalSessionInput,
	DevMachine,
	DevMachineTerminalSession,
	DevMachineEvent,
	DevMachineLogChunk,
	DevMachineService,
	DevMachinePolicy,
	DevMachineCheckout,
	DevMachineEnvironment,
	DevMachineScopeSetting,
	LaunchMachineServiceResponse,
	ResourceSample,
	TerminalSessionLaunchResponse
} from '$lib/types/dev-machine';

const base = (slug: string) => `/api/workspaces/${slug}`;

export function listDevMachines(slug: string, issueId?: string, page = 1, perPage = 50): Promise<PaginatedResponse<DevMachine>> {
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

export function updateDevMachine(slug: string, machineId: string, input: { keep_running?: boolean }): Promise<DevMachine> {
	return api.patch(`${base(slug)}/dev-machines/${machineId}`, input);
}

export function deleteDevMachine(slug: string, machineId: string): Promise<void> {
	return api.delete(`${base(slug)}/dev-machines/${machineId}`);
}

export function bulkDeleteDevMachines(slug: string, input: { machine_ids?: string[]; old_only?: boolean }): Promise<{ count: number }> {
	return api.deleteWithBody(`${base(slug)}/dev-machines/bulk`, input);
}

export function permanentDeleteDevMachine(slug: string, machineId: string): Promise<void> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/permanent-delete`);
}

export function bulkPermanentDeleteDevMachines(slug: string, input: BulkPermanentDeleteDevMachinesInput): Promise<{ count: number }> {
	return api.post(`${base(slug)}/dev-machines/bulk/permanent-delete`, input);
}

export function touchDevMachineActivity(slug: string, machineId: string): Promise<void> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/activity`);
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

export function listResourceUsage(slug: string, machineId: string): Promise<ResourceSample[]> {
	return api.get(`${base(slug)}/dev-machines/${machineId}/resource-usage`);
}

export function launchMachineService(slug: string, machineId: string, service: string, checkoutId?: string): Promise<LaunchMachineServiceResponse> {
	const query = checkoutId ? `?checkout_id=${encodeURIComponent(checkoutId)}` : '';
	return api.post(`${base(slug)}/dev-machines/${machineId}/services/${service}/launch${query}`);
}

export async function launchMachineServiceWithResume(
	slug: string,
	machineId: string,
	service: string,
	checkoutId?: string,
	options: ResumeRetryOptions = {}
): Promise<LaunchMachineServiceResponse> {
	const deadline = Date.now() + (options.timeoutMs ?? 120_000);
	let response = await launchMachineService(slug, machineId, service, checkoutId);
	while (response.status === 'resuming' || response.status === 'pending') {
		options.onStatus?.(response.status);
		await waitForLaunchRetry(slug, machineId, response.retry_after_seconds, deadline, options.onStatus);
		response = await launchMachineService(slug, machineId, service, checkoutId);
	}
	if (response.status !== 'ready' || !response.launch_url) {
		throw new Error('Dev Machine service is not ready');
	}
	if (service === 'ide' && !checkoutId && options.defaultFolder !== false) {
		response = { ...response, launch_url: appendQueryParam(response.launch_url, 'folder', options.defaultFolderPath ?? '/workspace/tasks') };
	}
	return response;
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
	if (machine.status !== 'running' || machine.desired_status !== 'running') throw new Error('Dev Machine is not running');
	return machine;
}

export function listMachineCheckouts(slug: string, machineId: string): Promise<DevMachineCheckout[]> {
	return api.get(`${base(slug)}/dev-machines/${machineId}/checkouts`);
}

export function checkoutIssue(slug: string, machineId: string, issueId: string): Promise<DevMachineCheckout> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/checkouts`, { issue_id: issueId });
}

export async function ensureIssueCheckout(slug: string, machineId: string, issueId: string): Promise<DevMachineCheckout> {
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
		options.onStatus?.(checkout);
		const delay = options.pollIntervalMs ?? 2_000;
		if (Date.now() + delay > deadline) throw new Error('Issue checkout is still preparing');
		await sleep(delay);
		const next = (await listMachineCheckouts(slug, machineId)).find((item) => item.id === checkout.id || item.issue_id === issueId);
		if (!next) throw new Error('Issue checkout is unavailable');
		checkout = next;
	}
	return checkout;
}

export function getDevMachineScopeSetting(slug: string, scopeType: 'workspace' | 'team' | 'project' | 'issue', scopeId?: string): Promise<DevMachineScopeSetting> {
	const query = new URLSearchParams({ scope_type: scopeType });
	if (scopeId) query.set('scope_id', scopeId);
	return api.get(`${base(slug)}/dev-machine-scope-setting?${query}`);
}

export function listDevMachineScopeSettings(slug: string): Promise<DevMachineScopeSetting[]> {
	return api.get(`${base(slug)}/dev-machine-scope-settings`);
}

export function updateDevMachineScopeSetting(slug: string, input: { scope_type: 'workspace' | 'team' | 'project' | 'issue'; scope_id?: string; github_repo_id?: string; base_branch?: string; environment_id?: string }): Promise<DevMachineScopeSetting> {
	return api.put(`${base(slug)}/dev-machine-scope-setting`, input);
}

export function deleteDevMachineScopeSetting(slug: string, scopeType: 'workspace' | 'team' | 'project' | 'issue', scopeId?: string): Promise<void> {
	const query = new URLSearchParams({ scope_type: scopeType });
	if (scopeId) query.set('scope_id', scopeId);
	return api.delete(`${base(slug)}/dev-machine-scope-setting?${query}`);
}

export function listDevMachineEnvironments(slug: string): Promise<DevMachineEnvironment[]> {
	return api.get(`${base(slug)}/dev-machine-environments`);
}

export function snapshotDevMachineEnvironment(slug: string, input: { name: string; source_machine_id: string }): Promise<DevMachineEnvironment> {
	return api.post(`${base(slug)}/dev-machine-environments`, input);
}

export function deleteDevMachineEnvironment(slug: string, environmentId: string): Promise<void> {
	return api.delete(`${base(slug)}/dev-machine-environments/${environmentId}`);
}

export function listTerminalSessions(slug: string, machineId: string): Promise<DevMachineTerminalSession[]> {
	return api.get(`${base(slug)}/dev-machines/${machineId}/terminal-sessions`);
}

export function createTerminalSession(slug: string, machineId: string, input: CreateTerminalSessionInput): Promise<TerminalSessionLaunchResponse> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/terminal-sessions`, input);
}

export async function createTerminalSessionWithResume(
	slug: string,
	machineId: string,
	input: CreateTerminalSessionInput,
	options: ResumeRetryOptions = {}
): Promise<TerminalSessionLaunchResponse> {
	const deadline = Date.now() + (options.timeoutMs ?? 120_000);
	let response = await createTerminalSession(slug, machineId, input);
	while (response.status === 'resuming' || response.status === 'pending') {
		options.onStatus?.(response.status);
		await waitForLaunchRetry(slug, machineId, response.retry_after_seconds, deadline, options.onStatus);
		response = await createTerminalSession(slug, machineId, input);
	}
	if (response.status !== 'ready') {
		throw new Error('Terminal session is not ready');
	}
	return response;
}

export function closeTerminalSession(slug: string, machineId: string, sessionId: string): Promise<DevMachineTerminalSession> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/terminal-sessions/${sessionId}/close`);
}

export function listAgentProviders(slug: string): Promise<AgentProvider[]> {
	return api.get(`${base(slug)}/dev-machine-providers`);
}

export function listMachineAgentProviders(slug: string, machineId: string): Promise<AgentProvider[]> {
	return api.get(`${base(slug)}/dev-machines/${machineId}/providers`);
}

export function createAgentRun(slug: string, machineId: string, input: CreateAgentRunInput): Promise<AgentRun> {
	return api.post(`${base(slug)}/dev-machines/${machineId}/agent-runs`, input);
}

export function listMachineAgentRuns(slug: string, machineId: string, page = 1, perPage = 50): Promise<PaginatedResponse<AgentRun>> {
	return api.get(`${base(slug)}/dev-machines/${machineId}/agent-runs?page=${page}&per_page=${perPage}`);
}

export function getAgentRun(slug: string, runId: string): Promise<AgentRun> {
	return api.get(`${base(slug)}/agent-runs/${runId}`);
}

export function cancelAgentRun(slug: string, runId: string): Promise<void> {
	return api.post(`${base(slug)}/agent-runs/${runId}/cancel`);
}

export function getAgentRunTrace(slug: string, runId: string, eventsAfterId = 0, eventsLimit = 200, logsAfterId = 0, logsLimit = 500): Promise<AgentRunTrace> {
	const query = new URLSearchParams({
		events_after_id: String(eventsAfterId),
		events_limit: String(eventsLimit),
		logs_after_id: String(logsAfterId),
		logs_limit: String(logsLimit)
	});
	return api.get(`${base(slug)}/agent-runs/${runId}/trace?${query}`);
}

export function getDevMachinePolicy(slug: string): Promise<DevMachinePolicy> {
	return api.get(`${base(slug)}/dev-machine-policy`);
}

export function updateDevMachinePolicy(slug: string, policy: Omit<DevMachinePolicy, 'workspace_id'>): Promise<DevMachinePolicy> {
	return api.patch(`${base(slug)}/dev-machine-policy`, policy);
}

export interface ResumeRetryOptions {
	timeoutMs?: number;
	defaultFolder?: boolean;
	defaultFolderPath?: string;
	onStatus?: (status: 'resuming' | 'pending', machine?: DevMachine) => void;
}

export interface CheckoutReadyOptions {
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
		if (machine.status === 'destroyed' || machine.status === 'expired' || machine.status === 'stopped' || machine.status === 'failed') {
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

function appendQueryParam(url: string, key: string, value: string): string {
	try {
		const parsed = new URL(url);
		if (!parsed.searchParams.has(key)) parsed.searchParams.set(key, value);
		return parsed.toString();
	} catch {
		const separator = url.includes('?') ? '&' : '?';
		return `${url}${separator}${encodeURIComponent(key)}=${encodeURIComponent(value)}`;
	}
}
