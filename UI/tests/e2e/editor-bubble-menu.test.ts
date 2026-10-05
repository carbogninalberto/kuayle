import { test, expect, type Page, type Locator } from '@playwright/test';

const teamId = '00000000-0000-0000-0000-000000000010';
const user = {
	id: '00000000-0000-0000-0000-000000000001',
	name: 'Test User',
	display_name: 'Test User',
	email: 'test@example.com',
	avatar_url: null
};
const workspace = { id: '00000000-0000-0000-0000-000000000002', name: 'Test Workspace', slug: 'test' };
const description =
	'<p>Select this text for formatting.</p><pre><code>const example = true;</code></pre><p><img src="https://assets.example.test/editor-test-image.svg" alt="Example"></p>' +
	Array.from({ length: 45 }, (_, i) => `<p>Description paragraph ${i + 1}.</p>`).join('');

async function openIssue(
	page: Page,
	saveGate: Promise<void> = Promise.resolve(),
	descriptionOverride?: () => string | undefined
) {
	const issues = [1, 2].map((n) => ({
		id: `00000000-0000-0000-0000-00000000002${n}`,
		identifier: `ENG-${n}`,
		title: `Editor regression ${n}`,
		description,
		status: 'todo',
		priority: 0,
		team_id: teamId,
		creator_id: user.id,
		project_id: null,
		cycle_id: null,
		assignee_id: null,
		parent_id: null,
		due_date: null,
		sort_order: n,
		labels: [],
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z'
	}));
	const unexpected: string[] = [];
	const errors: string[] = [];
	page.on('pageerror', (error) => errors.push(error.message));
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('**/editor-test-image.svg', (route) =>
		route.fulfill({
			contentType: 'image/svg+xml',
			body: '<svg xmlns="http://www.w3.org/2000/svg" width="120" height="60"><rect width="120" height="60" fill="green"/></svg>'
		})
	);
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (!path.startsWith('/api/')) return route.continue();
		if (path === '/api/auth/me') return route.fulfill({ json: user });
		if (path === '/api/preferences')
			return route.fulfill({
				json: {
					theme_mode: 'dark',
					dark_theme: 'dark',
					light_theme: 'light',
					font_size: 'default',
					workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'],
					team_workflow_sort_overrides: {}
				}
			});
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/teams')
			return route.fulfill({
				json: [{ id: teamId, name: 'Engineering', key: 'ENG', color: '#6366f1', icon: 'layers' }]
			});
		if (path === '/api/workspaces/test/issues')
			return route.fulfill({ json: { data: issues, total_count: 2, page: 1, has_more: false } });
		const issue = issues.find((item) => path === `/api/workspaces/test/issues/${item.identifier}`);
		if (issue) {
			const incoming = descriptionOverride?.();
			if (incoming !== undefined && route.request().method() === 'GET') issue.description = incoming;
			if (route.request().method() === 'PATCH') {
				await saveGate;
				Object.assign(issue, route.request().postDataJSON());
			}
			return route.fulfill({ json: issue });
		}
		if (path.endsWith('/comments') && route.request().method() === 'POST') {
			return route.fulfill({
				status: 201,
				json: { id: 'comment-1', user_id: user.id, body: route.request().postDataJSON().body }
			});
		}
		if (/\/issues\/ENG-[12]\/github$/.test(path))
			return route.fulfill({ json: { pull_requests: [], branches: [], commits: [] } });
		if (path.endsWith('/issue-copy-prompt')) return route.fulfill({ json: { prompt: '' } });
		if (/\/(projects|labels|members|views|statuses|cycles|comments|history|relations|dev-machines)$/.test(path))
			return route.fulfill({ json: [] });
		unexpected.push(path);
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});
	await page.goto('/test/issue/ENG-1');
	await expect(page.locator('.tiptap')).toHaveCount(2);
	await expect(page.locator('.tiptap').first()).toContainText('Select this text');
	return { unexpected, errors };
}

const menus = (page: Page) => page.getByRole('toolbar', { name: 'Editor formatting' });
const editor = (page: Page) => page.locator('.tiptap').first();

async function scrollDescription(page: Page, top: number) {
	return editor(page).evaluate((element, top) => {
		let parent = element.parentElement;
		while (
			parent &&
			!(parent.scrollHeight > parent.clientHeight && /auto|scroll/.test(getComputedStyle(parent).overflowY))
		)
			parent = parent.parentElement;
		if (!parent) throw new Error('Missing issue scroll container');
		parent.scrollTop = top;
		return parent.scrollTop;
	}, top);
}

async function selectText(target: Locator) {
	await target.scrollIntoViewIfNeeded();
	await target.evaluate((element) => {
		(element.closest('[contenteditable]') as HTMLElement).focus();
		const range = document.createRange();
		range.selectNodeContents(element);
		const selection = window.getSelection()!;
		selection.removeAllRanges();
		selection.addRange(range);
	});
}

for (const width of [1280, 390]) {
	test(`untouched issue editors stay hidden on outer scrolling and resize (${width}px)`, async ({ page }) => {
		await page.setViewportSize({ width, height: 720 });
		const evidence = await openIssue(page);
		await expect(menus(page)).toHaveCount(0);
		expect(await editor(page).evaluate((element) => element.contains(document.activeElement))).toBe(false);
		expect(await page.evaluate(() => window.getSelection()?.isCollapsed ?? true)).toBe(true);
		expect(await scrollDescription(page, 450)).toBeGreaterThan(0);
		await page.waitForTimeout(350);
		await expect(menus(page)).toHaveCount(0);
		await scrollDescription(page, 0);
		// Also exercise the window scroll listener used by the installed plugin.
		await page.evaluate(() => window.dispatchEvent(new Event('scroll')));
		await page.waitForTimeout(350);
		await expect(menus(page)).toHaveCount(0);
		await page.setViewportSize({ width: width - 20, height: 700 });
		await page.waitForTimeout(350);
		await expect(menus(page)).toHaveCount(0);
		expect(evidence).toEqual({ unexpected: [], errors: [] });
	});
}

test('text formatting works and selection clearing or leaving dismisses the menu', async ({ page }) => {
	const evidence = await openIssue(page);
	await selectText(editor(page).locator('p').first());
	await expect(menus(page)).toHaveCount(1);
	const saved = page.waitForResponse(
		(response) => response.request().method() === 'PATCH' && response.url().endsWith('/issues/ENG-1')
	);
	await menus(page).getByTitle('Bold', { exact: true }).click();
	await saved;
	await expect(editor(page).locator('p').first().locator('strong')).toHaveText('Select this text for formatting.');
	await expect(menus(page)).toHaveCount(1);
	await page.keyboard.press('ArrowRight');
	await expect(menus(page)).toHaveCount(0);
	await page.setViewportSize({ width: 1100, height: 700 });
	await expect(menus(page)).toHaveCount(0);
	await selectText(editor(page).locator('p').first());
	await expect(menus(page)).toHaveCount(1);
	await page.getByText('Editor regression 1', { exact: true }).click();
	await expect(menus(page)).toHaveCount(0);
	await page.waitForTimeout(400);
	await expect(menus(page)).toHaveCount(0);
	// Browser selection can be cleared without a ProseMirror transaction.
	await selectText(editor(page).locator('p').first());
	await expect(menus(page)).toHaveCount(1);
	await page.evaluate(() => window.getSelection()?.removeAllRanges());
	await expect(menus(page)).toHaveCount(0);
	expect(evidence).toEqual({ unexpected: [], errors: [] });
});

test('link input keeps the selection and code blocks exclude the menu', async ({ page }) => {
	const evidence = await openIssue(page);
	await selectText(editor(page).locator('p').first());
	await menus(page).getByTitle('Link', { exact: true }).click();
	const input = page.getByPlaceholder('https://...');
	await input.fill('https://example.com');
	// Resizing while focus is inside the menu must preserve the link field.
	await page.setViewportSize({ width: 1100, height: 700 });
	await expect(input).toBeVisible();
	await input.press('Enter');
	await expect(editor(page).locator('a')).toHaveAttribute('href', 'https://example.com');
	await selectText(editor(page).locator('pre code'));
	await page.waitForTimeout(350);
	await expect(menus(page)).toHaveCount(0);
	await page.setViewportSize({ width: 1050, height: 720 });
	await page.waitForTimeout(350);
	await expect(menus(page)).toHaveCount(0);
	expect(evidence).toEqual({ unexpected: [], errors: [] });
});

test('image selection preserves sizing actions and scroll follows the selected text', async ({ page }) => {
	const evidence = await openIssue(page);
	await editor(page).locator('.resizable-image-wrapper').click();
	await expect(menus(page)).toHaveCount(1);
	await menus(page).getByRole('button', { name: '50%', exact: true }).click();
	await expect(editor(page).getByRole('img', { name: 'Example' })).toHaveAttribute('width', '50%');
	await selectText(editor(page).locator('p').filter({ hasText: 'Description paragraph 10.' }));
	await expect(menus(page)).toHaveCount(1);
	const before = await menus(page).boundingBox();
	const top = await scrollDescription(page, 200);
	expect(top).toBe(200);
	await expect.poll(async () => (await menus(page).boundingBox())?.y).not.toBe(before?.y);
	await expect(menus(page)).toHaveCount(1);
	await expect
		.poll(async () => {
			const menu = (await menus(page).boundingBox())!;
			const text = (await editor(page).locator('p').filter({ hasText: 'Description paragraph 10.' }).boundingBox())!;
			return Math.min(Math.abs(menu.y + menu.height - text.y), Math.abs(menu.y - text.y - text.height));
		})
		.toBeLessThan(20);
	await page.keyboard.press('ArrowRight');
	await expect(menus(page)).toHaveCount(0);
	await scrollDescription(page, 0);
	await page.waitForTimeout(350);
	await expect(menus(page)).toHaveCount(0);
	expect(evidence).toEqual({ unexpected: [], errors: [] });
});

test('switching editors and navigating issues leaves no stale toolbar', async ({ page }) => {
	const evidence = await openIssue(page);
	await selectText(editor(page).locator('p').first());
	await expect(menus(page)).toHaveCount(1);
	const comment = page.locator('.tiptap').nth(1);
	await comment.fill('Comment selection');
	await expect(menus(page)).toHaveCount(0);
	await selectText(comment.locator('p'));
	await expect(menus(page)).toHaveCount(1);
	await menus(page).getByTitle('Italic', { exact: true }).click();
	await expect(comment.locator('em')).toHaveText('Comment selection');
	await expect(editor(page).locator('em')).toHaveCount(0);
	await page.getByTitle('Next issue (J)', { exact: true }).click();
	await expect(page).toHaveURL(/\/issue\/ENG-2$/);
	await expect(page.getByText('Editor regression 2', { exact: true })).toBeVisible();
	await page.waitForTimeout(400);
	await expect(menus(page)).toHaveCount(0);
	await scrollDescription(page, 400);
	await page.setViewportSize({ width: 1100, height: 700 });
	await page.waitForTimeout(350);
	await expect(menus(page)).toHaveCount(0);
	await selectText(editor(page).locator('p').first());
	await expect(menus(page)).toHaveCount(1);
	await page.getByTitle('Previous issue (K)', { exact: true }).click();
	await expect(page).toHaveURL(/\/issue\/ENG-1$/);
	await page.waitForTimeout(400);
	await expect(menus(page)).toHaveCount(0);
	expect(evidence).toEqual({ unexpected: [], errors: [] });
});

test('destroying an editor with selection and resize callbacks pending cannot revive its menu', async ({ page }) => {
	const evidence = await openIssue(page);
	await editor(page)
		.locator('p')
		.first()
		.dblclick({ position: { x: 15, y: 8 } });
	// Navigate before the selection debounce and resize callbacks complete.
	await page.evaluate(() => {
		window.dispatchEvent(new Event('resize'));
		(document.querySelector('button[title="Next issue (J)"]') as HTMLButtonElement).click();
	});
	await expect(page).toHaveURL(/\/issue\/ENG-2$/);
	await expect(page.getByText('Editor regression 2', { exact: true })).toBeVisible();
	await page.waitForTimeout(400);
	await expect(menus(page)).toHaveCount(0);
	await editor(page)
		.locator('p')
		.first()
		.dblclick({ position: { x: 15, y: 8 } });
	await expect(menus(page)).toHaveCount(1);
	await menus(page).getByTitle('Bold', { exact: true }).click();
	await expect(editor(page).locator('strong')).toHaveText('Select');
	expect(evidence).toEqual({ unexpected: [], errors: [] });
});

test('submitting a comment remounts an empty editor without a stale menu', async ({ page }) => {
	const evidence = await openIssue(page);
	const comment = page.locator('.tiptap').nth(1);
	await comment.fill('Comment to submit');
	await selectText(comment.locator('p'));
	await expect(menus(page)).toHaveCount(1);
	await page.getByTitle('Send (Ctrl+Enter)', { exact: true }).click();
	await expect(comment).toHaveText('');
	await page.waitForTimeout(400);
	await expect(menus(page)).toHaveCount(0);
	await page.setViewportSize({ width: 1100, height: 700 });
	await page.waitForTimeout(350);
	await expect(menus(page)).toHaveCount(0);
	await comment.fill('New comment');
	await selectText(comment.locator('p'));
	await expect(menus(page)).toHaveCount(1);
	expect(evidence).toEqual({ unexpected: [], errors: [] });
});

test('leaving the link field for non-focusable page chrome dismisses formatting', async ({ page }) => {
	const evidence = await openIssue(page);
	await selectText(editor(page).locator('p').first());
	await menus(page).getByTitle('Link', { exact: true }).click();
	await page.getByPlaceholder('https://...').fill('https://example.com');
	await page.mouse.click(700, 20);
	await expect(menus(page)).toHaveCount(0);
	await page.waitForTimeout(400);
	await expect(menus(page)).toHaveCount(0);
	expect(evidence).toEqual({ unexpected: [], errors: [] });
});

test('formatting then immediately opening the link field preserves unsaved text and selection', async ({ page }) => {
	let releaseSave!: () => void;
	const saveGate = new Promise<void>((resolve) => {
		releaseSave = resolve;
	});
	const evidence = await openIssue(page, saveGate);
	const firstSaveRequest = page.waitForRequest(
		(request) => request.method() === 'PATCH' && request.url().endsWith('/issues/ENG-1')
	);
	const firstSaveResponse = page.waitForResponse(
		(response) => response.request().method() === 'PATCH' && response.url().endsWith('/issues/ENG-1')
	);
	await selectText(editor(page).locator('p').first());
	await menus(page).getByTitle('Bold', { exact: true }).click();
	await menus(page).getByTitle('Link', { exact: true }).click();
	const input = page.getByPlaceholder('https://...');
	await input.fill('https://example.com');
	await firstSaveRequest;
	await expect(editor(page).locator('strong')).toHaveText('Select this text for formatting.');
	await expect(input).toBeVisible();
	releaseSave();
	await firstSaveResponse;
	await expect(input).toBeVisible();
	const linkSaveResponse = page.waitForResponse(
		(response) => response.request().method() === 'PATCH' && response.url().endsWith('/issues/ENG-1')
	);
	await input.press('Enter');
	await expect(editor(page).locator('strong a, a strong')).toHaveText('Select this text for formatting.');
	await page.mouse.click(700, 20);
	await expect(menus(page)).toHaveCount(0);
	await expect(editor(page).locator('strong a, a strong')).toHaveText('Select this text for formatting.');
	await linkSaveResponse;
	await page.reload();
	await expect(editor(page).locator('strong a, a strong')).toHaveText('Select this text for formatting.');
	expect(evidence).toEqual({ unexpected: [], errors: [] });
});

test('external description changes still synchronize and defer while the editor is focused', async ({ page }) => {
	let incoming: string | undefined;
	let writes = 0;
	page.on('request', (request) => {
		if (request.method() === 'PATCH') writes++;
	});
	const evidence = await openIssue(page, Promise.resolve(), () => incoming);
	incoming = '<p>Remote description</p>';
	await page.evaluate(() =>
		window.dispatchEvent(new CustomEvent('ws:issue-updated', { detail: { identifier: 'ENG-1' } }))
	);
	await expect(editor(page)).toHaveText('Remote description');
	await selectText(editor(page).locator('p'));
	await expect(menus(page)).toHaveCount(1);
	incoming = '<p>Second remote description</p>';
	const refreshed = page.waitForResponse(
		(response) => response.request().method() === 'GET' && response.url().endsWith('/issues/ENG-1')
	);
	await page.evaluate(() =>
		window.dispatchEvent(new CustomEvent('ws:issue-updated', { detail: { identifier: 'ENG-1' } }))
	);
	await refreshed;
	await expect(editor(page)).toHaveText('Remote description');
	await page.mouse.click(700, 20);
	await expect(editor(page)).toHaveText('Second remote description');
	await expect(menus(page)).toHaveCount(0);
	expect(writes).toBe(0);
	expect(evidence).toEqual({ unexpected: [], errors: [] });
});

test('the first formatting menu is positioned above its selected text before any scroll', async ({ page }) => {
	const evidence = await openIssue(page);
	await selectText(editor(page).locator('p').first());
	await expect(menus(page)).toHaveCount(1);
	await expect
		.poll(async () => {
			const menu = (await menus(page).boundingBox())!;
			const selected = await page.evaluate(() => {
				const range = window.getSelection()!.getRangeAt(0);
				const rect = range.getBoundingClientRect();
				let pane = range.startContainer.parentElement;
				while (pane && !/auto|scroll/.test(getComputedStyle(pane).overflowY)) pane = pane.parentElement;
				if (!pane) throw new Error('Missing clipping pane');
				const clip = pane.getBoundingClientRect();
				return { x: rect.x, y: rect.y, width: rect.width, left: clip.left, right: clip.right };
			});
			// Floating UI shifts the toolbar inside the issue pane to avoid sidebar clipping.
			const expectedLeft = Math.max(
				selected.left,
				Math.min(selected.right - menu.width, selected.x + selected.width / 2 - menu.width / 2)
			);
			return Math.max(Math.abs(menu.y + menu.height + 8 - selected.y), Math.abs(menu.x - expectedLeft));
		})
		.toBeLessThan(4);
	expect(evidence).toEqual({ unexpected: [], errors: [] });
});
