import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const teamId = '00000000-0000-0000-0000-000000000010';
interface MockUser {
	id: string;
	email: string;
	name: string;
	display_name: string;
	avatar_url: string | null;
	is_sysadmin: boolean;
}
const validUser: MockUser = {
	id: '00000000-0000-0000-0000-0000000000a1',
	email: 'valid@example.com',
	name: 'Valid User',
	display_name: 'Valid User',
	avatar_url: '/api/users/00000000-0000-0000-0000-0000000000a1/avatar/00000000-0000-0000-0000-0000000000f1.png',
	is_sysadmin: false
};
const noAvatarUser: MockUser = {
	id: '00000000-0000-0000-0000-0000000000a2',
	email: 'plain@example.com',
	name: 'Plain User',
	display_name: 'Plain User',
	avatar_url: null,
	is_sysadmin: false
};
const brokenUser: MockUser = {
	id: '00000000-0000-0000-0000-0000000000a3',
	email: 'broken@example.com',
	name: 'Broken User',
	display_name: 'Broken User',
	avatar_url: '/api/users/00000000-0000-0000-0000-0000000000a3/avatar/00000000-0000-0000-0000-0000000000f3.png',
	is_sysadmin: false
};
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==', 'base64');

function issue(index: number, identifier: string, title: string, assignee: MockUser) {
	return {
		id: `00000000-0000-0000-0000-00000000010${index}`,
		identifier,
		title,
		description: null,
		status: 'todo',
		status_id: '00000000-0000-0000-0000-0000000000b1',
		status_info: { id: '00000000-0000-0000-0000-0000000000b1', name: 'Todo', category: 'unstarted', color: '#999999', position: 0 },
		priority: 2,
		team_id: teamId,
		project_id: null,
		cycle_id: null,
		creator_id: validUser.id,
		assignee_id: assignee.id,
		parent_id: null,
		due_date: null,
		sort_order: index,
		assignee,
		assignees: [assignee],
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z'
	};
}

const issues = [
	issue(1, 'ENG-1', 'Has an avatar', validUser),
	issue(2, 'ENG-2', 'Has no avatar', noAvatarUser),
	issue(3, 'ENG-3', 'Has a broken avatar', brokenUser)
];

async function mockAvatars(page: Page) {
	const team = {
		id: teamId,
		name: 'Engineering',
		key: 'ENG',
		description: null,
		color: '#6366f1',
		icon: 'SquareUser',
		triage_enabled: false,
		parent_auto_close_enabled: false,
		sub_issue_auto_close_enabled: false,
		issue_copy_prompt: null,
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z'
	};
	const members = [validUser, noAvatarUser, brokenUser].map((user) => ({
		user_id: user.id,
		email: user.email,
		name: user.name,
		avatar_url: user.avatar_url,
		role: 'member',
		created_at: '2026-01-01T00:00:00Z'
	}));

	await page.routeWebSocket('**/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me') return route.fulfill({ json: { ...validUser, email: 'me@example.com', name: 'Me' } });
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
		if (path === '/api/workspaces') {
			return route.fulfill({
				json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test', logo_url: null, current_user_role: 'owner' }]
			});
		}
		if (path === '/api/workspaces/test') {
			return route.fulfill({
				json: {
					id: workspaceId,
					name: 'Test Workspace',
					slug: 'test',
					logo_url: null,
					owner_id: validUser.id,
					current_user_role: 'owner',
					share_link_min_role: 'admin',
					created_at: '2026-01-01T00:00:00Z',
					updated_at: '2026-01-01T00:00:00Z'
				}
			});
		}
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [team] });
		if (path === '/api/workspaces/test/teams/' + teamId + '/statuses') {
			return route.fulfill({ json: [] });
		}
		if (path === '/api/workspaces/test/members') return route.fulfill({ json: members });
		if (path === '/api/workspaces/test/invite-links') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/labels' || path === '/api/workspaces/test/projects') {
			return route.fulfill({ json: [] });
		}
		if (path === '/api/workspaces/test/views') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/teams/' + teamId + '/cycles') return route.fulfill({ json: [] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/issues') {
			return route.fulfill({ json: { data: issues, total_count: issues.length, page: 1, has_more: false } });
		}
		if (path === '/api/workspaces/test/issues/ENG-1') return route.fulfill({ json: issues[0] });
		if (path === '/api/workspaces/test/issues/ENG-1/comments') {
			return route.fulfill({
				json: [
					{
						id: '00000000-0000-0000-0000-0000000000c1',
						issue_id: issues[0].id,
						user_id: validUser.id,
						body: 'Comment with an avatar',
						user: validUser,
						created_at: '2026-01-01T00:00:00Z',
						updated_at: '2026-01-01T00:00:00Z'
					}
				]
			});
		}
		if (path === '/api/workspaces/test/issues/ENG-1/history') {
			return route.fulfill({
				json: [
					{
						id: '00000000-0000-0000-0000-0000000000d1',
						issue_id: issues[0].id,
						user_id: validUser.id,
						field: 'status_id',
						old_value: '00000000-0000-0000-0000-0000000000b1',
						new_value: '00000000-0000-0000-0000-0000000000b2',
						created_at: '2026-01-01T00:00:01Z'
					},
					{
						id: '00000000-0000-0000-0000-0000000000d2',
						issue_id: issues[0].id,
						user_id: brokenUser.id,
						field: 'priority',
						old_value: '1',
						new_value: '2',
						created_at: '2026-01-01T00:00:10Z'
					}
				]
			});
		}
		if (path.startsWith(validUser.avatar_url!)) return route.fulfill({ body: png, contentType: 'image/png' });
		if (path.startsWith('/api/users/00000000-0000-0000-0000-0000000000a3/avatar/')) {
			return route.fulfill({ status: 404, body: 'not found' });
		}
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});
}

test('shows image avatars in the issue list and initials only without a usable image', async ({ page }) => {
	await mockAvatars(page);
	await page.goto(`/test/teams/${teamId}`);

	const validRow = page.locator('[role="button"][aria-label="ENG-1"]');
	await expect(validRow.locator(`img[src="${validUser.avatar_url}"]`).first()).toBeVisible();

	const plainRow = page.locator('[role="button"][aria-label="ENG-2"]');
	await expect(plainRow.locator('[data-slot="avatar-fallback"]')).toHaveText('P');
	await expect(plainRow.locator('img')).toHaveCount(0);

	const brokenRow = page.locator('[role="button"][aria-label="ENG-3"]');
	await expect(brokenRow.locator('[data-slot="avatar-fallback"]')).toHaveText('B');
});

test('shows image avatars for the detail assignee chip and comment author', async ({ page }) => {
	await mockAvatars(page);
	await page.goto('/test/issue/ENG-1');

	await expect(page.getByText('Has an avatar').first()).toBeVisible();
	await expect(page.locator(`img[src="${validUser.avatar_url}"]`).first()).toBeVisible();
	await expect(page.getByText('Comment with an avatar')).toBeVisible();
	await expect(
		page.locator(`text=Comment with an avatar >> xpath=ancestor::div[contains(@class,"group/comment")]`).locator(`img[src="${validUser.avatar_url}"]`)
	).toBeVisible();
});

test('shows image avatars for activity feed actors', async ({ page }) => {
	await mockAvatars(page);
	await page.goto('/test/issue/ENG-1');

	const historyActor = page.locator(`[title="Valid User"] img[src="${validUser.avatar_url}"]`);
	await expect(historyActor.first()).toBeVisible();
	await expect(page.locator('[title="Broken User"] [data-slot="avatar-fallback"]')).toHaveText('B');
});

test('shows image avatars in the workspace members list', async ({ page }) => {
	await mockAvatars(page);
	await page.goto('/test/settings/members');

	const member = page.getByText('plain@example.com').locator('xpath=ancestor::div[contains(@class,"justify-between")]');
	await expect(member.locator('[data-slot="avatar-fallback"]')).toHaveText('P');
	await expect(page.locator(`img[src="${validUser.avatar_url}"]`).first()).toBeVisible();
});
