import { test, expect, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';

// Shared with the Go sanitizer's tests and the UI unit tests.
const contract: {
	documents: { name: string; html: string }[];
	stripped: { name: string; html: string; want: string }[];
} = JSON.parse(
	readFileSync(new URL('../../../BE/pkg/sanitize/testdata/editor_html_contract.json', import.meta.url), 'utf8')
);

const userId = '00000000-0000-0000-0000-000000000001';
const teamId = '00000000-0000-0000-0000-000000000010';
const statusId = '00000000-0000-0000-0000-000000000020';
const createdAt = '2026-01-01T00:00:00Z';
const overlayUrl = 'https://overlay.example/landing';

const classAttributes = (html: string) => [...html.matchAll(/\sclass="([^"]*)"/g)].map((match) => match[1]);

async function mockIssueWithComments(page: Page, bodies: string[]) {
	await page.routeWebSocket('**/ws', () => {});
	const overlayRequests: string[] = [];
	await page.route('https://overlay.example/**', (route) => {
		overlayRequests.push(route.request().url());
		return route.fulfill({ contentType: 'text/html', body: '<h1>Left the app</h1>' });
	});
	const user = { id: userId, email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null };
	const workspace = {
		id: '00000000-0000-0000-0000-000000000002',
		name: 'Test Workspace',
		slug: 'test',
		logo_url: null,
		created_at: createdAt,
		updated_at: createdAt
	};
	const team = {
		id: teamId,
		name: 'Engineering',
		key: 'ENG',
		description: null,
		color: '#6366f1',
		icon: 'layers',
		triage_enabled: false,
		parent_auto_close_enabled: false,
		sub_issue_auto_close_enabled: false,
		issue_copy_prompt: null,
		created_at: createdAt,
		updated_at: createdAt
	};
	const status = {
		id: statusId,
		team_id: teamId,
		name: 'Todo',
		slug: 'todo',
		category: 'unstarted',
		color: '#6366f1',
		position: 0,
		is_default: true,
		project_ids: [],
		created_at: createdAt,
		updated_at: createdAt
	};
	const issue = {
		id: '00000000-0000-0000-0000-000000000101',
		identifier: 'ENG-1',
		title: 'Stored content keeps only editor classes',
		description: '<p>Issue description</p>',
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
		parent: null,
		due_date: null,
		sort_order: 1000,
		labels: [],
		sub_issue_count: 0,
		sub_issue_done: 0,
		created_at: createdAt,
		updated_at: createdAt
	};
	const comments = bodies.map((body, index) => ({
		id: `00000000-0000-0000-0000-${String(200 + index).padStart(12, '0')}`,
		issue_id: issue.id,
		user_id: userId,
		body,
		user,
		replies: [],
		created_at: createdAt,
		updated_at: createdAt
	}));

	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const url = new URL(route.request().url());
		const path = url.pathname;
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
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [team] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/issues')
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path === '/api/workspaces/test/issues/ENG-1') return route.fulfill({ json: issue });
		if (path === '/api/workspaces/test/issues/ENG-1/comments') return route.fulfill({ json: comments });
		if (path === '/api/workspaces/test/issues/ENG-1/github')
			return route.fulfill({ json: { pull_requests: [], branches: [], commits: [] } });
		if (path === '/api/workspaces/test/dev-machines')
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path.match(/^\/api\/workspaces\/test\/teams\/[^/]+\/statuses$/)) return route.fulfill({ json: [status] });
		if (path.endsWith('/members')) return route.fulfill({ json: [{ user_id: userId, name: 'Test User', role: 'owner' }] });
		return route.fulfill({ json: [] });
	});
	return { overlayRequests };
}

test('a comment cannot stretch an invisible link over the page', async ({ page }) => {
	const { overlayRequests } = await mockIssueWithComments(page, [
		`<p>Visible comment text <a href="${overlayUrl}" class="fixed inset-0 z-50 opacity-0">overlay link</a></p>`
	]);
	await page.goto('/test/issue/ENG-1');

	const overlay = page.locator(`a[href="${overlayUrl}"]`);
	await expect(overlay).toHaveCount(1);

	// Sample the viewport: the link may only be hit where its own text is.
	const covered = await page.evaluate((href) => {
		let hits = 0;
		for (let x = 0.05; x < 1; x += 0.1) {
			for (let y = 0.05; y < 1; y += 0.1) {
				const element = document.elementFromPoint(window.innerWidth * x, window.innerHeight * y);
				if (element?.closest(`a[href="${href}"]`)) hits++;
			}
		}
		return hits;
	}, overlayUrl);
	expect(covered).toBeLessThanOrEqual(1);
	expect(await overlay.getAttribute('class')).toBeNull();

	// Clicks on the page reach what the reader sees, not the link.
	await page.getByText('Visible comment text').click({ position: { x: 5, y: 5 }, timeout: 5000 });
	await page.getByRole('heading', { name: 'Activity' }).click({ timeout: 5000 });
	expect(overlayRequests).toEqual([]);
	expect(new URL(page.url()).pathname).toBe('/test/issue/ENG-1');
});

test('comments keep editor classes and lose every other class', async ({ page }) => {
	await mockIssueWithComments(page, [...contract.documents, ...contract.stripped].map((doc) => doc.html));
	await page.goto('/test/issue/ENG-1');

	await expect(page.getByText('requirements.pdf (2 KB)')).toBeVisible();

	// Classes present in each rendered comment body, ignoring what the app adds
	// after sanitizing: the status icon inside an issue mention, and the title
	// and unavailable state it resolves for a wiki reference.
	const rendered = await page.evaluate(() =>
		[...document.querySelectorAll('.prose')]
			.filter((body) => !body.closest('.ProseMirror') && !body.querySelector('.ProseMirror') && !body.classList.contains('ProseMirror'))
			.map((body) =>
				[...body.querySelectorAll('[class]')]
					.filter((element) => !element.closest('.mention-status-icon, .mention-wiki-title'))
					.map((element) =>
						[...element.classList].filter((name) => !(name === 'mention-unavailable' && element.matches('.mention-wiki'))).join(' ')
					)
			)
	);
	const expected = [
		...contract.documents.map((doc) => classAttributes(doc.html)),
		...contract.stripped.map((doc) => classAttributes(doc.want))
	];
	expect(rendered).toEqual(expected);
	await expect(page.locator('.prose:not(.ProseMirror) .mention-issue').first()).toBeVisible();
});
