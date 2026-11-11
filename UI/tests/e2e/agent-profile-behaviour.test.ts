import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const userId = '00000000-0000-0000-0000-000000000001';

const profile = (id: string, name: string, role: string, behaviour: string) => ({
	id,
	workspace_id: workspaceId,
	name,
	role,
	behaviour,
	harness: 'codex',
	model: '',
	effort: '',
	instructions: '',
	skill_ids: [],
	created_at: '2026-11-01T00:00:00Z',
	updated_at: '2026-11-01T00:00:00Z'
});

// Settings → Agents with two stored profiles. saves records what the page
// sent; refuse answers a PUT with that error code.
async function mockProfiles(
	page: Page,
	options: { canManage?: boolean; refuse?: string } = {},
	saves: Array<{ method: string; body: Record<string, unknown> }> = []
) {
	const profiles = [
		profile('p-architect', 'Architect', 'Staff engineer', 'plans'),
		profile('p-reviewer', 'Reviewer', 'Reviewer', 'reviews')
	];
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
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
				json: {
					id: workspaceId,
					name: 'Test Workspace',
					slug: 'test',
					current_user_role: options.canManage === false ? 'member' : 'owner'
				}
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
					allowed_providers: ['codex'],
					allowed_repositories: [],
					allow_custom_providers: false,
					allow_unattended_automations: false,
					disk_quota_mode: 'hard'
				}
			});
		if (path === '/api/workspaces/test/agent-model-servers') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/agent-model-defaults') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/agent-profiles' && request.method() === 'GET')
			return route.fulfill({
				json: {
					profiles,
					can_manage: options.canManage !== false,
					harnesses: ['codex'],
					model_harnesses: ['codex'],
					model_formats: {},
					behaviours: ['plans', 'changes_code', 'reviews']
				}
			});
		if (path === '/api/workspaces/test/agent-profiles' && request.method() === 'POST') {
			const body = request.postDataJSON();
			saves.push({ method: 'POST', body });
			profiles.push({ ...profile('p-new', '', '', ''), ...body });
			return route.fulfill({ status: 201, json: profiles[profiles.length - 1] });
		}
		if (path.startsWith('/api/workspaces/test/agent-profiles/') && request.method() === 'PUT') {
			const body = request.postDataJSON();
			saves.push({ method: 'PUT', body });
			if (options.refuse)
				return route.fulfill({ status: 409, json: { error: { code: options.refuse, message: 'Refused by the server' } } });
			return route.fulfill({ json: { ...profiles[0], ...body } });
		}
		if (path === '/api/workspaces/test/agent-model-catalog')
			return route.fulfill({
				json: { harness: 'codex', source: '', updated_at: null, stale: false, unavailable: true, models: [] }
			});
		if (path === '/api/workspaces/test/assistant-skills') return route.fulfill({ json: { skills: [], can_manage: true } });
		// As in the other agent settings tests, anything else stays unanswered.
		return null;
	});
}

test('a new profile takes any role and one of three behaviours', async ({ page }) => {
	const saves: Array<{ method: string; body: Record<string, unknown> }> = [];
	await mockProfiles(page, {}, saves);
	await page.goto('/test/settings/agents?profile=new');
	const sheet = page.getByTestId('agent-profile-sheet');
	await expect(sheet).toBeVisible();

	// The role is a plain text field and what the agent does is a choice of three.
	const role = sheet.getByLabel('Role');
	await expect(role).toHaveJSProperty('tagName', 'INPUT');
	await expect(role).toHaveValue('');
	const picker = sheet.getByTestId('profile-behaviour');
	await expect(picker).toHaveAccessibleName('What it does');
	await expect(picker.getByRole('radio')).toHaveText(['Plans', 'Changes code', 'Reviews']);
	await expect(picker.getByRole('radio', { name: 'Changes code' })).toBeChecked();

	// Nothing explains the form beside its fields.
	const agentSection = sheet.locator('section[aria-labelledby="profile-agent"]');
	await expect(agentSection.locator('p')).toHaveCount(0);
	await expect(agentSection.getByRole('button', { name: /info|help/i })).toHaveCount(0);

	await sheet.locator('#profile-name').fill('Planner');
	await role.fill('  Tech lead · payments ');
	// The picker works from the keyboard: arrows move between the options.
	await picker.getByRole('radio', { name: 'Changes code' }).focus();
	await page.keyboard.press('ArrowLeft');
	await page.keyboard.press('Space');
	await expect(picker.getByRole('radio', { name: 'Plans' })).toBeChecked();
	await expect(picker.getByRole('radio', { name: 'Changes code' })).not.toBeChecked();
	// Choosing the chosen option again keeps it: a profile always does something.
	await picker.getByRole('radio', { name: 'Plans' }).click();
	await expect(picker.getByRole('radio', { name: 'Plans' })).toBeChecked();
	await expect(sheet.getByText('Tech lead · payments · Codex')).toBeVisible();

	await sheet.getByRole('button', { name: 'Save' }).click();
	await expect(page.getByText('Profile saved')).toBeVisible();
	expect(saves).toHaveLength(1);
	expect(saves[0].body).toMatchObject({ name: 'Planner', role: 'Tech lead · payments', behaviour: 'plans', harness: 'codex' });

	// The list names each profile's role and what it does.
	const rows = page.getByTestId('agent-profile');
	await expect(rows).toHaveCount(3);
	const planner = rows.filter({ hasText: 'Planner' });
	await expect(planner.getByTestId('agent-profile-role')).toHaveText('Tech lead · payments');
	await expect(planner.getByTestId('agent-profile-behaviour')).toHaveText('Plans');
	const reviewer = rows.filter({ hasText: 'Reviewer' });
	await expect(reviewer.getByTestId('agent-profile-behaviour')).toHaveText('Reviews');
	await expect(reviewer.getByTestId('agent-profile-role')).toHaveCount(0);
});

test('a profile used by workflows keeps its behaviour and says why', async ({ page }) => {
	const saves: Array<{ method: string; body: Record<string, unknown> }> = [];
	await mockProfiles(page, { refuse: 'PROFILE_BEHAVIOUR_IN_USE' }, saves);
	await page.goto('/test/settings/agents?profile=p-architect');
	const sheet = page.getByTestId('agent-profile-sheet');
	const picker = sheet.getByTestId('profile-behaviour');
	await expect(sheet.getByLabel('Role')).toHaveValue('Staff engineer');
	await expect(picker.getByRole('radio', { name: 'Plans' })).toBeChecked();

	await picker.getByRole('radio', { name: 'Reviews' }).click();
	await sheet.getByRole('button', { name: 'Save' }).click();
	await expect(
		page.getByText('Workflows use this profile, so what it does cannot change. Create a new profile instead.')
	).toBeVisible();
	expect(saves[0]).toMatchObject({ method: 'PUT', body: { behaviour: 'reviews', role: 'Staff engineer' } });
	await expect(sheet).toBeVisible();
});

test('a member reads a profile without changing it', async ({ page }) => {
	await mockProfiles(page, { canManage: false });
	await page.goto('/test/settings/agents?profile=p-architect');
	const sheet = page.getByTestId('agent-profile-sheet');
	await expect(sheet.getByLabel('Role')).toBeDisabled();
	for (const option of await sheet.getByTestId('profile-behaviour').getByRole('radio').all())
		await expect(option).toBeDisabled();
	await expect(sheet.getByRole('button', { name: 'Save' })).toHaveCount(0);
});
