import { test, expect, type Locator, type Page, type Route } from '@playwright/test';

const teamId = '00000000-0000-0000-0000-000000000010';
const otherTeamId = '00000000-0000-0000-0000-000000000011';
const projectId = '00000000-0000-0000-0000-000000000012';
const cycleId = '00000000-0000-0000-0000-000000000013';
const otherStatusId = '00000000-0000-0000-0000-000000000014';
const engStatusId = '00000000-0000-0000-0000-000000000015';
const user = { id: '00000000-0000-0000-0000-000000000001', email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null };
const workspace = { id: '00000000-0000-0000-0000-000000000002', name: 'Test Workspace', slug: 'test', logo_url: null, current_user_role: 'owner' };
const team = { id: teamId, name: 'Engineering', key: 'ENG', color: '#6366f1', icon: 'layers', triage_enabled: false };
const otherTeam = { id: otherTeamId, name: 'Operations', key: 'OPS', color: '#10b981', icon: 'layers', triage_enabled: false };

type SetupOptions = {
	templateTeamId?: string | null;
	templateStatus?: string | null;
	extraTeam?: typeof team | null;
	projects?: Record<string, unknown>[];
	cycles?: Record<string, unknown>[];
	statusesByTeam?: Record<string, Record<string, unknown>[]>;
	openFromList?: boolean;
	statusGate?: Promise<void>;
};

async function setup(
	page: Page,
	{
		templateTeamId = null,
		templateStatus = null,
		extraTeam = null,
		projects = [],
		cycles = [],
		statusesByTeam = {},
		openFromList = false,
		statusGate
	}: SetupOptions = {}
) {
	const uploads: Route[] = [];
	const issues: Record<string, unknown>[] = [];
	const statusRequests: string[] = [];
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path.startsWith('/api/workspaces/test/assets/')) return route.fulfill({ contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a4WQAAAAASUVORK5CYII=', 'base64') });
		if (path === '/api/auth/me') return route.fulfill({ json: user });
		if (path === '/api/preferences') return route.fulfill({ json: { theme_mode: 'dark', issues_group_by: 'status' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [team, ...(extraTeam ? [extraTeam] : [])] });
		if (path === '/api/workspaces/test/upload') { uploads.push(route); return; }
		if (path === '/api/workspaces/test/projects') return route.fulfill({ json: projects });
		const statusesMatch = path.match(/\/api\/workspaces\/test\/teams\/([^/]+)\/statuses$/);
		if (statusesMatch) {
			statusRequests.push(statusesMatch[1]);
			await statusGate;
			return route.fulfill({ json: statusesByTeam[statusesMatch[1]] ?? [] });
		}
		const cyclesMatch = path.match(/\/api\/workspaces\/test\/teams\/([^/]+)\/cycles$/);
		if (cyclesMatch) return route.fulfill({ json: cycles.filter((cycle) => cycle.team_id === cyclesMatch[1]) });
		if (path === '/api/workspaces/test/issue-templates')
			return route.fulfill({
				json: [
					{
						id: 'template',
						team_id: templateTeamId,
						status: templateStatus,
						title: 'Bug report',
						default_issue_title: '[Bug] Describe the problem',
						description: '<p>Template description</p>',
						priority: 0,
						label_ids: []
					}
				]
			});
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/issues') {
			if (request.method() === 'POST') {
				const issue = request.postDataJSON();
				issues.push(issue);
				return route.fulfill({ json: { ...issue, id: 'issue', identifier: 'ENG-1', status: 'backlog', creator_id: user.id, labels: [], created_at: '2026-01-01T00:00:00Z' } });
			}
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		}
		if (/\/api\/workspaces\/test\/(labels|members|views|favorites)$/.test(path)) return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: path } } });
	});
	await page.goto(`/test/teams/${teamId}`);
	await page.getByRole('button', { name: openFromList ? 'New Issue' : 'New issue', exact: true }).click();
	await expect(page.locator('#create-issue-title')).toBeVisible();
	await page.locator('#create-issue-title').fill('Issue with attachments');
	return { uploads, issues, statusRequests };
}

async function pasteFiles(target: Locator, names = ['screenshot.png']) {
	await target.evaluate((element, filenames) => {
		const data = new DataTransfer();
		for (const name of filenames) data.items.add(new File(['image'], name, { type: 'image/png' }));
		element.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
	}, names);
}

async function completeUpload(route: Route, name: string) {
	await route.fulfill({ json: { url: `/api/workspaces/test/assets/${name}`, filename: name, content_type: 'image/png' } });
}

test('blocks button and keyboard submission until all clipboard files are inserted', async ({ page }) => {
	const { uploads, issues } = await setup(page);
	await pasteFiles(page.locator('.tiptap'), ['first.png', 'second.png']);
	await expect.poll(() => uploads.length).toBe(1);
	await expect(page.getByRole('button', { name: 'Uploading attachments…' })).toBeDisabled();
	await page.locator('#create-issue-title').press('Control+Enter');
	await page.locator('.tiptap').press('Control+Enter');
	expect(issues).toHaveLength(0);
	await completeUpload(uploads[0], 'first.png');
	await expect.poll(() => uploads.length).toBe(2);
	await expect(page.getByRole('button', { name: 'Uploading attachments…' })).toBeDisabled();
	await completeUpload(uploads[1], 'second.png');
	await expect(page.locator('.tiptap img[src]')).toHaveCount(2);
	await page.getByRole('button', { name: 'Create issue', exact: true }).click();
	await expect.poll(() => issues.length).toBe(1);
	expect(issues[0].description).toContain('/api/workspaces/test/assets/first.png');
	expect(issues[0].description).toContain('/api/workspaces/test/assets/second.png');
});

test('recovers from a failed title paste upload and allows retry', async ({ page }) => {
	const { uploads } = await setup(page);
	await pasteFiles(page.locator('#create-issue-title'));
	await expect.poll(() => uploads.length).toBe(1);
	await uploads[0].fulfill({ status: 500, json: { error: { message: 'Upload rejected' } } });
	await expect(page.getByText('Upload rejected', { exact: true })).toBeVisible();
	await expect(page.locator('.editor-upload-placeholder')).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Create issue', exact: true })).toBeEnabled();
	await pasteFiles(page.locator('#create-issue-title'), ['retry.png']);
	await expect.poll(() => uploads.length).toBe(2);
	await completeUpload(uploads[1], 'retry.png');
	await expect(page.locator('.tiptap img[src]')).toHaveAttribute('src', '/api/workspaces/test/assets/retry.png');
	await expect(page.getByRole('button', { name: 'Create issue', exact: true })).toBeEnabled();
});

for (const replace of ['template', 'reopen'] as const) {
	test(`cancels replaced editor uploads on ${replace} without releasing the new upload guard`, async ({ page }) => {
		const { uploads, issues } = await setup(page);
		await pasteFiles(page.locator('#create-issue-title'), ['old.png', 'never-started.png']);
		await expect.poll(() => uploads.length).toBe(1);
		const aborted = page.waitForEvent('requestfailed', { predicate: (request) => new URL(request.url()).pathname === '/api/workspaces/test/upload' });
		if (replace === 'template') {
			await page.getByRole('button', { name: 'Template', exact: true }).first().click();
			await page.getByRole('button', { name: 'Bug report', exact: true }).click();
		} else {
			await page.keyboard.press('Escape');
			await expect(page.locator('#create-issue-title')).toHaveCount(0);
			await page.getByRole('button', { name: 'New issue', exact: true }).click();
			await page.locator('#create-issue-title').fill('New issue');
		}
		await aborted;
		await pasteFiles(page.locator('#create-issue-title'), ['new.png']);
		await expect.poll(() => uploads.length).toBe(2);
		await expect(page.getByRole('button', { name: 'Uploading attachments…' })).toBeDisabled();
		await page.locator('#create-issue-title').press('Control+Enter');
		expect(issues).toHaveLength(0);
		await completeUpload(uploads[1], 'new.png');
		await expect(page.locator('.tiptap img[src]')).toHaveCount(1);
		await expect(page.locator('.tiptap img[src]')).toHaveAttribute('src', '/api/workspaces/test/assets/new.png');
		await page.getByRole('button', { name: 'Create issue', exact: true }).click();
		await expect.poll(() => issues.length).toBe(1);
		expect(issues[0].description).toContain('/api/workspaces/test/assets/new.png');
		expect(issues[0].description).not.toContain('/api/workspaces/test/assets/old.png');
	});
}

test('inserts picker attachments and dropped images before submitting', async ({ page }) => {
	const { uploads, issues } = await setup(page);
	const picker = page.waitForEvent('filechooser');
	await page.getByRole('button', { name: 'Attach files', exact: true }).click();
	await (await picker).setFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('notes') });
	await expect.poll(() => uploads.length).toBe(1);
	await expect(page.getByRole('button', { name: 'Uploading attachments…' })).toBeDisabled();
	await uploads[0].fulfill({ json: { url: '/api/workspaces/test/assets/notes.txt', filename: 'notes.txt', content_type: 'text/plain' } });
	await expect(page.locator('.tiptap a[href="/api/workspaces/test/assets/notes.txt?download=1"]')).toBeVisible();
	await page.locator('.tiptap').evaluate((element) => {
		const data = new DataTransfer();
		data.items.add(new File(['image'], 'dropped.png', { type: 'image/png' }));
		const rect = element.getBoundingClientRect();
		element.dispatchEvent(new DragEvent('drop', { dataTransfer: data, clientX: rect.x + 20, clientY: rect.y + 20, bubbles: true, cancelable: true }));
	});
	await expect.poll(() => uploads.length).toBe(2);
	await completeUpload(uploads[1], 'dropped.png');
	await expect(page.locator('.tiptap img[src]')).toHaveAttribute('src', '/api/workspaces/test/assets/dropped.png');
	await page.getByRole('button', { name: 'Create issue', exact: true }).click();
	await expect.poll(() => issues.length).toBe(1);
	expect(issues[0].description).toContain('/api/workspaces/test/assets/notes.txt?download=1');
	expect(issues[0].description).toContain('/api/workspaces/test/assets/dropped.png');
});

test('prefills the template issue title while allowing a custom title', async ({ page }) => {
	const { issues } = await setup(page);

	await page.getByRole('button', { name: 'Template', exact: true }).first().click();
	await page.getByRole('button', { name: 'Bug report', exact: true }).click();
	await expect(page.locator('#create-issue-title')).toHaveValue('[Bug] Describe the problem');
	await page.locator('#create-issue-title').fill('Custom issue title');
	await page.getByRole('button', { name: 'Create issue', exact: true }).click();

	await expect.poll(() => issues.length).toBe(1);
	expect(issues[0].title).toBe('Custom issue title');
});

test('keeps the template issue title when creating more issues', async ({ page }) => {
	const { issues } = await setup(page);

	await page.getByRole('button', { name: 'Template', exact: true }).first().click();
	await page.getByRole('button', { name: 'Bug report', exact: true }).click();
	await page.getByRole('switch', { name: 'Create more' }).click();
	await page.locator('#create-issue-title').fill('First custom title');
	await page.getByRole('button', { name: 'Create issue', exact: true }).click();

	await expect.poll(() => issues.length).toBe(1);
	await expect(page.locator('#create-issue-title')).toHaveValue('[Bug] Describe the problem');
	await page.locator('#create-issue-title').fill('Second custom title');
	await page.getByRole('button', { name: 'Create issue', exact: true }).click();

	await expect.poll(() => issues.length).toBe(2);
	expect(issues.map((issue) => issue.title)).toEqual(['First custom title', 'Second custom title']);
});

test('falls back to the default status when a template category is unavailable', async ({ page }) => {
	let releaseStatuses!: () => void;
	const statusGate = new Promise<void>((resolve) => {
		releaseStatuses = resolve;
	});
	const { issues } = await setup(page, {
		templateStatus: 'cancelled',
		openFromList: true,
		statusGate,
		statusesByTeam: {
			[teamId]: [
				{
					id: engStatusId,
					team_id: teamId,
					name: 'Engineering backlog',
					category: 'backlog',
					is_default: true,
					position: 0
				}
			]
		}
	});

	await page.getByRole('button', { name: 'Template', exact: true }).first().click();
	await page.getByRole('button', { name: 'Bug report', exact: true }).click();
	releaseStatuses();
	await expect(page.getByRole('button', { name: 'Engineering backlog', exact: true })).toBeVisible();
	await page.getByRole('button', { name: 'Create issue', exact: true }).click();

	await expect.poll(() => issues.length).toBe(1);
	expect(issues[0].status_id).toBe(engStatusId);
});

test('uses the owning team when applying a team-scoped template', async ({ page }) => {
	const { issues, uploads, statusRequests } = await setup(page, {
		templateTeamId: otherTeamId,
		templateStatus: 'todo',
		extraTeam: otherTeam,
		openFromList: true,
		projects: [{ id: projectId, name: 'Engineering project', team_id: teamId }],
		cycles: [{ id: cycleId, name: 'Engineering cycle', team_id: teamId }],
		statusesByTeam: {
			[teamId]: [{ id: engStatusId, team_id: teamId, name: 'Engineering backlog', category: 'backlog', is_default: true, position: 0 }],
			[otherTeamId]: [{ id: otherStatusId, team_id: otherTeamId, name: 'Operations todo', category: 'unstarted', is_default: true, position: 0 }]
		}
	});

	await page.getByRole('button', { name: 'Project', exact: true }).click();
	await page.getByRole('option', { name: 'Engineering project', exact: true }).click();
	await page.getByRole('button', { name: 'Cycle', exact: true }).click();
	await page.getByRole('option', { name: 'Engineering cycle', exact: true }).click();

	await page.getByRole('button', { name: 'Template', exact: true }).first().click();
	await page.getByRole('button', { name: 'Bug report', exact: true }).click();
	await expect(page.getByRole('button', { name: 'Operations todo', exact: true })).toBeVisible();
	await pasteFiles(page.locator('#create-issue-title'), ['team-template.png']);
	await expect.poll(() => uploads.length).toBe(1);
	expect(new URL(uploads[0].request().url()).searchParams.get('team_id')).toBe(otherTeamId);
	await completeUpload(uploads[0], 'team-template.png');
	await page.locator('#create-issue-title').fill('Operations issue');
	await page.getByRole('button', { name: 'Create issue', exact: true }).click();

	await expect.poll(() => issues.length).toBe(1);
	expect(issues[0].team_id).toBe(otherTeamId);
	expect(issues[0].status_id).toBe(otherStatusId);
	expect(issues[0]).not.toHaveProperty('project_id');
	expect(issues[0]).not.toHaveProperty('cycle_id');
	await expect.poll(() => statusRequests.filter((id) => id === teamId).length).toBeGreaterThanOrEqual(2);
});
