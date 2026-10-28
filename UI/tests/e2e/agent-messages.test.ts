import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const time = '2026-10-28T09:30:00Z';

function message(over: Record<string, unknown>) {
	return {
		id: 'm1',
		from_task_id: 'task-2',
		from_issue_id: 'issue-2',
		from_identifier: 'ENG-2',
		from_title: 'API half',
		to_ref: 'ENG-4',
		to_issue_id: 'issue-4',
		to_task_id: 'task-4',
		to_identifier: 'ENG-4',
		to_title: 'UI half',
		channel: 'internal',
		body: 'The /v2/items endpoint is merged; it returns {id,name}.',
		state: 'queued',
		reject_reason: null,
		held_by_name: null,
		held_at: null,
		released_by_name: null,
		released_at: null,
		dropped_by_name: null,
		dropped_at: null,
		delivered_at: null,
		delivered_run_id: null,
		comment_state: 'none',
		comment_id: null,
		conversation_hold_id: null,
		can_moderate: true,
		created_at: time,
		...over
	};
}

async function mockNavigator(page: Page, moderator = true) {
	const state = {
		messages: [
			message({}),
			message({
				id: 'm2',
				state: 'held',
				channel: 'comment',
				held_by_name: 'Owner',
				conversation_hold_id: 'hold-1',
				body: '<img src=x onerror="window.evil=1"> Please switch to the v2 client.\nSecond line'
			}),
			message({
				id: 'm3',
				state: 'rejected',
				to_ref: 'PRV-9',
				to_issue_id: null,
				to_identifier: null,
				to_title: null,
				reject_reason: 'not a related issue of this task'
			})
		].map((item) => ({ ...item, can_moderate: moderator })),
		holds: [
			{
				id: 'hold-1',
				issue_a_id: 'issue-2',
				issue_b_id: 'issue-4',
				issue_a: 'ENG-2',
				issue_b: 'ENG-4',
				created_by_name: 'Owner',
				held_messages: 1,
				can_moderate: moderator,
				created_at: time
			}
		] as any[],
		queries: [] as string[],
		actions: [] as string[],
		holdRequests: [] as any[],
		released: [] as string[]
	};
	await page.routeWebSocket('**/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		const path = url.pathname;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: 'user-1', email: 'owner@example.com', name: 'Owner', display_name: 'Owner' }
			});
		if (path === '/api/workspaces')
			return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test')
			return route.fulfill({
				json: {
					id: workspaceId,
					name: 'Test Workspace',
					slug: 'test',
					current_user_role: moderator ? 'owner' : 'member'
				}
			});
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/agent-messages') {
			state.queries.push(url.search);
			const filter = url.searchParams.get('state');
			return route.fulfill({
				json: { messages: state.messages.filter((item) => !filter || item.state === filter) }
			});
		}
		if (path === '/api/workspaces/test/agent-message-holds' && request.method() === 'GET')
			return route.fulfill({ json: { holds: state.holds } });
		if (path === '/api/workspaces/test/agent-message-holds' && request.method() === 'POST') {
			state.holdRequests.push(request.postDataJSON());
			return route.fulfill({ status: 201, json: state.holds[0] });
		}
		const hold = path.match(/\/agent-message-holds\/([^/]+)$/);
		if (hold && request.method() === 'DELETE') {
			state.released.push(hold[1]);
			state.holds = [];
			return route.fulfill({ status: 204, body: '' });
		}
		const act = path.match(/\/agent-messages\/([^/]+)\/(hold|release|drop)$/);
		if (act) {
			state.actions.push(`${act[1]}:${act[2]}`);
			const next = { hold: 'held', release: 'queued', drop: 'dropped' }[act[2]]!;
			state.messages = state.messages.map((item) => (item.id === act[1] ? { ...item, state: next } : item));
			return route.fulfill({ json: state.messages.find((item) => item.id === act[1]) });
		}
		return route.fulfill({ json: [] });
	});
	return state;
}

test('the navigator lists every agent message and moderators clear held ones', async ({ page }) => {
	const state = await mockNavigator(page);
	await page.goto('/test/settings/agent-messages');
	await expect(page.getByRole('heading', { name: 'Agent messages' })).toBeVisible();
	const list = page.getByRole('list', { name: 'Agent messages' });
	await expect(list.getByRole('listitem')).toHaveCount(3);
	const held = list.getByRole('listitem', { name: 'Message from ENG-2 to ENG-4' }).filter({ hasText: 'Please switch' });
	await expect(held.getByRole('status')).toHaveText('Held');
	await expect(held.getByText('Held by Owner')).toBeVisible();
	// Agent text is shown escaped, never as markup.
	await expect(held.getByText('<img src=x onerror="window.evil=1">', { exact: false })).toBeVisible();
	expect(await page.evaluate(() => (window as any).evil)).toBeUndefined();
	await expect(list.getByText('Refused: not a related issue of this task')).toBeVisible();
	await expect(list.getByRole('listitem', { name: 'Message from ENG-2 to PRV-9' })).toBeVisible();

	await held.getByRole('button', { name: 'Release', exact: true }).click();
	await expect.poll(() => state.actions).toEqual(['m2:release']);
	const queued = list.getByRole('listitem').first();
	await queued.getByRole('button', { name: 'Hold', exact: true }).click();
	await expect.poll(() => state.actions).toEqual(['m2:release', 'm1:hold']);
	await queued.getByRole('button', { name: 'Hold this conversation' }).click();
	await expect.poll(() => state.holdRequests).toEqual([{ issue_ids: ['issue-2', 'issue-4'] }]);

	const conversations = page.getByRole('region', { name: 'Held conversations' });
	await expect(conversations.getByText('ENG-2 ⇄ ENG-4')).toBeVisible();
	await conversations.getByRole('button', { name: 'Release' }).click();
	await expect.poll(() => state.released).toEqual(['hold-1']);
	await expect(conversations).toHaveCount(0);

	await page.getByRole('combobox', { name: 'State' }).click();
	await page.getByRole('option', { name: 'Refused' }).click();
	await expect.poll(() => state.queries.at(-1)).toBe('?state=rejected');
	await expect(list.getByRole('listitem')).toHaveCount(1);
	await page.getByRole('textbox', { name: 'Issue' }).fill('eng-4');
	await expect.poll(() => state.queries.at(-1)).toBe('?issue=ENG-4&state=rejected');
	await page.getByRole('button', { name: 'Clear' }).click();
	await expect.poll(() => state.queries.at(-1)).toBe('');
});

test('members read the registry without moderation controls', async ({ page }) => {
	await mockNavigator(page, false);
	await page.goto('/test/settings/agent-messages');
	const list = page.getByRole('list', { name: 'Agent messages' });
	await expect(list.getByRole('listitem')).toHaveCount(3);
	await expect(list.getByRole('group', { name: 'Message actions' })).toHaveCount(0);
	await expect(page.getByRole('region', { name: 'Held conversations' }).getByRole('button')).toHaveCount(0);
});

test('the navigator stays compact at phone width in both themes', async ({ page }, testInfo) => {
	await mockNavigator(page);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/settings/agent-messages');
	await expect(page.getByRole('list', { name: 'Agent messages' }).getByRole('listitem')).toHaveCount(3);
	await expect(page.getByRole('combobox', { name: 'State' })).toHaveText('All states');
	for (const dark of [false, true]) {
		await page.evaluate((d) => {
			document.documentElement.classList.toggle('dark', d);
			document.documentElement.setAttribute('data-theme', d ? 'dark' : 'light');
		}, dark);
		const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
		expect(overflow).toBeLessThanOrEqual(0);
		await page.screenshot({
			path: testInfo.outputPath(`agent-messages-390-${dark ? 'dark' : 'light'}.png`),
			fullPage: true
		});
	}
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.screenshot({ path: testInfo.outputPath('agent-messages-desktop.png') });
});
