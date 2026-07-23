import { expect, test } from '@playwright/test';

function createRequestDelay() {
	let markStarted!: () => void;
	let release!: () => void;
	let markCompleted!: () => void;
	const started = new Promise<void>((resolve) => { markStarted = resolve; });
	const released = new Promise<void>((resolve) => { release = resolve; });
	const completed = new Promise<void>((resolve) => { markCompleted = resolve; });
	return { started, markStarted, released, release, completed, markCompleted };
}

test('handles machine actions without applying stale poll, route, or trace responses', async ({ page }) => {
	const machineId = '00000000-0000-0000-0000-000000000050';
	let pauseRequests = 0;
	let cancelRequests = 0;
	let keepRunningRequests = 0;
	let runCancelled = false;
	let machineRequestDelay: ReturnType<typeof createRequestDelay> | null = null;
	let traceRequestDelay: ReturnType<typeof createRequestDelay> | null = null;
	const unhandledPaths: string[] = [];
	const machine = {
		id: machineId,
		workspace_id: '00000000-0000-0000-0000-000000000002',
		routing_key: '0123456789abcdef0123',
		name: 'Runtime smoke machine',
		status: 'running',
		desired_status: 'running',
		generation: 1,
		repo_url: 'https://github.com/kuayle/kuayle',
		repo_provider: 'github',
		repo_owner: 'kuayle',
		repo_name: 'kuayle',
		base_branch: 'main',
		working_branch: 'kuayle/runtime-smoke',
		machine_size: 'medium',
		cpu_millis: 4000,
		memory_mb: 8192,
		disk_gb: 20,
		pids_limit: 1024,
		max_runtime_minutes: 240,
		keep_running: false,
		environment_builder: false,
		created_at: '2026-07-13T00:00:00Z',
		updated_at: '2026-07-13T00:00:00Z',
		expires_at: '2099-07-13T04:00:00Z'
	};
	const secondMachineId = '00000000-0000-0000-0000-000000000059';
	const secondMachine = {
		...machine,
		id: secondMachineId,
		routing_key: 'fedcba98765432100123',
		name: 'Second route machine',
		working_branch: 'kuayle/second-route'
	};
	const firstRunId = '00000000-0000-0000-0000-000000000052';
	const secondRunId = '00000000-0000-0000-0000-000000000058';
	const checkout = {
		id: '00000000-0000-0000-0000-000000000053', workspace_id: machine.workspace_id,
		machine_id: machineId, issue_id: '00000000-0000-0000-0000-000000000054',
		github_repo_id: '00000000-0000-0000-0000-000000000055', repository_full_name: 'kuayle/kuayle',
		base_branch: 'main', working_branch: 'kuayle/runtime-smoke', workspace_path: '/workspace/tasks/runtime-smoke',
		status: 'ready', created_at: '2026-07-13T00:00:00Z', updated_at: '2026-07-13T00:00:00Z'
	};

	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;

		if (path === '/api/auth/me') {
			return route.fulfill({
				json: {
					id: '00000000-0000-0000-0000-000000000001',
					email: 'test@example.com',
					name: 'Test User',
					display_name: 'Test User',
					avatar_url: null
				}
			});
		}
		if (path === '/api/preferences') {
			return route.fulfill({
				json: {
					font_size: 'default', pointer_cursors: true, theme_mode: 'dark',
					light_theme: 'light', dark_theme: 'dark', workflow_sort_mode: 'default',
					workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'],
					team_workflow_sort_overrides: {}, issues_group_by: 'status'
				}
			});
		}
		if (path === '/api/workspaces') {
			return route.fulfill({ json: [{ id: machine.workspace_id, name: 'Test Workspace', slug: 'test' }] });
		}
		if (path === '/api/workspaces/test') {
			return route.fulfill({ json: { id: machine.workspace_id, name: 'Test Workspace', slug: 'test' } });
		}
		if (
			path === '/api/workspaces/test/teams' ||
			path === '/api/workspaces/test/projects' ||
			path === '/api/workspaces/test/labels' ||
			path === '/api/workspaces/test/members' ||
			path === '/api/workspaces/test/views'
		) {
			return route.fulfill({ json: [] });
		}
		if (path === '/api/notifications') {
			return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		}
		if (path === '/api/workspaces/test/dev-machines') {
			return route.fulfill({ json: { data: [machine], total_count: 1, page: 1, has_more: false } });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}` && request.method() === 'PATCH') {
			keepRunningRequests += 1;
			Object.assign(machine, request.postDataJSON());
			return route.fulfill({ json: machine });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}`) {
			const response = { ...machine };
			const delay = machineRequestDelay;
			if (delay) {
				machineRequestDelay = null;
				delay.markStarted();
				await delay.released;
				await route.fulfill({ json: response });
				delay.markCompleted();
				return;
			}
			return route.fulfill({ json: response });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/services`) {
			return route.fulfill({
				json: [{
					id: '00000000-0000-0000-0000-000000000051', machine_id: machineId,
					service_type: 'ide', service_key: 'ide', container_name: 'kuayle-test-ide',
					image_ref: 'kuayle/dev-machine-ide:0.1.0', status: 'running', health_status: 'healthy'
				}]
			});
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/providers`) {
			return route.fulfill({ json: [{ id: 'opencode', display_name: 'OpenCode', default_image: 'kuayle/dev-machine-agent-opencode:0.1.0', required_secrets: [], supported_modes: ['autonomous'], custom: false }] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/checkouts`) {
			return route.fulfill({ json: [checkout] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/events`) {
			return route.fulfill({ json: [{ id: 1, workspace_id: machine.workspace_id, machine_id: machineId, source: 'collector', event_type: 'machine_a_event', payload: {}, occurred_at: '2026-07-13T00:00:00Z', created_at: '2026-07-13T00:00:00Z' }] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/logs`) {
			return route.fulfill({ json: [{ id: 1, workspace_id: machine.workspace_id, machine_id: machineId, stream: 'system', sequence: 1, content: 'machine A log', truncated: false, created_at: '2026-07-13T00:00:00Z' }] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/agent-runs`) {
			return route.fulfill({ json: { data: [{
				id: firstRunId, machine_id: machineId,
				workspace_id: machine.workspace_id, provider_id: 'opencode', mode: 'autonomous',
				status: runCancelled ? 'cancelled' : 'running', prompt: 'Test run', max_runtime_seconds: 600,
				push_branch: false, open_pull_request: false, created_at: '2026-07-13T00:00:00Z'
			}, {
				id: secondRunId, machine_id: machineId,
				workspace_id: machine.workspace_id, provider_id: 'claude', mode: 'autonomous',
				status: 'succeeded', prompt: 'Second test run', max_runtime_seconds: 600,
				push_branch: false, open_pull_request: false, created_at: '2026-07-12T00:00:00Z'
			}], total_count: 2, page: 1, has_more: false } });
		}
		if (path === `/api/workspaces/test/agent-runs/${firstRunId}/cancel` && request.method() === 'POST') {
			cancelRequests += 1;
			runCancelled = true;
			return route.fulfill({ status: 202, body: '' });
		}
		if (path === `/api/workspaces/test/agent-runs/${firstRunId}/trace`) {
			const delay = traceRequestDelay;
			if (delay) {
				traceRequestDelay = null;
				delay.markStarted();
				await delay.released;
			}
			await route.fulfill({ json: {
				run: { id: firstRunId, machine_id: machineId, workspace_id: machine.workspace_id, provider_id: 'opencode', mode: 'autonomous', status: 'cancelled', prompt: 'Stale first trace prompt', max_runtime_seconds: 600, push_branch: false, open_pull_request: false, created_at: '2026-07-13T00:00:00Z' },
				steps: [], events: [], logs: [], next_event_id: 0, next_log_id: 0, has_more_events: false, has_more_logs: false
			} });
			delay?.markCompleted();
			return;
		}
		if (path === `/api/workspaces/test/agent-runs/${secondRunId}/trace`) {
			return route.fulfill({ json: {
				run: { id: secondRunId, machine_id: machineId, workspace_id: machine.workspace_id, provider_id: 'claude', mode: 'autonomous', status: 'succeeded', prompt: 'Current second trace prompt', max_runtime_seconds: 600, push_branch: false, open_pull_request: false, created_at: '2026-07-12T00:00:00Z' },
				steps: [], events: [], logs: [], next_event_id: 0, next_log_id: 0, has_more_events: false, has_more_logs: false
			} });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/resource-usage`) {
			return route.fulfill({ json: [] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/pause` && request.method() === 'POST') {
			pauseRequests += 1;
			Object.assign(machine, { status: 'paused', desired_status: 'paused', generation: machine.generation + 1 });
			return route.fulfill({ status: 202, json: { status: 'pending' } });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/start` && request.method() === 'POST') {
			Object.assign(machine, { status: 'running', desired_status: 'running', generation: machine.generation + 1 });
			return route.fulfill({ status: 202, json: { status: 'pending' } });
		}
		if (path === `/api/workspaces/test/dev-machines/${secondMachineId}`) {
			return route.fulfill({ json: secondMachine });
		}
		if (
			path === `/api/workspaces/test/dev-machines/${secondMachineId}/services` ||
			path === `/api/workspaces/test/dev-machines/${secondMachineId}/checkouts` ||
			path === `/api/workspaces/test/dev-machines/${secondMachineId}/events` ||
			path === `/api/workspaces/test/dev-machines/${secondMachineId}/logs` ||
			path === `/api/workspaces/test/dev-machines/${secondMachineId}/resource-usage`
		) {
			return route.fulfill({ json: [] });
		}
		if (path === `/api/workspaces/test/dev-machines/${secondMachineId}/agent-runs`) {
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		}

		unhandledPaths.push(`${request.method()} ${path}`);
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});

	await page.goto('/test/machines');
	await expect(page.getByRole('heading', { name: 'Dev Machines' })).toBeVisible();
	const machineLink = page.getByRole('link', { name: /Runtime smoke machine/ });
	await machineLink.evaluate((element) => {
		const link = element as HTMLAnchorElement;
		const url = new URL(link.href);
		url.hash = 'activity';
		link.href = url.toString();
	});
	await machineLink.click();
	await expect(page.getByText('Runtime smoke machine', { exact: true })).toBeVisible();
	await expect(page.getByRole('heading', { name: 'Services' })).toBeVisible();
	await expect(page.getByText('machine a event', { exact: true })).toBeVisible();
	await expect(page.getByText('[system] machine A log', { exact: true })).toBeVisible();
	await expect.poll(() => page.locator('#activity').evaluate((target) => {
		let container = target.parentElement;
		while (container && !['auto', 'scroll'].includes(getComputedStyle(container).overflowY)) container = container.parentElement;
		if (!container) return false;
		const targetRect = target.getBoundingClientRect();
		const containerRect = container.getBoundingClientRect();
		return container.scrollTop > 0 && targetRect.top < containerRect.bottom && targetRect.bottom > containerRect.top;
	})).toBe(true);
	await expect(page.getByTitle('Save development environment')).toHaveCount(0);
	await page.getByRole('switch', { name: 'Keep running' }).click();
	await expect.poll(() => keepRunningRequests).toBe(1);
	await page.getByRole('button', { name: 'Run agent' }).click();
	await expect(page.getByRole('heading', { name: 'Run Agent' })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Provider' })).toHaveText('OpenCode');
	const agentDialog = page.getByRole('dialog');
	const pushBranch = agentDialog.getByRole('switch', { name: 'Push working branch' });
	const openPullRequest = agentDialog.getByRole('switch', { name: 'Open pull request' });
	await openPullRequest.click();
	await expect(openPullRequest).toBeChecked();
	await pushBranch.click();
	await expect(pushBranch).not.toBeChecked();
	await expect(openPullRequest).not.toBeChecked();
	await expect(openPullRequest).toBeDisabled();
	await agentDialog.getByRole('button', { name: 'Cancel' }).click();
	await page.locator('#agent-runs').getByRole('button', { name: 'Cancel' }).click();
	await expect.poll(() => cancelRequests).toBe(1);
	await expect(page.getByText('cancelled', { exact: true })).toBeVisible();
	traceRequestDelay = createRequestDelay();
	const staleTrace = traceRequestDelay;
	await page.getByRole('button', { name: 'View opencode agent run activity' }).click();
	await staleTrace.started;
	await page.evaluate(({ href, runId }) => {
		const link = document.createElement('a');
		link.href = href;
		link.hash = `agent-run-${runId}`;
		document.body.append(link);
		link.click();
		link.remove();
	}, { href: `/test/machines/${machineId}?agent_run_id=${secondRunId}`, runId: secondRunId });
	await expect(page.getByText('Current second trace prompt', { exact: true })).toBeVisible();
	staleTrace.release();
	await staleTrace.completed;
	await page.waitForTimeout(50);
	await expect(page.getByText('Current second trace prompt', { exact: true })).toBeVisible();
	await expect(page.getByText('Stale first trace prompt', { exact: true })).toHaveCount(0);
	await page.keyboard.press('Escape');
	await expect(page.getByRole('heading', { name: 'Agent Run Trace' })).toHaveCount(0);

	machineRequestDelay = createRequestDelay();
	const stalePoll = machineRequestDelay;
	await stalePoll.started;
	await page.getByTitle('Pause').click();
	await expect.poll(() => pauseRequests).toBe(1);
	await expect(page.getByTitle('Start')).toBeVisible();
	stalePoll.release();
	await stalePoll.completed;
	await page.waitForTimeout(50);
	await expect(page.getByTitle('Start')).toBeVisible();

	machineRequestDelay = createRequestDelay();
	const staleLifecycleRefresh = machineRequestDelay;
	await page.getByTitle('Start').click();
	await staleLifecycleRefresh.started;
	await page.evaluate((href) => {
		const link = document.createElement('a');
		link.id = 'machine-b-link';
		link.href = href;
		link.textContent = 'Open second machine';
		document.body.append(link);
	}, `/test/machines/${secondMachineId}`);
	await page.locator('#machine-b-link').click();
	await expect(page).toHaveURL(new RegExp(`/test/machines/${secondMachineId}$`));
	await expect(page.getByText('Second route machine', { exact: true })).toBeVisible();
	staleLifecycleRefresh.release();
	await staleLifecycleRefresh.completed;
	await page.waitForTimeout(50);
	await expect(page.getByText('Second route machine', { exact: true })).toBeVisible();
	await expect(page.getByText('Runtime smoke machine', { exact: true })).toHaveCount(0);
	await expect(page.getByText('machine a event', { exact: true })).toHaveCount(0);
	await expect(page.getByText('[system] machine A log', { exact: true })).toHaveCount(0);
	await expect(page.getByText('Test run', { exact: true })).toHaveCount(0);
	await expect(page.getByText('kuayle/runtime-smoke', { exact: true })).toHaveCount(0);
	expect(unhandledPaths).toEqual([]);
});

test('creates a generic machine with an accessible size and inactivity controls', async ({ page }) => {
	let createPayload: Record<string, unknown> | undefined;
	const availabilityRequests: string[] = [];
	const takenNameDelay = createRequestDelay();
	let suggestionDelay: ReturnType<typeof createRequestDelay> | null = null;
	const workspaceId = '00000000-0000-0000-0000-000000000002';

	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		const path = url.pathname;
		if (path === '/api/auth/me') return route.fulfill({ json: { id: '00000000-0000-0000-0000-000000000001', email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null } });
		if (path === '/api/preferences') return route.fulfill({ json: { font_size: 'default', pointer_cursors: true, theme_mode: 'dark', light_theme: 'light', dark_theme: 'dark', workflow_sort_mode: 'default', workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'], team_workflow_sort_overrides: {}, issues_group_by: 'status' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'owner' } });
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`)) return route.fulfill({ json: [] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/dev-machines' && request.method() === 'GET') return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path === '/api/workspaces/test/dev-machine-names/suggestion') {
			const delay = suggestionDelay;
			if (delay) {
				suggestionDelay = null;
				delay.markStarted();
				await delay.released;
			}
			await route.fulfill({ json: { name: 'quiet-orchid-7f3a', available: true } });
			delay?.markCompleted();
			return;
		}
		if (path === '/api/workspaces/test/dev-machine-names/availability') {
			const requestedName = url.searchParams.get('name') ?? '';
			availabilityRequests.push(requestedName);
			if (requestedName === 'taken-machine') {
				takenNameDelay.markStarted();
				await takenNameDelay.released;
			}
			await route.fulfill({ json: { name: requestedName, available: requestedName !== 'taken-machine' } });
			if (requestedName === 'taken-machine') takenNameDelay.markCompleted();
			return;
		}
		if (path === '/api/workspaces/test/dev-machine-providers') return route.fulfill({ json: [{ id: 'opencode', display_name: 'OpenCode', default_image: 'kuayle/opencode:1', required_secrets: [], supported_modes: ['autonomous'], custom: false }] });
		if (path === '/api/workspaces/test/dev-machine-policy') return route.fulfill({ json: { workspace_id: workspaceId, enabled: true, max_concurrent_machines: 5, max_machines_per_user: 2, max_daily_agent_runs: 25, max_runtime_minutes: 480, max_disk_gb: 100, idle_pause_minutes: 240, allowed_providers: ['opencode'], allowed_repositories: [], allow_custom_providers: false } });
		if (path === '/api/workspaces/test/dev-machine-environments') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/dev-machines' && request.method() === 'POST') {
			createPayload = request.postDataJSON();
			return route.fulfill({ status: 201, json: { id: '00000000-0000-0000-0000-000000000060', ...createPayload } });
		}
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});

	await page.goto('/test/machines');
	await page.locator('header').getByRole('button', { name: 'New machine' }).click();
	const dialog = page.getByRole('dialog');
	await expect(dialog.getByRole('heading', { name: 'New Dev Machine' })).toBeVisible();
	await expect(dialog.getByLabel('Repository')).toHaveCount(0);
	await expect(dialog.getByLabel('Machine name')).toHaveValue('quiet-orchid-7f3a');
	await dialog.getByLabel('Machine name').fill('pending-close');
	await dialog.getByRole('button', { name: 'Cancel' }).click();
	await page.waitForTimeout(350);
	expect(availabilityRequests).not.toContain('pending-close');

	await page.locator('header').getByRole('button', { name: 'New machine' }).click();
	await expect(dialog.getByLabel('Machine name')).toHaveValue('quiet-orchid-7f3a');
	await dialog.getByLabel('Machine name').fill('taken-machine');
	await takenNameDelay.started;
	await dialog.getByRole('button', { name: 'Cancel' }).click();
	suggestionDelay = createRequestDelay();
	const delayedSuggestion = suggestionDelay;
	await page.locator('header').getByRole('button', { name: 'New machine' }).click();
	await delayedSuggestion.started;
	await expect(dialog.getByLabel('Machine name')).toHaveValue('');
	takenNameDelay.release();
	await takenNameDelay.completed;
	await page.waitForTimeout(50);
	await expect(dialog.getByLabel('Machine name')).toHaveValue('');
	delayedSuggestion.release();
	await delayedSuggestion.completed;
	await expect(dialog.getByLabel('Machine name')).toHaveValue('quiet-orchid-7f3a');
	await dialog.getByLabel('Machine name').fill('fresh-machine');
	await expect(dialog.getByText('Name is available')).toBeVisible();
	await expect(dialog.locator('[data-selected="true"]')).toContainText('medium');
	await dialog.locator('label').filter({ hasText: 'large' }).click();
	await expect(dialog.locator('[data-selected="true"]')).toContainText('large');
	await dialog.getByRole('switch', { name: 'Keep running' }).click();
	await dialog.getByRole('button', { name: 'Create machine' }).click();
	await expect.poll(() => createPayload).toBeTruthy();
	expect(createPayload).toMatchObject({
		name: 'fresh-machine', size: 'large', keep_running: true,
		services: { ide: true, browser: true }
	});
	expect(availabilityRequests).toContain('taken-machine');
	expect(availabilityRequests).toContain('fresh-machine');
	expect(createPayload).not.toHaveProperty('repo');
	expect(createPayload).not.toHaveProperty('ttl_minutes');
});

test('retries a code-server launch while a paused machine resumes', async ({ page }) => {
	const workspaceId = '00000000-0000-0000-0000-000000000002';
	const machineId = '00000000-0000-0000-0000-000000000070';
	let launchRequests = 0;
	let failedLaunchRequests = 0;
	let failLaunch = false;
	let machineGets = 0;
	const machine = {
		id: machineId, workspace_id: workspaceId, routing_key: 'pausedmachine00000001', name: 'Paused machine',
		status: 'paused', desired_status: 'paused', generation: 1, repo_url: '', repo_owner: '', repo_name: '', base_branch: '', working_branch: '',
		machine_size: 'medium', cpu_millis: 4000, memory_mb: 8192, disk_gb: 50, pids_limit: 1024, max_runtime_minutes: 240,
		keep_running: false, environment_builder: true, created_at: '2026-07-13T00:00:00Z', updated_at: '2026-07-13T00:00:00Z', expires_at: '2099-07-13T04:00:00Z'
	};
	const unhandledPaths: string[] = [];

	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me') return route.fulfill({ json: { id: '00000000-0000-0000-0000-000000000001', email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null } });
		if (path === '/api/preferences') return route.fulfill({ json: { font_size: 'default', pointer_cursors: true, theme_mode: 'dark', light_theme: 'light', dark_theme: 'dark', workflow_sort_mode: 'default', workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'], team_workflow_sort_overrides: {}, issues_group_by: 'status' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'owner' } });
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`)) return route.fulfill({ json: [] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === `/api/workspaces/test/dev-machines/${machineId}`) {
			machineGets += 1;
			if (machineGets > 1) Object.assign(machine, { status: 'running', desired_status: 'running' });
			return route.fulfill({ json: machine });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/services`) return route.fulfill({ json: [{ id: 'svc-ide', machine_id: machineId, service_type: 'ide', service_key: 'ide', container_name: 'ide', image_ref: 'ide:1', status: machine.status === 'running' ? 'running' : 'stopped', health_status: machine.status === 'running' ? 'healthy' : 'paused' }] });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/checkouts`) return route.fulfill({ json: [] });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/events`) return route.fulfill({ json: [] });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/logs`) return route.fulfill({ json: [] });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/agent-runs`) return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/resource-usage`) return route.fulfill({ json: [] });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/services/ide/launch` && request.method() === 'POST') {
			if (failLaunch) {
				failedLaunchRequests += 1;
				if (failedLaunchRequests === 1) return route.fulfill({ status: 202, json: { status: 'pending', retry_after_seconds: 1 } });
				return route.fulfill({ status: 500, json: { error: { message: 'Resume failed' } } });
			}
			launchRequests += 1;
			if (launchRequests <= 2) {
				Object.assign(machine, { desired_status: 'running' });
				return route.fulfill({ status: 202, json: { status: 'resuming', retry_after_seconds: 1, operation: { id: 'op-resume', action: 'start', status: 'pending', generation: 2, idempotency_key: 'launch-resume', attempts: 0, created_at: '2026-07-13T00:00:00Z' } } });
			}
			return route.fulfill({ status: 201, json: { status: 'ready', launch_url: 'https://code.example.test/?ticket=redacted', expires_at: '2099-07-13T04:00:00Z' } });
		}
		unhandledPaths.push(`${request.method()} ${path}`);
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});

	await page.goto(`/test/machines/${machineId}`);
	await expect(page.getByText('Paused machine', { exact: true })).toBeVisible();
	const popupPromise = page.waitForEvent('popup');
	await page.getByRole('button', { name: /Code Editor/ }).first().click();
	await popupPromise;
	await expect.poll(() => launchRequests).toBeGreaterThanOrEqual(2);
	await expect(page.locator('.app-toast-shell')).toHaveCount(1);
	await expect.poll(() => launchRequests).toBe(3);
	await expect(page.getByText('Dev Machine is ready', { exact: true })).toBeVisible();
	await expect(page.locator('.app-toast-shell')).toHaveCount(1);

	failLaunch = true;
	const failedPopupPromise = page.waitForEvent('popup');
	await page.getByRole('button', { name: /Code Editor/ }).first().click();
	const failedPopup = await failedPopupPromise;
	await expect.poll(() => failedLaunchRequests).toBe(2);
	await expect(page.getByText('Resume failed', { exact: true })).toBeVisible();
	await expect(page.locator('.app-toast-shell')).toHaveCount(1);
	await expect.poll(() => failedPopup.isClosed()).toBe(true);
	expect(unhandledPaths).toEqual([]);
});

test('keeps multiple docked terminals alive across collapse and navigation', async ({ page }) => {
	const workspaceId = '00000000-0000-0000-0000-000000000002';
	const machineId = '00000000-0000-0000-0000-000000000080';
	const checkoutId = '00000000-0000-0000-0000-000000000081';
	const terminalPayloads: Record<string, unknown>[] = [];
	let closeRequests = 0;
	let terminalSessions = 0;
	let listSessionRequests = 0;

	await page.addInitScript(() => {
		class MockWebSocket extends EventTarget {
			static CONNECTING = 0;
			static OPEN = 1;
			static CLOSING = 2;
			static CLOSED = 3;
			readyState = MockWebSocket.CONNECTING;
			binaryType = 'arraybuffer';
			url: string;
			constructor(url: string) {
				super();
				this.url = url;
				if (url.startsWith('wss://terminal.example.test')) {
					(window as any).__terminalSockets ??= [];
					(window as any).__terminalSockets.push({ url, sent: [], closed: false });
				}
				setTimeout(() => {
					this.readyState = MockWebSocket.OPEN;
					this.dispatchEvent(new Event('open'));
				}, 0);
			}
			send(data: string | ArrayBuffer | Uint8Array) {
				const bytes = typeof data === 'string' ? Array.from(new TextEncoder().encode(data)) : Array.from(new Uint8Array(data instanceof ArrayBuffer ? data : data.buffer));
				const record = (window as any).__terminalSockets?.find((item: { url: string }) => item.url === this.url);
				if (record) record.sent.push(bytes);
			}
			close() {
				this.readyState = MockWebSocket.CLOSED;
				const record = (window as any).__terminalSockets?.find((item: { url: string }) => item.url === this.url);
				if (record) record.closed = true;
				this.dispatchEvent(new CloseEvent('close', { code: 1000 }));
			}
		}
		(window as any).WebSocket = MockWebSocket;
	});

	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const machine = { id: machineId, workspace_id: workspaceId, routing_key: 'terminalmachine0001', name: 'Terminal machine', status: 'running', desired_status: 'running', generation: 1, repo_owner: 'kuayle', repo_name: 'kuayle', base_branch: 'main', working_branch: 'work', machine_size: 'medium', cpu_millis: 4000, memory_mb: 8192, disk_gb: 50, pids_limit: 1024, max_runtime_minutes: 240, keep_running: false, environment_builder: false, created_at: '2026-07-13T00:00:00Z', updated_at: '2026-07-13T00:00:00Z', expires_at: '2099-07-13T04:00:00Z' };
		const checkout = { id: checkoutId, workspace_id: workspaceId, machine_id: machineId, issue_id: '00000000-0000-0000-0000-000000000082', github_repo_id: '00000000-0000-0000-0000-000000000083', repository_full_name: 'kuayle/kuayle', base_branch: 'main', working_branch: 'user/TST-1-terminal', workspace_path: '/workspace/tasks/TST-1-terminal', status: 'ready', created_at: '2026-07-13T00:00:00Z', updated_at: '2026-07-13T00:00:00Z' };
		if (path === '/api/auth/me') return route.fulfill({ json: { id: '00000000-0000-0000-0000-000000000001', email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null } });
		if (path === '/api/preferences') return route.fulfill({ json: { font_size: 'default', pointer_cursors: true, theme_mode: 'dark', light_theme: 'light', dark_theme: 'dark', workflow_sort_mode: 'default', workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'], team_workflow_sort_overrides: {}, issues_group_by: 'status' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'owner' } });
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`)) return route.fulfill({ json: [] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/dev-machines' && request.method() === 'GET') return route.fulfill({ json: { data: [machine], total_count: 1, page: 1, has_more: false } });
		if (path === `/api/workspaces/test/dev-machines/${machineId}`) return route.fulfill({ json: machine });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/services`) return route.fulfill({ json: [
			{ id: 'svc-ide', machine_id: machineId, service_type: 'ide', service_key: 'ide', container_name: 'ide', image_ref: 'ide:1', status: 'running', health_status: 'healthy' },
			{ id: 'svc-terminal', machine_id: machineId, service_type: 'terminal', service_key: 'terminal', container_name: 'terminal', image_ref: 'ide:1', status: 'running', health_status: 'healthy' }
		] });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/checkouts`) return route.fulfill({ json: [checkout] });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/events`) return route.fulfill({ json: [] });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/logs`) return route.fulfill({ json: [] });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/agent-runs`) return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/resource-usage`) return route.fulfill({ json: [] });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/terminal-sessions` && request.method() === 'GET') {
			listSessionRequests += 1;
			return route.fulfill({ json: [] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/terminal-sessions` && request.method() === 'POST') {
			terminalPayloads.push(request.postDataJSON());
			terminalSessions += 1;
			const suffix = terminalSessions === 1 ? '84' : '85';
			const sessionId = `00000000-0000-0000-0000-0000000000${suffix}`;
			return route.fulfill({ status: 201, json: { status: 'ready', protocol: 'ttyd.v1', web_socket_url: `wss://terminal.example.test/ws?ticket=secret-${terminalSessions}&session=term-${terminalSessions}&cwd=/workspace/tasks/TST-1-terminal`, expires_at: '2099-07-13T04:00:00Z', session: { id: sessionId, machine_id: machineId, checkout_id: checkoutId, name: 'Terminal', runtime_session_name: `term-${terminalSessions}`, status: 'active', created_at: '2026-07-13T00:00:00Z', last_activity_at: '2026-07-13T00:00:00Z' } } });
		}
		if (path.startsWith(`/api/workspaces/test/dev-machines/${machineId}/terminal-sessions/`) && path.endsWith('/close') && request.method() === 'POST') {
			closeRequests += 1;
			return route.fulfill({ json: { id: path.split('/').at(-2), machine_id: machineId, checkout_id: checkoutId, name: 'Terminal', runtime_session_name: 'term', status: 'closed', created_at: '2026-07-13T00:00:00Z', last_activity_at: '2026-07-13T00:00:00Z', closed_at: '2026-07-13T00:01:00Z' } });
		}
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});

	await page.goto(`/test/machines/${machineId}`);
	await expect(page.getByTestId('terminal-dock')).toHaveCount(0);
	await page.getByRole('button', { name: 'Terminal', exact: true }).click();
	await expect(page.getByTestId('terminal-dock')).toBeVisible();
	await expect(page.getByTestId('native-terminal')).toBeVisible();
	await expect.poll(() => terminalPayloads.length).toBe(1);
	expect(listSessionRequests).toBe(0);
	expect(terminalPayloads[0]).toMatchObject({ checkout_id: checkoutId });
	await expect.poll(() => page.evaluate(() => (window as any).__terminalSockets?.[0]?.sent.length ?? 0)).toBeGreaterThan(0);
	const firstFrame = await page.evaluate(() => new TextDecoder().decode(new Uint8Array((window as any).__terminalSockets[0].sent[0])));
	const initialSize = JSON.parse(firstFrame);
	expect(initialSize).toMatchObject({ AuthToken: '', columns: expect.any(Number), rows: expect.any(Number) });
	expect(initialSize.columns).toBeGreaterThan(20);
	expect(initialSize.rows).toBeGreaterThan(5);
	expect(initialSize.rows).toBeLessThan(100);
	const dockHeight = await page.getByTestId('terminal-dock').evaluate((element) => element.getBoundingClientRect().height);
	const terminalHeight = await page.getByTestId('native-terminal').evaluate((element) => element.getBoundingClientRect().height);
	expect(terminalHeight).toBeLessThanOrEqual(dockHeight);

	await page.getByRole('button', { name: 'Collapse terminal dock' }).click();
	await expect(page.getByTestId('native-terminal')).toBeHidden();
	await expect.poll(() => page.evaluate(() => (window as any).__terminalSockets?.filter((item: { closed: boolean }) => item.closed).length ?? 0)).toBe(0);
	await page.getByRole('button', { name: 'Expand terminal dock' }).click();
	await expect(page.getByTestId('native-terminal')).toBeVisible();

	await page.getByRole('button', { name: 'Terminal', exact: true }).click();
	await expect.poll(() => terminalPayloads.length).toBe(2);
	await expect(page.getByRole('tab')).toHaveCount(2);
	await expect.poll(() => page.evaluate(() => (window as any).__terminalSockets?.[1]?.sent.length ?? 0)).toBeGreaterThan(0);
	await page.getByRole('tab').first().click();
	await expect.poll(() => page.evaluate(() => (window as any).__terminalSockets?.filter((item: { closed: boolean }) => item.closed).length ?? 0)).toBe(0);

	await page.getByRole('link', { name: 'Dev Machines' }).click();
	await expect(page).toHaveURL(/\/test\/machines$/);
	await expect(page.getByTestId('terminal-dock')).toBeVisible();
	await expect(page.getByRole('tab')).toHaveCount(2);
	await expect.poll(() => closeRequests).toBe(0);

	await page.getByTestId('close-tab').first().click();
	await expect.poll(() => closeRequests).toBe(1);
	await expect(page.getByRole('tab')).toHaveCount(1);
	await page.getByTestId('close-tab').click();
	await expect.poll(() => closeRequests).toBe(2);
	await expect(page.getByTestId('terminal-dock')).toHaveCount(0);
});

test('uses guarded permanent-delete routes for old-machine cleanup', async ({ page }) => {
	const workspaceId = '00000000-0000-0000-0000-000000000002';
	let bulkPayload: Record<string, unknown> | undefined;
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me') return route.fulfill({ json: { id: '00000000-0000-0000-0000-000000000001', email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null } });
		if (path === '/api/preferences') return route.fulfill({ json: { font_size: 'default', pointer_cursors: true, theme_mode: 'dark', light_theme: 'light', dark_theme: 'dark', workflow_sort_mode: 'default', workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'], team_workflow_sort_overrides: {}, issues_group_by: 'status' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'owner' } });
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`)) return route.fulfill({ json: [] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/dev-machines' && request.method() === 'GET') return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path === '/api/workspaces/test/dev-machines/bulk/permanent-delete' && request.method() === 'POST') {
			bulkPayload = request.postDataJSON();
			return route.fulfill({ json: { count: 3 } });
		}
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});

	await page.goto('/test/machines');
	await page.getByRole('button', { name: 'Delete old' }).click();
	await page.getByRole('button', { name: 'Permanently delete old machines' }).click();
	await expect.poll(() => bulkPayload).toMatchObject({ include_failed: true, include_expired: true });
});

test('cancels Issue Machine Picker work when the dialog is dismissed', async ({ page }) => {
	const workspaceId = '00000000-0000-0000-0000-000000000002';
	const teamId = '00000000-0000-0000-0000-000000000010';
	const issueId = '00000000-0000-0000-0000-000000000100';
	const machineId = '00000000-0000-0000-0000-000000000110';
	const checkoutId = '00000000-0000-0000-0000-000000000111';
	let machineGets = 0;
	let startRequests = 0;
	let checkoutRequests = 0;
	let terminalRequests = 0;
	let checkoutDelay: ReturnType<typeof createRequestDelay> | null = null;
	let launchDelay: ReturnType<typeof createRequestDelay> | null = null;
	const issue = {
		id: issueId, identifier: 'TST-43', title: 'Picker cancellation test', description: null,
		status: 'backlog', team_id: teamId, project_id: null, cycle_id: null, creator_id: 'u1',
		assignee_id: null, parent_id: null, due_date: null, sort_order: 0,
		created_at: '2026-07-13T00:00:00Z', updated_at: '2026-07-13T00:00:00Z'
	};
	const machine = {
		id: machineId, workspace_id: workspaceId, routing_key: 'pickercancellation001', name: 'Picker machine',
		status: 'paused', desired_status: 'paused', generation: 1, repo_owner: 'kuayle', repo_name: 'kuayle',
		base_branch: 'main', working_branch: 'kuayle/picker', machine_size: 'medium', cpu_millis: 4000,
		memory_mb: 8192, disk_gb: 50, pids_limit: 1024, max_runtime_minutes: 240, keep_running: false,
		environment_builder: false, created_at: '2026-07-13T00:00:00Z', updated_at: '2026-07-13T00:00:00Z',
		expires_at: '2099-07-13T04:00:00Z'
	};
	const checkout = {
		id: checkoutId, workspace_id: workspaceId, machine_id: machineId, issue_id: issueId,
		github_repo_id: '00000000-0000-0000-0000-000000000112', repository_full_name: 'kuayle/kuayle',
		base_branch: 'main', working_branch: 'kuayle/TST-43-picker', workspace_path: '/workspace/tasks/TST-43-picker',
		status: 'ready', created_at: '2026-07-13T00:00:00Z', updated_at: '2026-07-13T00:00:00Z'
	};

	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me') return route.fulfill({ json: { id: 'u1', email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null } });
		if (path === '/api/preferences') return route.fulfill({ json: { font_size: 'default', pointer_cursors: true, theme_mode: 'dark', light_theme: 'light', dark_theme: 'dark', workflow_sort_mode: 'default', workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'], team_workflow_sort_overrides: {}, issues_group_by: 'status' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'owner' } });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [{ id: teamId, name: 'Engineering', key: 'ENG', color: '#6366f1', icon: 'layers', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }] });
		if (['/api/workspaces/test/projects', '/api/workspaces/test/labels', '/api/workspaces/test/members', '/api/workspaces/test/views'].includes(path)) return route.fulfill({ json: [] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === `/api/workspaces/test/teams/${teamId}/statuses` || path === `/api/workspaces/test/teams/${teamId}/cycles`) return route.fulfill({ json: [] });
		if (path === `/api/workspaces/test/issues/${issue.identifier}` && request.method() === 'GET') return route.fulfill({ json: issue });
		if (path === `/api/workspaces/test/issues/${issue.identifier}/comments` || path === `/api/workspaces/test/issues/${issue.identifier}/history`) return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/issues' && request.method() === 'GET') return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path === '/api/workspaces/test/dev-machines' && request.method() === 'GET') return route.fulfill({ json: { data: [machine], total_count: 1, page: 1, has_more: false } });
		if (path === `/api/workspaces/test/dev-machines/${machineId}` && request.method() === 'GET') {
			machineGets += 1;
			return route.fulfill({ json: machine });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/start` && request.method() === 'POST') {
			startRequests += 1;
			Object.assign(machine, { status: 'running', desired_status: 'running', generation: 2 });
			return route.fulfill({ status: 202, json: { status: 'pending' } });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/checkouts` && request.method() === 'GET') {
			checkoutRequests += 1;
			const delay = checkoutDelay;
			if (delay) {
				checkoutDelay = null;
				delay.markStarted();
				await delay.released;
			}
			await route.fulfill({ json: [checkout] });
			delay?.markCompleted();
			return;
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/services/ide/launch` && request.method() === 'POST') {
			const delay = launchDelay;
			if (delay) {
				launchDelay = null;
				delay.markStarted();
				await delay.released;
			}
			await route.fulfill({ status: 201, json: { status: 'ready', launch_url: 'https://code.example.test/?ticket=redacted', expires_at: '2099-07-13T04:00:00Z' } });
			delay?.markCompleted();
			return;
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/terminal-sessions` && request.method() === 'POST') {
			terminalRequests += 1;
			return route.fulfill({ status: 202, json: { status: 'pending' } });
		}
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});

	async function openPicker(action: 'Open Code Editor' | 'Open Terminal' | 'Run Agent') {
		await page.getByTitle('Issue actions').click();
		await page.getByRole('button', { name: action }).click();
		const picker = page.getByRole('dialog', { name: 'Choose Dev Machine' });
		await expect(picker).toBeVisible();
		return picker;
	}

	await page.goto('/test/issue/TST-43');
	await expect(page.getByText('Picker cancellation test')).toBeVisible();
	const terminalPicker = await openPicker('Open Terminal');
	await terminalPicker.getByRole('button', { name: 'Open Terminal' }).click();
	await expect.poll(() => startRequests).toBe(1);
	await page.keyboard.press('Escape');
	await expect(page.getByRole('heading', { name: 'Choose Dev Machine' })).toHaveCount(0);
	await expect.poll(() => machineGets).toBeGreaterThan(1);
	await page.waitForTimeout(50);
	expect(checkoutRequests).toBe(0);
	expect(terminalRequests).toBe(0);
	await expect(page.getByTestId('terminal-dock')).toHaveCount(0);

	checkoutDelay = createRequestDelay();
	const delayedCheckout = checkoutDelay;
	const agentPicker = await openPicker('Run Agent');
	await agentPicker.getByRole('button', { name: 'Continue to Agent' }).click();
	await delayedCheckout.started;
	await page.keyboard.press('Escape');
	delayedCheckout.release();
	await delayedCheckout.completed;
	await page.waitForTimeout(50);
	await expect(page.getByRole('heading', { name: 'Run Agent' })).toHaveCount(0);

	launchDelay = createRequestDelay();
	const delayedLaunch = launchDelay;
	const idePicker = await openPicker('Open Code Editor');
	const popupPromise = page.waitForEvent('popup');
	await idePicker.getByRole('button', { name: 'Open Code Editor' }).click();
	const popup = await popupPromise;
	await delayedLaunch.started;
	await page.keyboard.press('Escape');
	await expect.poll(() => popup.isClosed()).toBe(true);
	delayedLaunch.release();
	await delayedLaunch.completed;
	await page.waitForTimeout(50);
	expect(popup.isClosed()).toBe(true);
	await expect(page.getByRole('heading', { name: 'Choose Dev Machine' })).toHaveCount(0);
	await expect(page).toHaveURL('/test/issue/TST-43');
});

test('searches linked repositories in IssueRepositoryDialog and saves selection', async ({ page }) => {
	const workspaceId = '00000000-0000-0000-0000-000000000002';
	const teamId = '00000000-0000-0000-0000-000000000010';
	const issueId = '00000000-0000-0000-0000-000000000100';
	let putPayload: Record<string, unknown> | undefined;

	const repos = [
		{ id: 'repo-1', github_repo_id: 1001, full_name: 'kuayle/kuayle', default_branch: 'main', is_active: true },
		{ id: 'repo-2', github_repo_id: 1002, full_name: 'kuayle/backend', default_branch: 'develop', is_active: true },
	];

	const issue = {
		id: issueId, identifier: 'TST-42', title: 'Searchable repository test', description: null,
		status: 'backlog', team_id: teamId, project_id: null, cycle_id: null, creator_id: 'u1',
		assignee_id: null, parent_id: null, due_date: null, sort_order: 0,
		created_at: '2026-07-13T00:00:00Z', updated_at: '2026-07-13T00:00:00Z'
	};

	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;

		if (path === '/api/auth/me') return route.fulfill({ json: { id: 'u1', email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null } });
		if (path === '/api/preferences') return route.fulfill({ json: { font_size: 'default', pointer_cursors: true, theme_mode: 'dark', light_theme: 'light', dark_theme: 'dark', workflow_sort_mode: 'default', workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'], team_workflow_sort_overrides: {}, issues_group_by: 'status' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'owner' } });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [{ id: teamId, name: 'Engineering', key: 'ENG', color: '#6366f1', icon: 'layers', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }] });
		if (['/api/workspaces/test/projects', '/api/workspaces/test/labels', '/api/workspaces/test/members', '/api/workspaces/test/views'].includes(path)) return route.fulfill({ json: [] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === `/api/workspaces/test/teams/${teamId}/statuses`) return route.fulfill({ json: [] });
		if (path === `/api/workspaces/test/teams/${teamId}/cycles`) return route.fulfill({ json: [] });
		if (path === `/api/workspaces/test/issues/${issue.identifier}` && request.method() === 'GET') return route.fulfill({ json: issue });
		if (path === `/api/workspaces/test/issues/${issue.identifier}/comments`) return route.fulfill({ json: [] });
		if (path === `/api/workspaces/test/issues/${issue.identifier}/history`) return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/issues' && request.method() === 'GET') return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path === '/api/workspaces/test/github/status') return route.fulfill({ json: { configured: true, installed: true, repos } });
		if (path === '/api/workspaces/test/dev-machine-environments') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/dev-machine-scope-setting' && request.method() === 'GET') return route.fulfill({ json: { workspace_id: workspaceId, issue_id: issueId } });
		if (path === '/api/workspaces/test/dev-machine-scope-setting' && request.method() === 'PUT') {
			putPayload = request.postDataJSON();
			return route.fulfill({ json: { workspace_id: workspaceId, issue_id: issueId } });
		}
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});

	await page.goto('/test/issue/TST-42');
	await expect(page.getByText('Searchable repository test')).toBeVisible();

	await page.getByTitle('Issue actions').click();
	await page.getByRole('button', { name: 'Set Development Defaults' }).click();

	const dialog = page.getByRole('dialog');
	await expect(dialog.getByRole('heading', { name: 'Issue development defaults' })).toBeVisible();

	const repoButton = dialog.getByLabel('Repository');
	await expect(repoButton).toBeEnabled();
	await repoButton.click();

	await page.getByPlaceholder('Search repositories...').fill('backend');
	await page.getByRole('option', { name: 'kuayle/backend' }).click();

	await dialog.getByRole('button', { name: 'Save defaults' }).click();

	await expect.poll(() => putPayload).toBeTruthy();
	expect(putPayload).toMatchObject({
		scope_type: 'issue',
		scope_id: issueId,
		github_repo_id: 'repo-2',
		base_branch: 'develop'
	});
});

test('opens agent-run trace sheet from card click, activity click, deep link, and cursor polls', async ({ page }) => {
	const machineId = '00000000-0000-0000-0000-000000000060';
	const runId = '00000000-0000-0000-0000-000000000061';
	const runId2 = '00000000-0000-0000-0000-000000000062';
	let traceRequests = 0;
	let lastTraceEventsAfterId = 0;
	let lastTraceLogsAfterId = 0;

	const machine = {
		id: machineId,
		workspace_id: '00000000-0000-0000-0000-000000000002',
		routing_key: '0123456789abcdef0123',
		name: 'Trace test machine',
		status: 'running',
		desired_status: 'running',
		generation: 1,
		repo_url: 'https://github.com/kuayle/kuayle',
		repo_provider: 'github',
		repo_owner: 'kuayle',
		repo_name: 'kuayle',
		base_branch: 'main',
		working_branch: 'kuayle/trace',
		machine_size: 'medium',
		cpu_millis: 4000,
		memory_mb: 8192,
		disk_gb: 20,
		pids_limit: 1024,
		max_runtime_minutes: 240,
		keep_running: false,
		environment_builder: false,
		created_at: '2026-07-13T00:00:00Z',
		updated_at: '2026-07-13T00:00:00Z',
		expires_at: '2099-07-13T04:00:00Z'
	};

	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;

		if (path === '/api/auth/me') {
			return route.fulfill({ json: { id: '00000000-0000-0000-0000-000000000001', email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null } });
		}
		if (path === '/api/preferences') {
			return route.fulfill({ json: { font_size: 'default', pointer_cursors: true, theme_mode: 'dark', light_theme: 'light', dark_theme: 'dark', workflow_sort_mode: 'default', workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'], team_workflow_sort_overrides: {}, issues_group_by: 'status' } });
		}
		if (path === '/api/workspaces') {
			return route.fulfill({ json: [{ id: machine.workspace_id, name: 'Test Workspace', slug: 'test' }] });
		}
		if (path === '/api/workspaces/test') {
			return route.fulfill({ json: { id: machine.workspace_id, name: 'Test Workspace', slug: 'test', current_user_role: 'admin' } });
		}
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`)) {
			return route.fulfill({ json: [] });
		}
		if (path === '/api/notifications') {
			return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		}
		if (path === '/api/workspaces/test/dev-machines') {
			return route.fulfill({ json: { data: [machine], total_count: 1, page: 1, per_page: 50, has_more: false } });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}` && request.method() === 'GET') {
			return route.fulfill({ json: machine });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/services`) {
			return route.fulfill({ json: [] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/checkouts`) {
			return route.fulfill({ json: [] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/resource-usage`) {
			return route.fulfill({ json: [] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/agent-runs` && request.method() === 'GET') {
			return route.fulfill({
				json: {
					data: [
						{ id: runId, machine_id: machineId, provider_id: 'opencode', mode: 'autonomous', status: 'succeeded', prompt: 'Fix the bug', summary: 'Fixed bug in login', changed_files: ['src/login.ts'], commits: ['abc1234'], branch: 'kuayle/trace', pull_request_url: null, tests_run: ['TestLogin'], test_status: 'passed', risk_notes: ['Minor risk'], exit_code: 0, error_message: null, created_at: '2026-07-13T00:00:00Z', started_at: '2026-07-13T00:01:00Z', completed_at: '2026-07-13T00:05:00Z' },
						{ id: runId2, machine_id: machineId, provider_id: 'claude-code', mode: 'autonomous', status: 'running', prompt: 'Add tests', summary: null, changed_files: [], commits: [], branch: null, pull_request_url: null, tests_run: [], test_status: 'not_run', risk_notes: [], exit_code: null, error_message: null, created_at: '2026-07-13T01:00:00Z', started_at: '2026-07-13T01:01:00Z', completed_at: null }
					],
					total_count: 2, page: 1, per_page: 50, has_more: false
				}
			});
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/events` && request.method() === 'GET') {
			return route.fulfill({
				json: [
					{ id: 1, machine_id: machineId, agent_run_id: runId, source: 'agent', event_type: 'agent_run.queued', payload: {}, occurred_at: '2026-07-13T00:00:00Z' },
					{ id: 2, machine_id: machineId, agent_run_id: runId, source: 'agent', event_type: 'agent_run.completed', payload: { exit_code: 0 }, occurred_at: '2026-07-13T00:05:00Z' }
				]
			});
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/logs` && request.method() === 'GET') {
			return route.fulfill({ json: [] });
		}
		if (path === '/api/workspaces/test/dev-machine-providers') {
			return route.fulfill({ json: [] });
		}
		if (path === `/api/workspaces/test/agent-runs/${runId}/trace` && request.method() === 'GET') {
			traceRequests++;
			const url = new URL(request.url());
			const eventsAfterId = parseInt(url.searchParams.get('events_after_id') || '0');
			const logsAfterId = parseInt(url.searchParams.get('logs_after_id') || '0');
			lastTraceEventsAfterId = eventsAfterId;
			lastTraceLogsAfterId = logsAfterId;

			const allEvents = [
				{ id: 1, machine_id: machineId, agent_run_id: runId, source: 'agent', event_type: 'agent_run.queued', payload: {}, occurred_at: '2026-07-13T00:00:00Z' },
				{ id: 2, machine_id: machineId, agent_run_id: runId, source: 'agent', event_type: 'agent_run.started', payload: {}, occurred_at: '2026-07-13T00:01:00Z' },
				{ id: 3, machine_id: machineId, agent_run_id: runId, source: 'agent', event_type: 'agent_run.completed', payload: { exit_code: 0 }, occurred_at: '2026-07-13T00:05:00Z' }
			];
			const filteredEvents = eventsAfterId === 0 ? allEvents : allEvents.filter((e) => e.id > eventsAfterId);

			return route.fulfill({
				json: {
					run: { id: runId, machine_id: machineId, provider_id: 'opencode', mode: 'autonomous', status: 'succeeded', prompt: 'Fix the bug', summary: 'Fixed bug in login', changed_files: ['src/login.ts'], commits: ['abc1234'], branch: 'kuayle/trace', pull_request_url: null, tests_run: ['TestLogin'], test_status: 'passed', risk_notes: ['Minor risk'], exit_code: 0, error_message: null, created_at: '2026-07-13T00:00:00Z', started_at: '2026-07-13T00:01:00Z', completed_at: '2026-07-13T00:05:00Z' },
					steps: [{ id: runId, agent_run_id: runId, sequence: 1, step_type: 'shell', name: 'Run tests', status: 'succeeded', command_argv: null, summary: 'All tests passed', exit_code: 0, started_at: '2026-07-13T00:02:00Z', completed_at: '2026-07-13T00:03:00Z', created_at: '2026-07-13T00:02:00Z' }],
					events: filteredEvents,
					logs: [], next_event_id: filteredEvents.at(-1)?.id ?? eventsAfterId, next_log_id: logsAfterId,
					has_more_events: false, has_more_logs: false
				}
			});
		}
		if (path === `/api/workspaces/test/agent-runs/${runId2}/trace` && request.method() === 'GET') {
			traceRequests++;
			return route.fulfill({
				json: {
					run: { id: runId2, machine_id: machineId, provider_id: 'claude-code', mode: 'autonomous', status: 'running', prompt: 'Add tests', summary: null, changed_files: [], commits: [], branch: null, pull_request_url: null, tests_run: [], test_status: 'not_run', risk_notes: [], exit_code: null, error_message: null, created_at: '2026-07-13T01:00:00Z', started_at: '2026-07-13T01:01:00Z', completed_at: null },
					steps: [],
					events: [],
					logs: [], next_event_id: 0, next_log_id: 0, has_more_events: false, has_more_logs: false
				}
			});
		}
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});

	// Test 1: Navigate to machine page and click the agent-run card
	await page.goto('/test/machines');
	await page.getByText('Trace test machine').click();
	await page.waitForURL(`**/machines/${machineId}`);

	// Click the first agent-run card
	await page.getByRole('button', { name: 'View opencode agent run activity' }).click();

	// Verify trace sheet opened using sheet-scoped locators
	await expect(page.getByRole('heading', { name: 'Agent Run Trace' })).toBeVisible();
	// Use sheet content scope to avoid matching the card
	const sheetContent = page.locator('[data-slot="sheet-content"]');
	await expect(sheetContent.getByText('opencode')).toBeVisible();
	await expect(sheetContent.getByText('Fix the bug')).toBeVisible();
	await expect(sheetContent.getByText('Fixed bug in login')).toBeVisible();
	await expect(sheetContent.getByText('src/login.ts')).toBeVisible();
	await expect(sheetContent.getByText('TestLogin')).toBeVisible();
	await expect(sheetContent.getByText('Minor risk')).toBeVisible();

	// Close the sheet via the sheet's close button (data-slot="sheet-close")
	await page.locator('[data-slot="sheet-close"]').click();
	await expect(page.getByRole('heading', { name: 'Agent Run Trace' })).not.toBeVisible();

	// Test 2: Click an activity event with agent_run_id
	const activitySection = page.locator('#activity').first();
	await expect(activitySection.getByText('agent run.queued')).toBeVisible();
	await activitySection.getByText('agent run.queued').click();
	await expect(page.getByRole('heading', { name: 'Agent Run Trace' })).toBeVisible();
	await page.locator('[data-slot="sheet-close"]').click();

	// Test 3: Navigate with deep link
	await page.goto(`/test/machines/${machineId}?agent_run_id=${runId}`);
	// The deep link opens immediately and remains shareable while the sheet is open.
	await expect(page.getByRole('heading', { name: 'Agent Run Trace' })).toBeVisible({ timeout: 10000 });
	await expect(page).toHaveURL(/agent_run_id/);
	await page.locator('[data-slot="sheet-close"]').click();
	await expect(page).not.toHaveURL(/agent_run_id/);

	// Test 4: Cursor polling for active run
	await page.getByRole('button', { name: 'View claude-code agent run activity' }).click();
	await expect(page.getByRole('heading', { name: 'Agent Run Trace' })).toBeVisible();
	const activeSheetContent = page.locator('[data-slot="sheet-content"]');
	await expect(activeSheetContent.getByText('claude-code')).toBeVisible({ timeout: 10000 });

	// Verify multiple trace requests were made (including cursor polling for active run)
	await expect.poll(() => traceRequests).toBeGreaterThan(1);
});
