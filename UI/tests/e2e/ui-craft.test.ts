import { test, expect, type Page, type Locator } from '@playwright/test';

// Regressions from the KUA-62 control migration: Buttons injected shadcn sizing,
// expanded/focus highlights and row semantics that the original controls did not have.

const userId = '00000000-0000-0000-0000-000000000001';
const teamId = '00000000-0000-0000-0000-000000000010';
const todoId = '00000000-0000-0000-0000-000000000020';
const doneId = '00000000-0000-0000-0000-000000000021';
const createdAt = '2026-01-01T00:00:00Z';
const transparent = 'rgba(0, 0, 0, 0)';

const user = { id: userId, email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null };
const workspace = { id: '00000000-0000-0000-0000-000000000002', name: 'Test Workspace', slug: 'test', logo_url: null, current_user_role: 'owner', created_at: createdAt, updated_at: createdAt };
const team = { id: teamId, name: 'Engineering', key: 'ENG', description: null, color: '#6366f1', icon: 'layers', triage_enabled: false, created_at: createdAt, updated_at: createdAt };
const statuses = [
	{ id: todoId, team_id: teamId, name: 'Todo', slug: 'todo', category: 'unstarted', color: '#6366f1', position: 0, is_default: true, project_ids: [], created_at: createdAt, updated_at: createdAt },
	{ id: doneId, team_id: teamId, name: 'Done', slug: 'done', category: 'completed', color: '#22c55e', position: 1, is_default: false, project_ids: [], created_at: createdAt, updated_at: createdAt }
];
const issues = [1, 2, 3].map((number) => {
	const status = number === 3 ? statuses[1] : statuses[0];
	return {
		id: `00000000-0000-0000-0000-${String(100 + number).padStart(12, '0')}`,
		identifier: `ENG-${number}`,
		title: `Craft issue ${number}`,
		description: null,
		status: status.slug,
		status_id: status.id,
		status_info: { id: status.id, name: status.name, category: status.category, color: status.color, position: status.position },
		priority: 3,
		team_id: teamId,
		project_id: null,
		cycle_id: null,
		creator_id: userId,
		assignee_id: null,
		creator: user,
		assignee: null,
		assignees: [],
		parent_id: null,
		due_date: null,
		sort_order: number * 1000,
		labels: [],
		sub_issue_count: 0,
		sub_issue_done: 0,
		created_at: createdAt,
		updated_at: createdAt
	};
});

async function mockWorkspace(page: Page, theme: 'light' | 'dark' = 'dark') {
	await page.routeWebSocket('**/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path === '/api/auth/me') return route.fulfill({ json: user });
		if (path === '/api/preferences') return route.fulfill({ json: { theme_mode: theme, light_theme: 'light', dark_theme: 'dark', pointer_cursors: true, issues_group_by: 'status', workflow_sort_mode: 'default', workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'], team_workflow_sort_overrides: {} } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [team] });
		if (path === `/api/workspaces/test/teams/${teamId}/statuses`) return route.fulfill({ json: statuses });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/issues') return route.fulfill({ json: { data: issues, total_count: issues.length, page: 1, has_more: false } });
		if (path === '/api/workspaces/test/dev-machines') return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (/^\/api\/workspaces\/test\/issues\/ENG-\d+$/.test(path)) return route.fulfill({ json: issues.find((issue) => path.endsWith(`/${issue.identifier}`)) });
		if (/^\/api\/workspaces\/test\/issues\/ENG-\d+\/github$/.test(path)) return route.fulfill({ json: { pull_requests: [], branches: [], commits: [] } });
		return route.fulfill({ json: [] });
	});
}

const background = (locator: Locator) => locator.evaluate((node) => getComputedStyle(node).backgroundColor);

test.describe('login controls', () => {
	test('password reveal is centered in the field and stays out of the tab order', async ({ page }) => {
		await page.goto('/login');
		const field = page.getByRole('textbox', { name: 'Password', exact: true });
		const toggle = page.getByRole('button', { name: 'Show password' });
		await expect(toggle).toHaveAttribute('tabindex', '-1');

		const [fieldBox, toggleBox, iconBox] = await Promise.all([
			field.boundingBox(),
			toggle.boundingBox(),
			toggle.locator('svg').boundingBox()
		]);
		expect(toggleBox!.height).toBeCloseTo(fieldBox!.height, 0);
		const fieldCenter = fieldBox!.y + fieldBox!.height / 2;
		expect(Math.abs(iconBox!.y + iconBox!.height / 2 - fieldCenter)).toBeLessThanOrEqual(1.5);
		expect(fieldBox!.x + fieldBox!.width - (toggleBox!.x + toggleBox!.width)).toBeLessThanOrEqual(1);

		await page.getByLabel('Email').focus();
		await page.keyboard.press('Tab');
		await expect(field).toBeFocused();
		await page.keyboard.press('Tab');
		await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeFocused();

		await toggle.click();
		await expect(page.getByRole('button', { name: 'Hide password' })).toBeVisible();
		await expect(field).toHaveAttribute('type', 'text');
	});

	test('sign in keeps its crafted full-width accent button', async ({ page }) => {
		await page.goto('/login');
		const submit = page.getByRole('button', { name: 'Sign in', exact: true });
		const field = page.getByLabel('Email');
		const [submitBox, fieldBox] = await Promise.all([submit.boundingBox(), field.boundingBox()]);
		expect(submitBox!.height).toBeGreaterThanOrEqual(36);
		expect(submitBox!.width).toBeCloseTo(fieldBox!.width, 0);
		const accent = await page.evaluate(() => {
			const probe = document.createElement('div');
			probe.style.backgroundColor = 'var(--app-accent)';
			document.body.append(probe);
			const color = getComputedStyle(probe).backgroundColor;
			probe.remove();
			return color;
		});
		expect(await background(submit)).toBe(accent);
		await expect(submit).toHaveCSS('font-size', '14px');
	});
});

test.describe('workspace controls', () => {
	test.beforeEach(async ({ page }) => {
		await page.setViewportSize({ width: 1280, height: 900 });
		await mockWorkspace(page);
	});

	test('sidebar section toggles report state without an expanded highlight', async ({ page }) => {
		await page.goto(`/test/teams/${teamId}`);
		const nav = page.locator('nav:visible');
		const teams = nav.getByRole('button', { name: 'Teams', exact: true });
		await expect(teams).toHaveAttribute('aria-expanded', 'true');
		expect(await background(teams)).toBe(transparent);
		const engineering = nav.getByRole('button', { name: 'Engineering', exact: true });
		await expect(engineering).toHaveAttribute('aria-expanded', 'true');
		expect(await background(engineering)).toBe(transparent);
		await expect(engineering).toHaveCSS('font-weight', '400');

		await teams.click();
		await expect(teams).toHaveAttribute('aria-expanded', 'false');
		await expect(engineering).toHaveCount(0);
		await teams.click();
		await expect(teams).toHaveAttribute('aria-expanded', 'true');
		await expect(engineering).toBeVisible();
	});

	test('issue group headers collapse without an expanded highlight or ring', async ({ page }) => {
		await page.goto(`/test/teams/${teamId}`);
		const header = page.getByRole('button', { name: /^Todo\s*2$/ });
		await expect(header).toHaveAttribute('aria-expanded', 'true');
		expect(await background(header)).toBe(transparent);
		await header.focus();
		await expect(header).toHaveCSS('box-shadow', 'none');

		await page.keyboard.press('Enter');
		await expect(header).toHaveAttribute('aria-expanded', 'false');
		await expect(page.getByText('Craft issue 1', { exact: true })).toHaveCount(0);
		await header.click();
		await expect(page.getByText('Craft issue 1', { exact: true })).toBeVisible();
	});

	test('the selection zone selects issues without opening them', async ({ page }) => {
		await page.goto(`/test/teams/${teamId}`);
		const zones = page.getByTestId('issue-select-zone');
		await expect(zones).toHaveCount(3);
		const first = zones.nth(0);
		const box = await first.boundingBox();
		// Click the zone's padding rather than the checkbox itself.
		await page.mouse.move(box!.x + 3, box!.y + box!.height / 2);
		await page.mouse.click(box!.x + 3, box!.y + box!.height / 2);
		await expect(page).toHaveURL(`/test/teams/${teamId}`);
		await expect(first.getByRole('checkbox')).toBeChecked();

		const third = zones.nth(2);
		await third.hover();
		await page.keyboard.down('Shift');
		await third.click();
		await page.keyboard.up('Shift');
		await expect(zones.nth(1).getByRole('checkbox')).toBeChecked();
		await expect(third.getByRole('checkbox')).toBeChecked();
	});

	test('issue rows keep keyboard activation', async ({ page }) => {
		await page.goto(`/test/teams/${teamId}`);
		const row = page.getByRole('button', { name: 'ENG-1', exact: true });
		await row.focus();
		await page.keyboard.press('Enter');
		await expect(page).toHaveURL('/test/issue/ENG-1');
	});

	test('the collapsed sidebar drawer closes from its overlay by click and Escape', async ({ page }) => {
		await page.addInitScript(() => localStorage.setItem('sidebar_collapsed_panel', 'true'));
		await page.goto(`/test/teams/${teamId}`);
		const overlay = page.getByRole('button', { name: 'Close sidebar' });
		const drawer = page.locator('[role="presentation"]').filter({ has: page.getByRole('link', { name: 'Inbox' }) });

		await page.mouse.move(400, 400);
		await page.mouse.move(2, 400);
		await expect(drawer).toHaveCSS('transform', 'matrix(1, 0, 0, 1, 0, 0)');
		await expect(overlay).toHaveCSS('background-color', 'rgba(0, 0, 0, 0.15)');
		await page.mouse.click(1000, 500);
		await expect(overlay).toHaveCSS('background-color', transparent);

		await page.mouse.move(400, 400);
		await page.mouse.move(2, 400);
		await expect(overlay).toHaveCSS('background-color', 'rgba(0, 0, 0, 0.15)');
		await overlay.focus();
		await page.keyboard.press('Escape');
		await expect(overlay).toHaveCSS('background-color', transparent);
	});
});
