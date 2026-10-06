import { test, expect, type Page, type WebSocketRoute } from '@playwright/test';

const userId = '00000000-0000-0000-0000-000000000001';
const teamId = '00000000-0000-0000-0000-000000000010';
const secondTeamId = '00000000-0000-0000-0000-000000000011';
const statusId = '00000000-0000-0000-0000-000000000020';
const teamPath = `/test/teams/${teamId}`;
const createdAt = '2026-01-01T00:00:00Z';

async function mockWorkspace(page: Page) {
 let revoked = false;
 let isPrivate = true;
 const visibilityWrites: unknown[] = [];
 let teamMembers: {user_id: string; team_id: string}[] = [];
	let socket: WebSocketRoute | undefined;
	let workspaceRevoked = false;
	await page.routeWebSocket('**/ws', ws => { socket = ws; });
	const unhandledPaths: string[] = [];
	const pageErrors: string[] = [];
	page.on('pageerror', (error) => pageErrors.push(error.message));
	const user = {
		id: userId,
		email: 'test@example.com',
		name: 'Test User',
		display_name: 'Test User',
		avatar_url: null
	};
	const workspace = {
		id: '00000000-0000-0000-0000-000000000002',
		name: 'Test Workspace',
  current_user_role: 'owner',
  privacy_enabled: true,
		slug: 'test',
		logo_url: null,
		created_at: createdAt,
		updated_at: createdAt
	};
	const teams = [teamId, secondTeamId].map((id, index) => ({
		id,
		name: index === 0 ? 'Engineering' : 'Design',
		key: index === 0 ? 'ENG' : 'DES',
		description: null,
		color: '#6366f1',
		icon: 'layers',
		is_private: index === 0,
  triage_enabled: false,
		parent_auto_close_enabled: false,
		sub_issue_auto_close_enabled: false,
		issue_copy_prompt: null,
		created_at: createdAt,
		updated_at: createdAt
	}));
	const issues = [1, 2, 3].map((number) => ({
		id: `00000000-0000-0000-0000-${String(100 + number).padStart(12, '0')}`,
		identifier: `ENG-${number}`,
		title: `Navigation issue ${number}`,
		description: null,
		status: 'todo',
		status_id: statusId,
		status_info: { id: statusId, name: 'Todo', category: 'unstarted', color: '#6366f1', position: 0 },
		priority: 3,
		team_id: teamId,
		project_id: null,
		cycle_id: null,
		creator_id: userId,
		assignee_id: userId,
		creator: user,
		assignee: user,
		assignees: [user],
		parent_id: null,
		due_date: null,
		sort_order: number * 1000,
		labels: [],
		sub_issue_count: 0,
		sub_issue_done: 0,
		created_at: createdAt,
		updated_at: createdAt
	}));
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const url = new URL(route.request().url());
		const path = url.pathname;
		if (workspaceRevoked && path === '/api/workspaces/test') return route.fulfill({status:403,json:{error:{message:'Access removed'}}});
		if (workspaceRevoked && path === '/api/workspaces') return route.fulfill({json:[]});
		if (!path.startsWith('/api/')) return route.continue();
		if (path === '/api/auth/me') return route.fulfill({ json: user });
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
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: revoked ? [teams[1]] : [{...teams[0], is_private: isPrivate}, teams[1]] });
  if (path === `/api/workspaces/test/teams/${teamId}/visibility`) {
   const body = route.request().postDataJSON(); visibilityWrites.push(body); isPrivate = body.is_private;
   return route.fulfill({json: {...teams[0], is_private: isPrivate}});
  }
  if (path === `/api/workspaces/test/teams/${teamId}/members`) {
   if (route.request().method() === 'POST') teamMembers = [{user_id: userId, team_id: teamId}];
   return route.fulfill({json: teamMembers});
  }
  if (path === `/api/workspaces/test/teams/${teamId}/members/${userId}`) {teamMembers = []; return route.fulfill({json: {}});}
  if (path === '/api/workspaces/test/members') return route.fulfill({json: [{user_id: userId, name: 'Test User', role: 'member', email: 'test@example.com'}]});
  if (path === '/api/workspaces/test/github/status') return route.fulfill({json: {connected: false, repositories: []}});
  if (path.includes('/dev-machine')) return route.fulfill({json: []});
  if (path === '/api/system/update-status') return route.fulfill({json: {}});
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/issues')
			return route.fulfill({
				json: {
					data: revoked ? [] : issues,
					total_count: revoked ? 0 : issues.length,
					page: 1,
					has_more: false
				}
			});
		if (path === '/api/workspaces/test/dev-machines')
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path.match(/^\/api\/workspaces\/test\/teams\/[^/]+\/statuses$/))
			return route.fulfill({
				json: [
					{
						id: statusId,
						team_id: url.pathname.split('/')[5],
						name: 'Todo',
						slug: 'todo',
						category: 'unstarted',
						color: '#6366f1',
						position: 0,
						is_default: true,
						project_ids: [],
						created_at: createdAt,
						updated_at: createdAt
					}
				]
			});
		const detailMatch = path.match(/^\/api\/workspaces\/test\/issues\/(ENG-[123])$/);
		if (detailMatch && revoked) return route.fulfill({status: 404, json: {error: {message: 'Not found'}}});
		if (detailMatch) return route.fulfill({ json: issues.find((issue) => issue.identifier === detailMatch[1]) });
		if (path.match(/^\/api\/workspaces\/test\/issues\/ENG-[123]\/github$/))
			return route.fulfill({ json: { pull_requests: [], branches: [], commits: [] } });
		if (
			[
				'/api/workspaces/test/projects',
				'/api/workspaces/test/labels',
				'/api/workspaces/test/members',
				'/api/workspaces/test/views'
			].includes(path) ||
			path.match(/^\/api\/workspaces\/test\/teams\/[^/]+\/cycles$/) ||
			path.match(/^\/api\/workspaces\/test\/issues\/ENG-[123]\/(comments|history|relations|sub-issues)$/)
		)
			return route.fulfill({ json: [] });
		unhandledPaths.push(path);
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});
	return { disconnectRevokedWorkspace: () => { workspaceRevoked = true; expect(socket).toBeDefined(); socket!.close({code:1001,reason:'Proxy disconnected'}); }, revoke: () => { revoked = true; }, visibilityWrites, issues, assertHealthy: () => { expect(pageErrors).toEqual([]); expect(unhandledPaths).toEqual([]); } };
}


test('an open private issue disappears when live access is revoked', async ({page}) => {
 const server = await mockWorkspace(page);
 await page.goto('/test/issue/ENG-1');
 await expect(page.getByText('Navigation issue 1', {exact: true}).first()).toBeVisible();
 server.revoke();
 await page.evaluate(() => window.dispatchEvent(new CustomEvent('app:refresh', {detail: {slug: 'test', privacy: true, resources: ['issues', 'teams', 'projects', 'members', 'workspace']}})));
 await expect(page.getByText('This resource is unavailable or you no longer have access.')).toBeVisible();
 await expect(page.getByText('Navigation issue 1', {exact: true})).toHaveCount(0);
 await expect(page.getByText('Engineering', {exact: true})).toHaveCount(0);
 server.assertHealthy();
});

test('making a private team public requires explicit confirmation', async ({page}) => {
 const server = await mockWorkspace(page);
 await page.goto(`/test/settings/teams/${teamId}`);
 await page.getByRole('button', {name: 'Make public', exact: true}).click();
 const dialog = page.getByRole('dialog');
 await expect(dialog.getByRole('button', {name: 'Save', exact: true})).toBeDisabled();
 await dialog.getByRole('checkbox').check();
 await dialog.getByRole('button', {name: 'Save', exact: true}).click();
 await expect(page.getByRole('button', {name: 'Make private', exact: true})).toBeVisible();
 expect(server.visibilityWrites).toEqual([{is_private: false, confirm_public: true, acknowledge_privacy_limitations: false}]);
 await page.getByRole('button', {name: 'Make private', exact: true}).click();
 await expect(dialog.getByText('I understand these workspace-wide limitations.')).toBeVisible();
 await expect(dialog.getByRole('button', {name: 'Save', exact: true})).toBeDisabled();
 await page.screenshot({path: '../tmp/reports/issue-61-privacy-desktop.png', animations: 'disabled'});
 await page.setViewportSize({width: 390, height: 844});
 await page.screenshot({path: '../tmp/reports/issue-61-privacy-mobile.png', animations: 'disabled'});
 await dialog.getByRole('checkbox').check();
 await dialog.getByRole('button', {name: 'Save', exact: true}).click();
 await expect(page.getByRole('button', {name: 'Make public', exact: true})).toBeVisible();
 expect(server.visibilityWrites[1]).toEqual({is_private: true, confirm_public: false, acknowledge_privacy_limitations: true});
 server.assertHealthy();
});


test('an older successful issue response cannot restore a revoked page', async ({page}) => {
 const server = await mockWorkspace(page);
 await page.goto('/test/issue/ENG-1');
 await expect(page.getByText('Navigation issue 1', {exact: true}).first()).toBeVisible();
 let release: (() => void) | undefined;
 const gate = new Promise<void>(resolve => { release = resolve; });
 let intercepted = false;
 await page.route('**/api/workspaces/test/issues/ENG-1', async route => {
  if (intercepted) return route.fallback();
  intercepted = true;
  await gate;
  await route.fulfill({json: server.issues[0]});
 });
 await page.evaluate(() => window.dispatchEvent(new CustomEvent('app:refresh', {detail: {slug: 'test', resources: ['issues']}})));
 await expect.poll(() => intercepted).toBe(true);
 server.revoke();
 await page.evaluate(() => window.dispatchEvent(new CustomEvent('app:refresh', {detail: {slug: 'test', resources: ['issues', 'teams', 'members']}})));
 await expect(page.getByText('This resource is unavailable or you no longer have access.')).toBeVisible();
 const staleResponse = page.waitForResponse(response => response.url().endsWith('/issues/ENG-1') && response.status() === 200);
 release!();
 await staleResponse;
 await expect(page.getByText('This resource is unavailable or you no longer have access.')).toBeVisible();
 await expect(page.getByText('Navigation issue 1', {exact: true})).toHaveCount(0);
 server.assertHealthy();
});


test('an administrator can add and remove an explicit team member', async ({page}) => {
 const server = await mockWorkspace(page);
 await page.goto(`/test/settings/teams/${teamId}`);
 await page.getByRole('combobox', {name: 'Choose a workspace member'}).selectOption(userId);
 await page.getByRole('button', {name: 'Add member', exact: true}).click();
 await expect(page.getByRole('button', {name: 'Remove Test User from team'})).toBeVisible();
 await page.getByRole('button', {name: 'Remove Test User from team'}).click();
 await expect(page.getByRole('button', {name: 'Remove Test User from team'})).toHaveCount(0);
 await expect(page.getByRole('combobox', {name: 'Choose a workspace member'}).getByRole('option', {name: 'Test User (member)'})).toHaveCount(1);
 server.assertHealthy();
});


test('a non-policy WebSocket close still rechecks workspace access and clears cached content', async ({page}) => {
 const server = await mockWorkspace(page);
 await page.goto('/test/issue/ENG-1');
 await expect(page.getByText('Navigation issue 1', {exact:true}).first()).toBeVisible();
 server.disconnectRevokedWorkspace();
 await expect(page.getByText('Navigation issue 1', {exact:true})).toHaveCount(0);
 await expect(page.getByText('Engineering', {exact:true})).toHaveCount(0);
 server.assertHealthy();
});
