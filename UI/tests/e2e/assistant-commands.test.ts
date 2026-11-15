import { test, expect, type Page } from '@playwright/test';

// Approval cards of the command tools (KUA-153): starting a temporary machine
// and running a command, then the command's result on the approved card.
const at = '2026-11-15T09:30:00Z';
const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'member' };
const conversation = { id: 'chat-1', title: 'Compute', workspace_id: 'workspace-1', creator_id: 'user-1', created_at: at, updated_at: at };
const userMessage = { id: 'message-1', conversation_id: 'chat-1', sequence: 1, role: 'user', content: 'compute 2**100 in python', status: 'completed', provider: '', model: '', created_at: at };
const answer = { ...userMessage, id: 'message-2', sequence: 2, role: 'assistant', content: 'Approval is pending.', provider: 'openai_compatible', model: 'm' };
const machine = { machine_id: 'machine-1', machine: 'tmp-1a2b3c4d', cpu_millis: 1000, memory_mb: 2048, disk_gb: 20, temporary: true, reused: false, expires_at: '2026-11-15T10:00:00Z' };

const call = (name: string, input: Record<string, unknown>) => ({
	id: 'tool-call-1',
	conversation_id: 'chat-1',
	message_id: answer.id,
	sequence: 0,
	call_id: 'call_1',
	name,
	input,
	output: { approval_id: 'approval-1', status: 'pending_user_approval' },
	status: 'completed',
	created_at: at
});

const approval = (action: string, payload: Record<string, unknown>) => ({
	id: 'approval-1',
	conversation_id: 'chat-1',
	tool_call_id: 'tool-call-1',
	action,
	actor_id: 'user-1',
	issue_id: '00000000-0000-0000-0000-000000000000',
	payload,
	status: 'pending',
	expires_at: '2026-11-16T09:30:00Z',
	created_at: at,
	updated_at: at
});

async function chat(page: Page, tools: unknown[], approvals: any[]) {
	const state = { approvals, decisions: [] as string[], run: null as Record<string, unknown> | null, runReads: 0 };
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.routeWebSocket('**/conversations/chat-1/stream*', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me') return route.fulfill({ json: { id: 'user-1', name: 'Ada', display_name: 'Ada', email: 'ada@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path.endsWith('/assistant/status')) return route.fulfill({ json: { configured: true, provider: 'openai_compatible', model: 'm' } });
		if (path.endsWith('/conversations')) return route.fulfill({ json: [conversation] });
		if (path.endsWith('/approvals/approval-1/command-run')) {
			state.runReads++;
			return state.run ? route.fulfill({ json: state.run }) : route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
		}
		if (path.endsWith('/approvals/approval-1')) {
			const decision = request.postDataJSON().decision;
			state.decisions.push(decision);
			const resolved = { ...state.approvals[0], status: decision === 'approve' ? 'approved' : 'denied', resolved_at: at, updated_at: '2026-11-15T09:31:00Z' };
			state.approvals = [resolved];
			return route.fulfill({ json: resolved });
		}
		if (path.endsWith('/events')) return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/conversations/chat-1')
			return route.fulfill({
				json: { conversation, messages: [userMessage, answer], tools, approvals: state.approvals, cursor: 0, has_more: false }
			});
		return route.fulfill({ json: [] });
	});
	return state;
}

const noHorizontalScroll = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);

test('starting a temporary machine shows its size, lifetime, credentials and quota', async ({ page }) => {
	const state = await chat(page, [call('start_temporary_machine', {})], [
		approval('start_temporary_machine', {
			cpu_millis: 1000,
			memory_mb: 2048,
			disk_gb: 20,
			profile: 'headless',
			ttl_minutes: 30,
			quota_used: 1,
			quota_limit: 2,
			working_directory: '/workspace/kuayle-assistant/chat-1',
			credentials: false
		})
	]);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByText('Start a temporary machine?', { exact: true })).toBeVisible();
	await expect(
		page.getByText('A 1 vCPU, 2 GB memory, 20 GB disk machine for this conversation, permanently deleted 30 minutes after it starts.')
	).toBeVisible();
	await expect(page.getByText('No repository and no credentials; internet only through the workspace proxy.')).toBeVisible();
	await expect(page.getByText('1 of 2 temporary machines are in use in this workspace.')).toBeVisible();
	expect(await noHorizontalScroll(page)).toBe(true);
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Started a temporary machine', { exact: true })).toBeVisible();
	expect(state.decisions).toEqual(['approve']);
});

test('a command shows exactly what runs and where, then its result names the machine', async ({ page }) => {
	const command = "python3 -c 'print(2**100)' # <script>window.evil=1</script>";
	const state = await chat(page, [call('run_command', { command })], [
		approval('run_command', { ...machine, command, write: false, working_directory: '/workspace', timeout_seconds: 60 })
	]);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByText('Run a read-only command on tmp-1a2b3c4d?', { exact: true })).toBeVisible();
	await expect(page.getByText(command, { exact: false })).toBeVisible();
	expect(await page.evaluate(() => (window as any).evil)).toBeUndefined();
	await expect(
		page.getByText('tmp-1a2b3c4d: 1 vCPU, 2 GB memory, 20 GB disk · temporary machine, deleted when it expires')
	).toBeVisible();
	await expect(page.getByText('Runs in /workspace with /workspace read-only and no credentials; stopped after 60 seconds.')).toBeVisible();
	expect(await noHorizontalScroll(page)).toBe(true);

	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Started run_command on tmp-1a2b3c4d', { exact: true })).toBeVisible();
	await expect(page.getByText('run_command is queued')).toBeVisible();
	state.run = {
		...machine,
		run_id: 'run-1',
		status: 'succeeded',
		command,
		write: false,
		working_directory: '/workspace',
		exit_code: 0,
		output_tail: '1267650600228229401496703205376\n',
		output_truncated: false,
		duration_ms: 800
	};
	const result = page.getByTestId('command-run-result');
	await expect(result.getByText('run_command succeeded')).toBeVisible({ timeout: 10_000 });
	await expect(result.getByText('1267650600228229401496703205376')).toBeVisible();
	await expect(result.getByText('Exit code 0')).toBeVisible();
	await expect(
		result.getByText('tmp-1a2b3c4d: 1 vCPU, 2 GB memory, 20 GB disk · temporary machine, deleted when it expires')
	).toBeVisible();
	expect(await noHorizontalScroll(page)).toBe(true);
	expect(state.decisions).toEqual(['approve']);
});

test('a write command on the user own machine says it is reused and writable', async ({ page }) => {
	await chat(page, [call('run_command_write', { command: 'echo hi > note.txt' })], [
		approval('run_command_write', {
			...machine,
			machine: 'laptop-box',
			cpu_millis: 4000,
			memory_mb: 8192,
			disk_gb: 40,
			temporary: false,
			reused: true,
			expires_at: undefined,
			command: 'echo hi > note.txt',
			write: true,
			working_directory: '/workspace/kuayle-assistant/chat-1',
			timeout_seconds: 60
		})
	]);
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByText('Run a command that can write files on laptop-box?', { exact: true })).toBeVisible();
	await expect(page.getByText('laptop-box: 4 vCPU, 8 GB memory, 40 GB disk · your own machine, reused')).toBeVisible();
	await expect(
		page.getByText('Runs in /workspace/kuayle-assistant/chat-1 and can write to /workspace, with no credentials; stopped after 60 seconds.')
	).toBeVisible();
});
