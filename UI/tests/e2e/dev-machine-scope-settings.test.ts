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

test('keeps project and team development settings scoped during rapid navigation', async ({ page }) => {
	const workspaceId = '00000000-0000-0000-0000-000000000002';
	const teamAId = '00000000-0000-0000-0000-000000000010';
	const teamBId = '00000000-0000-0000-0000-000000000011';
	const projectAId = '00000000-0000-0000-0000-000000000020';
	const projectBId = '00000000-0000-0000-0000-000000000021';
	const repoAId = '00000000-0000-0000-0000-000000000030';
	const repoBId = '00000000-0000-0000-0000-000000000031';
	const environmentAId = '00000000-0000-0000-0000-000000000040';
	const environmentBId = '00000000-0000-0000-0000-000000000041';
	const now = '2026-07-23T00:00:00Z';
	const teams = [
		{ id: teamAId, name: 'Team Alpha', key: 'ALPHA', description: null, color: '#3b82f6', icon: 'SquareUser', triage_enabled: false, parent_auto_close_enabled: false, sub_issue_auto_close_enabled: false, issue_copy_prompt: null, created_at: now, updated_at: now },
		{ id: teamBId, name: 'Team Beta', key: 'BETA', description: null, color: '#22c55e', icon: 'SquareUser', triage_enabled: false, parent_auto_close_enabled: false, sub_issue_auto_close_enabled: false, issue_copy_prompt: null, created_at: now, updated_at: now }
	];
	const projects = [
		{ id: projectAId, name: 'Project Alpha', description: null, status: 'planned', team_id: teamAId, lead_id: null, start_date: null, target_date: null, sort_order: 1, created_at: now, updated_at: now },
		{ id: projectBId, name: 'Project Beta', description: null, status: 'planned', team_id: teamBId, lead_id: null, start_date: null, target_date: null, sort_order: 2, created_at: now, updated_at: now }
	];
	const repositories = [
		{ id: repoAId, github_repo_id: 1001, full_name: 'kuayle/repo-alpha', default_branch: 'main', is_active: true },
		{ id: repoBId, github_repo_id: 1002, full_name: 'kuayle/repo-beta', default_branch: 'develop', is_active: true }
	];
	const environments = [
		{ id: environmentAId, workspace_id: workspaceId, name: 'Alpha environment', status: 'ready', image_ref: 'alpha:latest', created_at: now, updated_at: now },
		{ id: environmentBId, workspace_id: workspaceId, name: 'Beta environment', status: 'ready', image_ref: 'beta:latest', created_at: now, updated_at: now }
	];
	const scopeDelays = new Map<string, ReturnType<typeof createRequestDelay>>();
	const savedSettings: Record<string, unknown>[] = [];
	let deleteRequests = 0;
	let workspaceRole = 'owner';

	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		const path = url.pathname;
		if (path === '/api/auth/me') return route.fulfill({ json: { id: '00000000-0000-0000-0000-000000000001', email: 'owner@example.com', name: 'Owner', display_name: 'Owner', avatar_url: null } });
		if (path === '/api/preferences') return route.fulfill({ json: { font_size: 'default', pointer_cursors: true, theme_mode: 'dark', light_theme: 'light', dark_theme: 'dark', workflow_sort_mode: 'default', workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'], team_workflow_sort_overrides: {}, issues_group_by: 'status' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: workspaceRole } });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: teams });
		if (path === '/api/workspaces/test/projects') return route.fulfill({ json: projects });
		if (path === `/api/workspaces/test/projects/${projectAId}`) return route.fulfill({ json: projects[0] });
		if (path === `/api/workspaces/test/projects/${projectBId}`) return route.fulfill({ json: projects[1] });
		if (path === '/api/workspaces/test/issues') return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path === `/api/workspaces/test/teams/${teamAId}/cycles` || path === `/api/workspaces/test/teams/${teamBId}/cycles`) return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/github/status') return route.fulfill({ json: { configured: true, installed: true, global_app: false, repos: repositories } });
		if (path === '/api/workspaces/test/dev-machine-environments') return route.fulfill({ json: environments });
		if (path === '/api/workspaces/test/dev-machine-scope-setting' && request.method() === 'GET') {
			const scopeType = url.searchParams.get('scope_type');
			const scopeId = url.searchParams.get('scope_id') ?? '';
			const delay = scopeDelays.get(scopeId);
			if (delay) {
				scopeDelays.delete(scopeId);
				delay.markStarted();
				await delay.released;
			}
			const useBeta = scopeId === teamBId || scopeId === projectBId;
			await route.fulfill({ json: {
				workspace_id: workspaceId,
				team_id: scopeType === 'team' ? scopeId : undefined,
				project_id: scopeType === 'project' ? scopeId : undefined,
				github_repo_id: useBeta ? repoBId : repoAId,
				environment_id: useBeta ? environmentBId : environmentAId
			} });
			delay?.markCompleted();
			return;
		}
		if (path === '/api/workspaces/test/dev-machine-scope-setting' && request.method() === 'PUT') {
			savedSettings.push(request.postDataJSON());
			return route.fulfill({ json: request.postDataJSON() });
		}
		if (path === '/api/workspaces/test/dev-machine-scope-setting' && request.method() === 'DELETE') {
			deleteRequests += 1;
			return route.fulfill({ status: 204, body: '' });
		}
		if (['labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`)) return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});

	const teamADelay = createRequestDelay();
	const teamBDelay = createRequestDelay();
	scopeDelays.set(teamAId, teamADelay);
	scopeDelays.set(teamBId, teamBDelay);
	await page.goto(`/test/settings/teams/${teamAId}`);
	await teamADelay.started;
	await page.evaluate((href) => {
		const link = document.createElement('a');
		link.href = href;
		document.body.append(link);
		link.click();
		link.remove();
	}, `/test/settings/teams/${teamBId}`);
	await expect(page).toHaveURL(new RegExp(`/test/settings/teams/${teamBId}$`));
	await teamBDelay.started;
	const teamSave = page.getByRole('button', { name: 'Save development defaults' });
	await expect(teamSave).toBeDisabled();
	teamBDelay.release();
	await teamBDelay.completed;
	await expect(page.getByText('kuayle/repo-beta', { exact: true })).toBeVisible();
	await expect(page.getByText('Beta environment', { exact: true })).toBeVisible();
	await expect(teamSave).toBeEnabled();
	teamADelay.release();
	await teamADelay.completed;
	await page.waitForTimeout(50);
	await expect(page.getByText('kuayle/repo-beta', { exact: true })).toBeVisible();
	await expect(page.getByText('kuayle/repo-alpha', { exact: true })).toHaveCount(0);
	await teamSave.click();
	await expect.poll(() => savedSettings.length).toBe(1);
	expect(savedSettings[0]).toMatchObject({ scope_type: 'team', scope_id: teamBId, github_repo_id: repoBId, environment_id: environmentBId });

	const projectADelay = createRequestDelay();
	const projectBDelay = createRequestDelay();
	scopeDelays.set(projectAId, projectADelay);
	scopeDelays.set(projectBId, projectBDelay);
	await page.goto(`/test/projects/${projectAId}`);
	await projectADelay.started;
	await page.evaluate((href) => {
		const link = document.createElement('a');
		link.href = href;
		document.body.append(link);
		link.click();
		link.remove();
	}, `/test/projects/${projectBId}`);
	await expect(page).toHaveURL(new RegExp(`/test/projects/${projectBId}$`));
	await projectBDelay.started;
	const projectSettings = page.getByTitle('Loading development settings');
	await expect(projectSettings).toBeDisabled();
	projectBDelay.release();
	await projectBDelay.completed;
	await page.getByTitle('Development settings').click();
	const projectDialog = page.getByRole('dialog');
	await expect(projectDialog.getByText('kuayle/repo-beta', { exact: true })).toBeVisible();
	await expect(projectDialog.getByText('Beta environment', { exact: true })).toBeVisible();
	projectADelay.release();
	await projectADelay.completed;
	await page.waitForTimeout(50);
	await expect(projectDialog.getByText('kuayle/repo-beta', { exact: true })).toBeVisible();
	await expect(projectDialog.getByText('kuayle/repo-alpha', { exact: true })).toHaveCount(0);
	await projectDialog.getByRole('button', { name: 'Save', exact: true }).click();
	await expect.poll(() => savedSettings.length).toBe(2);
	expect(savedSettings[1]).toMatchObject({ scope_type: 'project', scope_id: projectBId, github_repo_id: repoBId, environment_id: environmentBId });
	expect(deleteRequests).toBe(0);

	workspaceRole = 'member';
	await page.goto(`/test/projects/${projectAId}`);
	const readOnlySettings = page.getByTitle('View development settings');
	await expect(readOnlySettings).toBeEnabled();
	await readOnlySettings.click();
	const readOnlyDialog = page.getByRole('dialog');
	await expect(readOnlyDialog.getByText('Workspace owners and admins manage project development defaults.')).toBeVisible();
	await expect(readOnlyDialog.getByText('kuayle/repo-alpha', { exact: true })).toBeVisible();
	await expect(readOnlyDialog.getByText('Alpha environment', { exact: true })).toBeVisible();
	await expect(readOnlyDialog.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
	expect(savedSettings).toHaveLength(2);
	expect(deleteRequests).toBe(0);
});
