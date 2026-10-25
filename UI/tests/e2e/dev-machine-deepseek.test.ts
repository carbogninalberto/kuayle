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
			return route.fulfill({ json: { id: userId, email: 'owner@example.com', name: 'Owner', display_name: 'Owner', avatar_url: null } });
		}
		if (path === '/api/preferences') {
			return route.fulfill({
				json: {
					font_size: 'default', pointer_cursors: true, theme_mode: 'dark', light_theme: 'light',
					dark_theme: 'dark', workflow_sort_mode: 'default',
					workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'],
					team_workflow_sort_overrides: {}, issues_group_by: 'status'
				}
			});
		}
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test') {
			return route.fulfill({ json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: role } });
		}
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`)) {
			return route.fulfill({ json: [] });
		}
		return null;
	});
}

test('saves, updates without a key, and removes the workspace DeepSeek credential', async ({ page }) => {
	let configured = false;
	let model = 'deepseek-flash';
	let thinking = 'high';
	const updates: Array<Record<string, unknown>> = [];
	let deletes = 0;
	await mockShell(page);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/workspaces/test/dev-machine-policy') {
			return route.fulfill({
				json: {
					workspace_id: workspaceId, enabled: true, max_concurrent_machines: 5, max_machines_per_user: 2,
					max_daily_agent_runs: 25, max_runtime_minutes: 480, max_disk_gb: 100, idle_pause_minutes: 240,
					allowed_providers: ['codex'], allowed_repositories: [], allow_custom_providers: false,
					disk_quota_mode: 'hard'
				}
			});
		}
		if (path === '/api/workspaces/test/github/status') {
			return route.fulfill({ json: { configured: true, installed: true, global_app: false, repos: [] } });
		}
		if (path === '/api/workspaces/test/dev-machine-scope-setting') return route.fulfill({ json: { workspace_id: workspaceId } });
		if (path === '/api/workspaces/test/dev-machine-environments') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/dev-machine-model-providers/deepseek') {
			if (request.method() === 'PUT') {
				const payload = request.postDataJSON() as Record<string, unknown>;
				updates.push(payload);
				configured = true;
				model = String(payload.default_model);
				thinking = String(payload.thinking);
			}
			if (request.method() === 'DELETE') {
				deletes += 1;
				configured = false;
				model = 'deepseek-flash';
				thinking = 'high';
				return route.fulfill({ status: 204, body: '' });
			}
			return route.fulfill({
				json: {
					provider: 'deepseek', configured, default_model: model, thinking,
					models: ['deepseek-flash', 'deepseek-v4-pro'], thinking_levels: ['off', 'low', 'high', 'max'],
					updated_at: configured ? now : null
				}
			});
		}
		return route.fallback();
	});

	await page.goto('/test/settings/dev-machines');
	await expect(page.getByRole('heading', { name: 'DeepSeek', exact: true })).toBeVisible();
	await page.getByLabel('DeepSeek API key').fill('sk-deepseek-secret');
	await page.getByRole('combobox', { name: 'Default model' }).click();
	await page.getByRole('option', { name: 'deepseek-v4-pro' }).click();
	await page.getByRole('combobox', { name: 'Thinking level' }).click();
	await page.getByRole('option', { name: 'Max' }).click();
	await page.getByRole('button', { name: 'Save DeepSeek' }).click();
	await expect.poll(() => updates.length).toBe(1);
	expect(updates[0]).toMatchObject({
		api_key: 'sk-deepseek-secret', default_model: 'deepseek-v4-pro', thinking: 'max'
	});
	await expect(page.getByLabel('DeepSeek API key')).toHaveAttribute('placeholder', 'Stored');

	await page.getByRole('combobox', { name: 'Default model' }).click();
	await page.getByRole('option', { name: 'deepseek-flash' }).click();
	await page.getByRole('button', { name: 'Save DeepSeek' }).click();
	await expect.poll(() => updates.length).toBe(2);
	expect(updates[1]).toEqual({ default_model: 'deepseek-flash', thinking: 'max' });

	await page.getByRole('button', { name: 'Remove', exact: true }).click();
	await page.getByRole('alertdialog').getByRole('button', { name: 'Remove', exact: true }).click();
	await expect.poll(() => deletes).toBe(1);
	await expect(page.getByRole('button', { name: 'Save DeepSeek' })).toBeDisabled();
});

test('keeps the policy usable when the DeepSeek configuration fails to load', async ({ page }) => {
	let deepSeekLoads = 0;
	await mockShell(page);
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path === '/api/workspaces/test/dev-machine-policy') {
			return route.fulfill({
				json: {
					workspace_id: workspaceId, enabled: true, max_concurrent_machines: 5, max_machines_per_user: 2,
					max_daily_agent_runs: 25, max_runtime_minutes: 480, max_disk_gb: 100, idle_pause_minutes: 240,
					allowed_providers: ['codex'], allowed_repositories: [], allow_custom_providers: false,
					disk_quota_mode: 'hard'
				}
			});
		}
		if (path === '/api/workspaces/test/github/status') {
			return route.fulfill({ json: { configured: true, installed: true, global_app: false, repos: [] } });
		}
		if (path === '/api/workspaces/test/dev-machine-scope-setting') return route.fulfill({ json: { workspace_id: workspaceId } });
		if (path === '/api/workspaces/test/dev-machine-environments') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/dev-machine-model-providers/deepseek') {
			deepSeekLoads += 1;
			if (deepSeekLoads === 1) return route.fulfill({ status: 500, json: { error: 'boom' } });
			return route.fulfill({
				json: {
					provider: 'deepseek', configured: false, default_model: 'deepseek-flash', thinking: 'high',
					models: ['deepseek-flash', 'deepseek-v4-pro'], thinking_levels: ['off', 'low', 'high', 'max'], updated_at: null
				}
			});
		}
		return route.fallback();
	});

	await page.goto('/test/settings/dev-machines');
	await expect(page.getByRole('heading', { name: 'Dev Machines', exact: true })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Save policy' })).toBeVisible();
	await expect(page.getByText('Unable to load the DeepSeek configuration')).toBeVisible();
	await expect(page.getByLabel('DeepSeek API key')).toHaveCount(0);

	await page.getByRole('button', { name: 'Retry DeepSeek' }).click();
	await expect(page.getByLabel('DeepSeek API key')).toBeVisible();
});

test('run dialog sends DeepSeek model and thinking for Codex', async ({ page }) => {
	const runBodies: Array<Record<string, unknown>> = [];
	await mockShell(page);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const machine = {
			id: machineId, workspace_id: workspaceId, routing_key: '0123456789abcdef0123',
			name: 'DeepSeek machine', status: 'running', desired_status: 'running', generation: 1,
			repo_owner: 'kuayle', repo_name: 'test', base_branch: 'main', working_branch: 'kuayle/test',
			machine_size: 'medium', cpu_millis: 4000, memory_mb: 8192, disk_gb: 20, pids_limit: 512,
			max_runtime_minutes: 240, keep_running: false, environment_builder: false,
			created_at: now, updated_at: now, expires_at: '2099-07-13T04:00:00Z'
		};
		if (path === '/api/workspaces/test/dev-machines') {
			return route.fulfill({ json: { data: [machine], total_count: 1, page: 1, has_more: false } });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}`) return route.fulfill({ json: machine });
		if (path === `/api/workspaces/test/dev-machines/${machineId}/services`) {
			return route.fulfill({ json: [{ id: '00000000-0000-0000-0000-000000000051', machine_id: machineId, service_type: 'ide', service_key: 'ide', container_name: 'ide', image_ref: 'kuayle/ide:test', status: 'running', health_status: 'healthy' }] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/providers`) {
			return route.fulfill({ json: [{ id: 'codex', display_name: 'Codex', default_image: 'kuayle/codex:test', required_secrets: [], supported_modes: ['autonomous'], custom: false }] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/checkouts`) {
			return route.fulfill({
				json: [{
					id: '00000000-0000-0000-0000-000000000052', workspace_id: workspaceId, machine_id: machineId,
					issue_id: '00000000-0000-0000-0000-000000000053', github_repo_id: '00000000-0000-0000-0000-000000000054',
					repository_full_name: 'kuayle/test', base_branch: 'main', working_branch: 'kuayle/deepseek',
					workspace_path: '/workspace/tasks/deepseek', status: 'ready', created_at: now, updated_at: now
				}]
			});
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/events` || path === `/api/workspaces/test/dev-machines/${machineId}/logs` || path === `/api/workspaces/test/dev-machines/${machineId}/resource-usage`) {
			return route.fulfill({ json: [] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/agent-runs` && request.method() === 'POST') {
			const payload = request.postDataJSON() as Record<string, unknown>;
			runBodies.push(payload);
			return route.fulfill({
				status: 202,
				json: {
					id: '00000000-0000-0000-0000-000000000060', machine_id: machineId, workspace_id: workspaceId,
					provider_id: 'codex', mode: 'autonomous', status: 'queued', prompt: String(payload.prompt),
					max_runtime_seconds: Number(payload.max_runtime_seconds), push_branch: false, open_pull_request: false,
					created_at: now
				}
			});
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/agent-runs`) {
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		}
		if (path === '/api/workspaces/test/dev-machine-policy') {
			return route.fulfill({ json: { workspace_id: workspaceId, enabled: true, max_concurrent_machines: 5, max_machines_per_user: 2, max_daily_agent_runs: 25, max_runtime_minutes: 480, max_disk_gb: 100, idle_pause_minutes: 240, allowed_providers: ['codex'], allowed_repositories: [], allow_custom_providers: false, disk_quota_mode: 'hard' } });
		}
		if (path === '/api/workspaces/test/dev-machine-model-providers/deepseek') {
			return route.fulfill({
				json: {
					provider: 'deepseek', configured: true, default_model: 'deepseek-flash', thinking: 'high',
					models: ['deepseek-flash', 'deepseek-v4-pro'], thinking_levels: ['off', 'low', 'high', 'max'], updated_at: now
				}
			});
		}
		return route.fallback();
	});

	await page.goto(`/test/machines/${machineId}`);
	await page.getByRole('button', { name: 'Run agent' }).click();
	const dialog = page.getByRole('dialog');
	await expect(dialog.getByRole('heading', { name: 'Run Agent' })).toBeVisible();
	await dialog.getByRole('combobox', { name: 'DeepSeek model' }).click();
	await page.getByRole('option', { name: 'deepseek-v4-pro' }).click();
	await dialog.getByRole('combobox', { name: 'DeepSeek thinking' }).click();
	await page.getByRole('option', { name: 'Low' }).click();
	await dialog.getByLabel('Prompt and context').fill('Implement the change');
	await dialog.getByRole('button', { name: 'Run Agent', exact: true }).click();

	await expect.poll(() => runBodies.length).toBe(1);
	expect(runBodies[0]).toMatchObject({ provider: 'codex', model: 'deepseek-v4-pro', thinking: 'low' });
});
