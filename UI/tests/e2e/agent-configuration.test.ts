import { expect, test } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const userId = '00000000-0000-0000-0000-000000000001';
const machineId = '00000000-0000-0000-0000-000000000050';
const now = '2026-07-13T00:00:00Z';

async function mockShell(page: import('@playwright/test').Page, role = 'owner') {
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me') {
			return route.fulfill({
				json: { id: userId, email: 'owner@example.com', name: 'Owner', display_name: 'Owner', avatar_url: null }
			});
		}
		if (path === '/api/preferences') {
			return route.fulfill({
				json: {
					font_size: 'default',
					pointer_cursors: true,
					theme_mode: 'dark',
					light_theme: 'light',
					dark_theme: 'dark',
					workflow_sort_mode: 'default',
					workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'],
					team_workflow_sort_overrides: {},
					issues_group_by: 'status'
				}
			});
		}
		if (path === '/api/workspaces')
			return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test') {
			return route.fulfill({
				json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: role }
			});
		}
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`)) {
			return route.fulfill({ json: [] });
		}
		return null;
	});
}

const deepSeek = {
	id: '00000000-0000-0000-0000-000000000070',
	name: 'DeepSeek',
	openai_base_url: 'https://api.deepseek.com',
	anthropic_base_url: 'https://api.deepseek.com/anthropic',
	models: ['deepseek-flash', 'deepseek-v4-pro'],
	default_model: 'deepseek-flash',
	fast_model: '',
	thinking: 'high',
	is_default: true,
	harnesses: ['codex', 'claude-code'],
	thinking_levels: ['off', 'low', 'high', 'max'],
	updated_at: now
};

test('adds a model server from fetched models, edits it without the key and removes it', async ({ page }) => {
	let servers: Array<Record<string, unknown>> = [];
	const saves: Array<{ method: string; body: Record<string, unknown> }> = [];
	const discoveries: Array<Record<string, unknown>> = [];
	let policySaves: Array<Record<string, unknown>> = [];
	await mockShell(page);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/workspaces/test/dev-machine-policy') {
			if (request.method() === 'PATCH') {
				policySaves.push(request.postDataJSON());
				return route.fulfill({
					json: {
						...{
							workspace_id: workspaceId,
							enabled: true,
							max_concurrent_machines: 5,
							max_machines_per_user: 2,
							max_daily_agent_runs: 25,
							max_runtime_minutes: 480,
							max_disk_gb: 100,
							idle_pause_minutes: 240,
							allowed_providers: ['codex'],
							allowed_repositories: [],
							allow_custom_providers: false,
							allow_unattended_automations: false,
							disk_quota_mode: 'hard'
						},
						...request.postDataJSON()
					}
				});
			}
			return route.fulfill({
				json: {
					workspace_id: workspaceId,
					enabled: true,
					max_concurrent_machines: 5,
					max_machines_per_user: 2,
					max_daily_agent_runs: 25,
					max_runtime_minutes: 480,
					max_disk_gb: 100,
					idle_pause_minutes: 240,
					allowed_providers: ['codex'],
					allowed_repositories: [],
					allow_custom_providers: false,
					allow_unattended_automations: false,
					disk_quota_mode: 'hard'
				}
			});
		}
		if (path === '/api/workspaces/test/agent-model-servers/discover') {
			discoveries.push(request.postDataJSON());
			return route.fulfill({ json: { models: ['qwen3-coder:30b', 'gpt-oss-120b'] } });
		}
		if (path === '/api/workspaces/test/agent-model-servers' && request.method() === 'POST') {
			const body = request.postDataJSON();
			saves.push({ method: 'POST', body });
			const created = {
				...body,
				id: 'server-1',
				fast_model: body.fast_model,
				is_default: true,
				harnesses: ['codex'],
				thinking_levels: ['off', 'low', 'high', 'max'],
				updated_at: now
			};
			delete created.api_key;
			servers = [created];
			return route.fulfill({ status: 201, json: created });
		}
		if (path === '/api/workspaces/test/agent-model-servers/server-1') {
			if (request.method() === 'DELETE') {
				servers = [];
				return route.fulfill({ status: 204, body: '' });
			}
			const body = request.postDataJSON();
			saves.push({ method: 'PUT', body });
			servers = [{ ...servers[0], ...body }];
			return route.fulfill({ json: servers[0] });
		}
		if (path === '/api/workspaces/test/agent-model-servers') return route.fulfill({ json: servers });
		return route.fallback();
	});

	await page.goto('/test/settings/agents');
	await expect(page.getByRole('heading', { name: 'Agent configuration', exact: true })).toBeVisible();
	await expect(page.getByText('No model servers yet.', { exact: false })).toBeVisible();
	await page.getByRole('button', { name: 'Add server' }).click();
	const dialog = page.getByRole('dialog');
	await dialog.getByLabel('Name').fill('Local vLLM');
	await dialog.getByLabel('OpenAI-compatible base URL').fill('https://models.example.test/v1');
	await expect(dialog.getByRole('button', { name: 'Fetch models' })).toBeDisabled();
	await dialog.getByLabel('API key').fill('sk-model-secret');
	await dialog.getByRole('button', { name: 'Fetch models' }).click();
	await expect.poll(() => discoveries.length).toBe(1);
	expect(discoveries[0]).toEqual({ openai_base_url: 'https://models.example.test/v1', api_key: 'sk-model-secret' });
	await dialog.getByRole('combobox', { name: 'Default model' }).click();
	await page.getByRole('option', { name: 'gpt-oss-120b' }).click();
	await dialog.getByRole('combobox', { name: 'Thinking' }).click();
	await page.getByRole('option', { name: 'Max' }).click();
	await dialog.getByRole('button', { name: 'Save', exact: true }).click();
	await expect.poll(() => saves.length).toBe(1);
	expect(saves[0].body).toMatchObject({
		name: 'Local vLLM',
		openai_base_url: 'https://models.example.test/v1',
		api_key: 'sk-model-secret',
		models: ['qwen3-coder:30b', 'gpt-oss-120b'],
		default_model: 'gpt-oss-120b',
		thinking: 'max'
	});
	await expect(page.getByText('Local vLLM')).toBeVisible();
	await expect(page.getByText('Default', { exact: true })).toBeVisible();

	await page.getByRole('button', { name: 'Edit Local vLLM' }).click();
	await expect(dialog.getByLabel('API key')).toHaveAttribute('placeholder', 'Stored — leave blank to keep it');
	// The stored key can list models for the saved URL without being re-entered.
	await dialog.getByRole('button', { name: 'Fetch models' }).click();
	await expect.poll(() => discoveries.length).toBe(2);
	expect(discoveries[1]).toEqual({ server_id: 'server-1', openai_base_url: 'https://models.example.test/v1' });
	await dialog.getByRole('button', { name: 'Save', exact: true }).click();
	await expect.poll(() => saves.length).toBe(2);
	expect(saves[1].method).toBe('PUT');
	expect(saves[1].body).not.toHaveProperty('api_key');

	await page.getByRole('switch', { name: 'Allow Claude Code' }).click();
	await expect.poll(() => policySaves.length).toBe(1);
	expect(policySaves[0]).toMatchObject({
		allowed_providers: ['codex', 'claude-code'],
		allow_custom_providers: false,
		max_disk_gb: 100
	});
	expect(policySaves[0]).not.toHaveProperty('workspace_id');

	await page.getByRole('button', { name: 'Remove Local vLLM' }).click();
	await page.getByRole('alertdialog').getByRole('button', { name: 'Delete', exact: true }).click();
	await expect(page.getByText('No model servers yet.', { exact: false })).toBeVisible();
	expect(JSON.stringify(servers)).not.toContain('sk-model-secret');
});

test('members see model servers but cannot change them', async ({ page }) => {
	await mockShell(page, 'member');
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path === '/api/workspaces/test/dev-machine-policy')
			return route.fulfill({
				json: {
					workspace_id: workspaceId,
					enabled: true,
					max_concurrent_machines: 5,
					max_machines_per_user: 2,
					max_daily_agent_runs: 25,
					max_runtime_minutes: 480,
					max_disk_gb: 100,
					idle_pause_minutes: 240,
					allowed_providers: ['codex'],
					allowed_repositories: [],
					allow_custom_providers: false,
					allow_unattended_automations: false,
					disk_quota_mode: 'hard'
				}
			});
		if (path === '/api/workspaces/test/agent-model-servers') return route.fulfill({ json: [deepSeek] });
		return route.fallback();
	});
	await page.goto('/test/settings/agents');
	await expect(page.getByText('deepseek-flash', { exact: true })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Add server' })).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Edit DeepSeek' })).toHaveCount(0);
	await expect(page.getByRole('switch', { name: 'Allow Codex' })).toBeDisabled();
});

test('run dialog sends the model server, model and thinking for Codex', async ({ page }) => {
	const runBodies: Array<Record<string, unknown>> = [];
	await mockShell(page);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const machine = {
			id: machineId,
			workspace_id: workspaceId,
			routing_key: '0123456789abcdef0123',
			name: 'DeepSeek machine',
			status: 'running',
			desired_status: 'running',
			generation: 1,
			repo_owner: 'kuayle',
			repo_name: 'test',
			base_branch: 'main',
			working_branch: 'kuayle/test',
			machine_size: 'medium',
			cpu_millis: 4000,
			memory_mb: 8192,
			disk_gb: 20,
			pids_limit: 512,
			max_runtime_minutes: 240,
			keep_running: false,
			environment_builder: false,
			created_at: now,
			updated_at: now,
			expires_at: '2099-07-13T04:00:00Z'
		};
		if (path === '/api/workspaces/test/dev-machines') {
			return route.fulfill({ json: { data: [machine], total_count: 1, page: 1, has_more: false } });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}`) return route.fulfill({ json: machine });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/services`) {
			return route.fulfill({
				json: [
					{
						id: '00000000-0000-0000-0000-000000000051',
						machine_id: machineId,
						service_type: 'ide',
						service_key: 'ide',
						container_name: 'ide',
						image_ref: 'kuayle/ide:test',
						status: 'running',
						health_status: 'healthy'
					}
				]
			});
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/providers`) {
			return route.fulfill({
				json: [
					{
						id: 'codex',
						display_name: 'Codex',
						default_image: 'kuayle/codex:test',
						required_secrets: [],
						supported_modes: ['autonomous'],
						custom: false
					}
				]
			});
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/checkouts`) {
			return route.fulfill({
				json: [
					{
						id: '00000000-0000-0000-0000-000000000052',
						workspace_id: workspaceId,
						machine_id: machineId,
						issue_id: '00000000-0000-0000-0000-000000000053',
						github_repo_id: '00000000-0000-0000-0000-000000000054',
						repository_full_name: 'kuayle/test',
						base_branch: 'main',
						working_branch: 'kuayle/deepseek',
						workspace_path: '/workspace/tasks/deepseek',
						status: 'ready',
						created_at: now,
						updated_at: now
					}
				]
			});
		}
		if (
			path === `/api/workspaces/test/dev-machines/${machineId}/events` ||
			path === `/api/workspaces/test/dev-machines/${machineId}/logs` ||
			path === `/api/workspaces/test/dev-machines/${machineId}/resource-usage`
		) {
			return route.fulfill({ json: [] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/agent-runs` && request.method() === 'POST') {
			const payload = request.postDataJSON() as Record<string, unknown>;
			runBodies.push(payload);
			return route.fulfill({
				status: 202,
				json: {
					id: '00000000-0000-0000-0000-000000000060',
					machine_id: machineId,
					workspace_id: workspaceId,
					provider_id: 'codex',
					mode: 'autonomous',
					status: 'queued',
					prompt: String(payload.prompt),
					max_runtime_seconds: Number(payload.max_runtime_seconds),
					push_branch: false,
					open_pull_request: false,
					created_at: now
				}
			});
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/agent-runs`) {
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		}
		if (path === '/api/workspaces/test/dev-machine-policy') {
			return route.fulfill({
				json: {
					workspace_id: workspaceId,
					enabled: true,
					max_concurrent_machines: 5,
					max_machines_per_user: 2,
					max_daily_agent_runs: 25,
					max_runtime_minutes: 480,
					max_disk_gb: 100,
					idle_pause_minutes: 240,
					allowed_providers: ['codex'],
					allowed_repositories: [],
					allow_custom_providers: false,
					disk_quota_mode: 'hard'
				}
			});
		}
		if (path === '/api/workspaces/test/agent-model-servers') return route.fulfill({ json: [deepSeek] });
		return route.fallback();
	});

	await page.goto(`/test/machines/${machineId}`);
	await page.getByRole('button', { name: 'Run agent' }).click();
	const dialog = page.getByRole('dialog');
	await expect(dialog.getByRole('heading', { name: 'Run Agent' })).toBeVisible();
	await dialog.getByRole('combobox', { name: 'Model', exact: true }).click();
	await page.getByRole('option', { name: 'deepseek-v4-pro' }).click();
	await dialog.getByRole('combobox', { name: 'Effort' }).click();
	await page.getByRole('option', { name: 'Low' }).click();
	await dialog.getByLabel('Prompt and context').fill('Implement the change');
	await dialog.getByRole('button', { name: 'Run Agent', exact: true }).click();

	await expect.poll(() => runBodies.length).toBe(1);
	expect(runBodies[0]).toMatchObject({
		provider: 'codex',
		model_server_id: deepSeek.id,
		model: 'deepseek-v4-pro',
		thinking: 'low'
	});
});
