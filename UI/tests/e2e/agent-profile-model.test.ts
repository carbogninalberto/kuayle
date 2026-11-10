import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const userId = '00000000-0000-0000-0000-000000000001';
const levels = ['off', 'low', 'medium', 'high', 'xhigh', 'max'];

const goPlan = {
	harness: 'opencode',
	source: 'opencode-go',
	updated_at: '2026-07-13T00:00:00Z',
	stale: false,
	unavailable: false,
	models: [
		{ id: 'opencode-go/deepseek-v4.1-flash', name: 'DeepSeek V4.1 Flash', efforts: ['off', 'low', 'high', 'max'] },
		{ id: 'opencode-go/glm-6', name: 'GLM 6', efforts: ['off', 'high'] },
		{ id: 'opencode-go/kimi-k3', name: 'Kimi K3', efforts: [] }
	]
};

// The agent settings page with a profile sheet open on a new profile. catalog
// answers GET /agent-model-catalog; saves and catalogRequests record what the
// page sent.
async function mockProfiles(
	page: Page,
	catalog: (harness: string, refresh: boolean) => { status?: number; json: unknown },
	saves: Array<Record<string, unknown>>,
	catalogRequests: string[]
) {
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		const path = url.pathname;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: userId, email: 'owner@example.com', name: 'Owner', display_name: 'Owner', avatar_url: null }
			});
		if (path === '/api/preferences')
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
		if (path === '/api/workspaces')
			return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test')
			return route.fulfill({
				json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'owner' }
			});
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`))
			return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/dev-machine-policy')
			return route.fulfill({
				json: {
					workspace_id: workspaceId,
					enabled: true,
					max_concurrent_machines: 5,
					max_machines_per_user: 2,
					max_daily_agent_runs: 25,
					max_disk_gb: 100,
					allowed_providers: ['opencode', 'codex'],
					allowed_repositories: [],
					allow_custom_providers: false,
					allow_unattended_automations: false,
					disk_quota_mode: 'hard'
				}
			});
		if (path === '/api/workspaces/test/agent-model-servers') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/agent-model-defaults')
			return route.fulfill({
				json: ['codex', 'claude-code'].map((provider) => ({ provider, model: '', thinking: '', thinking_levels: levels }))
			});
		if (path === '/api/workspaces/test/agent-profiles' && request.method() === 'GET')
			return route.fulfill({
				json: {
					profiles: [],
					can_manage: true,
					harnesses: ['opencode', 'codex'],
					model_harnesses: ['opencode', 'codex'],
					model_formats: { opencode: { format: 'provider/model', example: 'opencode-go/deepseek-v4.1-flash' } },
					roles: ['implementer', 'reviewer']
				}
			});
		if (path === '/api/workspaces/test/agent-profiles' && request.method() === 'POST') {
			const body = request.postDataJSON();
			saves.push(body);
			return route.fulfill({ status: 201, json: { id: 'profile-1', ...body, skill_ids: [], version: 1 } });
		}
		if (path === '/api/workspaces/test/agent-model-catalog') {
			catalogRequests.push(url.search);
			const answer = catalog(url.searchParams.get('harness') ?? '', url.searchParams.get('refresh') === 'true');
			return route.fulfill({ status: answer.status ?? 200, json: answer.json });
		}
		if (path === '/api/workspaces/test/assistant-skills') return route.fulfill({ json: { skills: [], can_manage: true } });
		// As in the other agent settings tests, anything else stays unanswered.
		return null;
	});
}

test('the model field suggests the catalog, limits the effort and still takes any typed model', async ({ page }) => {
	const saves: Array<Record<string, unknown>> = [];
	const requests: string[] = [];
	await mockProfiles(page, () => ({ json: goPlan }), saves, requests);
	await page.goto('/test/settings/agents?profile=new');
	const sheet = page.getByTestId('agent-profile-sheet');
	await expect(sheet).toBeVisible();
	await sheet.locator('#profile-name').fill('Cheap');

	// Any level can be chosen before a model is.
	await sheet.locator('#profile-effort').click();
	await page.getByRole('option', { name: 'Medium' }).click();

	// Suggestions are filtered by name as well as id, and chosen with the keyboard.
	const model = sheet.locator('#profile-model');
	await expect(model).toHaveText('Agent default');
	await model.click();
	const search = page.getByPlaceholder('Search or type a model');
	await expect(search).toBeFocused();
	await expect(page.getByRole('option')).toHaveCount(3);
	await search.fill('glm');
	await expect(page.getByRole('option')).toHaveCount(2);
	await expect(page.getByRole('option').first()).toContainText('opencode-go/glm-6');
	await expect(page.getByRole('option').last()).toHaveText('Use “glm”');
	await search.press('Enter');
	await expect(model).toHaveText('opencode-go/glm-6');
	await expect(model).toBeFocused();

	// GLM 6 allows off and high only: medium goes back to the default, and says so.
	await expect(sheet.getByTestId('profile-effort-reset')).toHaveText(
		'opencode-go/glm-6 does not allow the effort Medium; it was reset to the agent default.'
	);
	await sheet.locator('#profile-effort').click();
	await expect(page.getByRole('option')).toHaveText(['Agent default', 'Off', 'High']);
	await page.getByRole('option', { name: 'High' }).click();
	await expect(sheet.getByTestId('profile-effort-reset')).toHaveCount(0);

	// A model the list does not have is used exactly as typed, with every level.
	await model.click();
	await search.fill('anthropic/claude-sonnet-5');
	await expect(page.getByRole('option')).toHaveText(['Use “anthropic/claude-sonnet-5”']);
	await search.press('Enter');
	await expect(model).toHaveText('anthropic/claude-sonnet-5');
	await sheet.locator('#profile-effort').click();
	await expect(page.getByRole('option')).toHaveCount(7);
	await page.keyboard.press('Escape');

	await sheet.getByRole('button', { name: 'Save' }).click();
	await expect.poll(() => saves.length).toBe(1);
	expect(saves[0]).toMatchObject({ name: 'Cheap', harness: 'opencode', model: 'anthropic/claude-sonnet-5', effort: 'high' });
	expect(requests).toEqual(['?harness=opencode']);
});

test('an unavailable list leaves the field usable and can be refreshed', async ({ page }) => {
	const saves: Array<Record<string, unknown>> = [];
	const requests: string[] = [];
	let reachable = false;
	await mockProfiles(
		page,
		(harness) => {
			if (harness === 'codex')
				return {
					json: { harness, source: 'workspace', stale: false, unavailable: false, models: [{ id: 'gpt-5.5', name: 'gpt-5.5', efforts: [] }] }
				};
			return reachable
				? { json: { ...goPlan, stale: true } }
				: { json: { harness, source: 'opencode-go', stale: false, unavailable: true, models: [] } };
		},
		saves,
		requests
	);
	await page.goto('/test/settings/agents?profile=new');
	const sheet = page.getByTestId('agent-profile-sheet');
	await sheet.locator('#profile-name').fill('Offline');
	const model = sheet.locator('#profile-model');
	await model.click();
	const status = page.getByTestId('model-combobox-status');
	await expect(status).toHaveText('The model list is unavailable. You can still type a model.');
	await expect(page.getByRole('option')).toHaveCount(0);

	// Refresh asks the server again; a list it could not update is shown as such.
	reachable = true;
	await page.getByRole('button', { name: 'Refresh' }).click();
	await expect(status).toHaveText('The model list could not be updated; this is the last one read.');
	await expect(page.getByRole('option')).toHaveCount(3);
	expect(requests).toEqual(['?harness=opencode', '?harness=opencode&refresh=true']);

	const search = page.getByPlaceholder('Search or type a model');
	await search.fill('opencode-go/brand-new');
	await search.press('Enter');
	await expect(model).toHaveText('opencode-go/brand-new');
	await sheet.getByRole('button', { name: 'Save' }).click();
	await expect.poll(() => saves.length).toBe(1);
	expect(saves[0]).toMatchObject({ model: 'opencode-go/brand-new', effort: '' });
});

test('changing the coding agent loads that agent’s suggestions', async ({ page }) => {
	const requests: string[] = [];
	await mockProfiles(
		page,
		(harness) =>
			harness === 'codex'
				? { json: { harness, source: 'workspace', stale: false, unavailable: false, models: [{ id: 'gpt-5.5', name: 'gpt-5.5', efforts: [] }] } }
				: { status: 500, json: { error: { code: 'INTERNAL', message: 'boom' } } },
		[],
		requests
	);
	await page.goto('/test/settings/agents?profile=new');
	const sheet = page.getByTestId('agent-profile-sheet');
	const model = sheet.locator('#profile-model');
	// The request itself failing is the same to the user as an unavailable list.
	await model.click();
	await expect(page.getByTestId('model-combobox-status')).toHaveText(
		'The model list is unavailable. You can still type a model.'
	);
	await page.keyboard.press('Escape');

	await sheet.locator('#profile-harness').click();
	await page.getByRole('option', { name: 'Codex' }).click();
	await model.click();
	await expect(page.getByRole('option')).toHaveText(['gpt-5.5']);
	await expect(page.getByTestId('model-combobox-status')).toHaveText('');
	expect(requests).toEqual(['?harness=opencode', '?harness=codex']);
});
