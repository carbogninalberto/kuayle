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
							max_disk_gb: 100,
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
					max_disk_gb: 100,
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
					max_disk_gb: 100,
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
