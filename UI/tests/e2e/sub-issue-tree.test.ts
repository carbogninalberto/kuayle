import { expect, test, type Page } from '@playwright/test';

const teamId = '00000000-0000-0000-0000-000000000010';
const todoId = '00000000-0000-0000-0000-000000000030';
const doneId = '00000000-0000-0000-0000-000000000031';
const now = '2026-01-01T00:00:00Z';
const issueId = (n: number) => `00000000-0000-0000-0000-${String(100 + n).padStart(12, '0')}`;

const statuses = [
	{ id: todoId, team_id: teamId, name: 'Todo', slug: 'todo', category: 'unstarted', position: 0, is_default: true },
	{ id: doneId, team_id: teamId, name: 'Done', slug: 'done', category: 'completed', position: 1, is_default: false }
];

function issue(n: number, title: string, parent: number | string | null, done = false) {
	const status = done ? statuses[1] : statuses[0];
	return {
		id: issueId(n),
		identifier: `ENG-${n}`,
		title,
		team_id: teamId,
		status_id: status.id,
		status: status.slug,
		status_info: { id: status.id, name: status.name, category: status.category, color: null, position: status.position },
		priority: 3,
		parent_id: typeof parent === 'number' ? issueId(parent) : parent,
		project_id: null,
		cycle_id: null,
		labels: [],
		assignees: [],
		sort_order: n * 1000,
		created_at: now,
		updated_at: now
	};
}

// ENG-1 has ENG-2 and ENG-3 in the same group, ENG-4 under ENG-2, and ENG-5 in another group.
// ENG-6's parent is not in the list (filtered out or on another page).
const issues = [
	{ ...issue(1, 'Parent epic', null), sub_issue_count: 3, sub_issue_done: 1 },
	{ ...issue(2, 'First child', 1), sub_issue_count: 1, sub_issue_done: 0 },
	issue(3, 'Second child', 1),
	issue(4, 'Grandchild', 2),
	issue(5, 'Finished child', 1, true),
	issue(6, 'Orphaned child', '00000000-0000-0000-0000-000000000999')
];

async function mockTeamPage(page: Page) {
	await page.routeWebSocket('**/ws', () => {});
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user', name: 'Owner', display_name: 'Owner', email: 'owner@example.com' } });
		if (path === '/api/preferences') return route.fulfill({ json: { issues_group_by: 'status', theme_mode: 'dark' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: 'workspace', slug: 'test', name: 'Test' }] });
		if (path === '/api/workspaces/test')
			return route.fulfill({ json: { id: 'workspace', slug: 'test', name: 'Test', current_user_role: 'owner' } });
		if (path === '/api/workspaces/test/teams')
			return route.fulfill({
				json: [{ id: teamId, name: 'Engineering', key: 'ENG', color: '#6366f1', icon: 'layers', created_at: now }]
			});
		if (path === `/api/workspaces/test/teams/${teamId}/statuses`) return route.fulfill({ json: statuses });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/issues')
			return route.fulfill({ json: { data: issues, total_count: issues.length, page: 1, has_more: false } });
		if (path === '/api/workspaces/test/projects') return route.fulfill({ json: [project] });
		if (path === `/api/workspaces/test/projects/${projectId}`)
			return route.fulfill({ json: { ...project, progress: { total: issues.length, completed: 1, cancelled: 0 } } });
		if (path.endsWith('/github/status')) return route.fulfill({ json: { configured: false, repos: [] } });
		if (path.endsWith('/dev-machine-scope-setting')) return route.fulfill({ json: {} });
		return route.fulfill({ json: [] });
	});
}

const projectId = '00000000-0000-0000-0000-000000000020';
const project = { id: projectId, name: 'Launch', team_id: teamId, status: 'planned', color: null, icon: null, created_at: now };

test('nests sub-issues under their parent, collapsible by mouse and keyboard, and remembers it', async ({ page }) => {
	await mockTeamPage(page);
	await page.goto(`/test/teams/${teamId}`);
	const row = (identifier: string) => page.getByRole('button', { name: identifier, exact: true });
	const toggles = page.getByRole('button', { name: /^Sub-issues of ENG-/ });

	// Flat by default: every issue is its own row and no row has a sub-issue toggle.
	for (const n of [1, 2, 3, 4, 5, 6]) await expect(row(`ENG-${n}`)).toBeVisible();
	await expect(toggles).toHaveCount(0);

	await page.getByRole('button', { name: 'Group', exact: true }).click();
	const nest = page.getByRole('switch', { name: 'Nest sub-issues' });
	await expect(nest).not.toBeChecked();
	await nest.click();
	await expect(nest).toBeChecked();
	await page.keyboard.press('Escape');

	// Collapsed by default. A sub-issue whose parent is in another group or not loaded stays a top-level row.
	const parentToggle = page.getByRole('button', { name: 'Sub-issues of ENG-1 (2)' });
	await expect(parentToggle).toHaveAttribute('aria-expanded', 'false');
	await expect(toggles).toHaveCount(1);
	for (const n of [2, 3, 4]) await expect(row(`ENG-${n}`)).toBeHidden();
	await expect(row('ENG-5')).toBeVisible();
	await expect(row('ENG-6')).toBeVisible();

	// The chevron is a real button: Enter expands without opening the parent issue.
	await parentToggle.focus();
	await page.keyboard.press('Enter');
	await expect(parentToggle).toHaveAttribute('aria-expanded', 'true');
	await expect(page).toHaveURL(new RegExp(`/test/teams/${teamId}$`));
	const controls = await parentToggle.getAttribute('aria-controls');
	expect(controls).toBeTruthy();
	await expect(page.locator(`[id="${controls}"]`).getByRole('button', { name: 'ENG-2', exact: true })).toBeVisible();
	await expect(row('ENG-3')).toBeVisible();
	await expect(row('ENG-4')).toBeHidden();

	// Arrow keys on a focused row: Right expands, Left collapses, Left again moves to the parent row.
	const childToggle = page.getByRole('button', { name: 'Sub-issues of ENG-2 (1)' });
	await row('ENG-2').focus();
	await page.keyboard.press('ArrowRight');
	await expect(childToggle).toHaveAttribute('aria-expanded', 'true');
	await expect(row('ENG-4')).toBeVisible();
	await page.keyboard.press('ArrowLeft');
	await expect(childToggle).toHaveAttribute('aria-expanded', 'false');
	await expect(row('ENG-4')).toBeHidden();
	await page.keyboard.press('ArrowLeft');
	await expect(row('ENG-1')).toBeFocused();
	await page.keyboard.press('ArrowLeft');
	await expect(parentToggle).toHaveAttribute('aria-expanded', 'false');
	await expect(row('ENG-2')).toBeHidden();

	// Space on the chevron and a mouse click toggle it as well.
	await parentToggle.focus();
	await page.keyboard.press('Space');
	await expect(parentToggle).toHaveAttribute('aria-expanded', 'true');
	await childToggle.click();
	await expect(row('ENG-4')).toBeVisible();
	await expect(page).toHaveURL(new RegExp(`/test/teams/${teamId}$`));

	// The nesting option and the expanded parents survive a reload.
	await page.reload();
	await expect(page.getByRole('button', { name: 'Sub-issues of ENG-1 (2)' })).toHaveAttribute('aria-expanded', 'true');
	await expect(row('ENG-4')).toBeVisible();

	// Turning nesting off restores the flat list.
	await page.getByRole('button', { name: 'Group', exact: true }).click();
	await page.getByRole('switch', { name: 'Nest sub-issues' }).click();
	await page.keyboard.press('Escape');
	await expect(toggles).toHaveCount(0);
	for (const n of [1, 2, 3, 4, 5, 6]) await expect(row(`ENG-${n}`)).toBeVisible();
});

test('project issue lists nest sub-issues with the same option', async ({ page }) => {
	await mockTeamPage(page);
	await page.goto(`/test/projects/${projectId}`);
	const scoped = page.getByTestId('scoped-issues');
	const row = (identifier: string) => scoped.getByRole('button', { name: identifier, exact: true });
	await expect(row('ENG-2')).toBeVisible();

	await scoped.getByRole('button', { name: 'Group by', exact: true }).click();
	await page.getByRole('switch', { name: 'Nest sub-issues' }).click();
	await page.keyboard.press('Escape');

	const parentToggle = scoped.getByRole('button', { name: 'Sub-issues of ENG-1 (2)' });
	await expect(parentToggle).toHaveAttribute('aria-expanded', 'false');
	await expect(row('ENG-2')).toBeHidden();
	await expect(row('ENG-6')).toBeVisible();
	await parentToggle.click();
	await expect(parentToggle).toHaveAttribute('aria-expanded', 'true');
	await expect(row('ENG-2')).toBeVisible();
	await expect(page).toHaveURL(new RegExp(`/test/projects/${projectId}$`));
});
