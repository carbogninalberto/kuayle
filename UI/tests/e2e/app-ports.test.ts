import { test, expect, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const machineId = '00000000-0000-0000-0000-000000000070';
const userId = '00000000-0000-0000-0000-000000000001';
const time = '2026-10-28T00:00:00Z';
const host = (port: number) => `portsmachine000000001-${port}.machines.example.test`;

type Port = {
	id: string;
	port: number;
	label: string | null;
	source: 'user' | 'agent' | 'task';
	agent_task_id: string | null;
	task_identifier: string | null;
	host: string;
	listening: boolean;
	created_at: string;
};

const appPort = (over: Partial<Port> & { id: string; port: number }): Port => ({
	label: null,
	source: 'user',
	agent_task_id: null,
	task_identifier: null,
	host: host(over.port),
	listening: false,
	created_at: time,
	...over
});

// Mocks the app ports contract of the machine API: list, expose, rename,
// remove and launch, with the documented PORT_* refusals.
async function fixture(page: Page, options: { creator?: string } = {}) {
	const state = {
		ports: [
			appPort({ id: 'port-1', port: 3000, label: 'web', listening: true }),
			appPort({
				id: 'port-2',
				port: 4100,
				label: 'ENG-7',
				source: 'task',
				agent_task_id: 'task-7',
				task_identifier: 'ENG-7'
			}),
			appPort({ id: 'port-3', port: 8080, source: 'agent', listening: true })
		] as Port[],
		listening: [3000, 5173, 8080],
		lists: 0,
		created: [] as { port: number; label?: string }[],
		renamed: [] as { id: string; label: string | null }[],
		removed: [] as string[],
		launches: [] as string[],
		// Launch answers 202 starting this many times before the host is ready.
		startingLaunches: 0,
		reserved: [2222, 7681],
		unhandled: [] as string[]
	};
	const machine = {
		id: machineId,
		workspace_id: workspaceId,
		routing_key: 'portsmachine000000001',
		name: 'Ports machine',
		status: 'running',
		desired_status: 'running',
		generation: 1,
		repo_url: '',
		repo_owner: '',
		repo_name: '',
		base_branch: '',
		working_branch: '',
		machine_size: 'medium',
		cpu_millis: 4000,
		memory_mb: 8192,
		disk_gb: 50,
		pids_limit: 1024,
		max_runtime_minutes: 240,
		keep_running: false,
		environment_builder: true,
		created_by_user_id: options.creator ?? userId,
		created_at: time,
		updated_at: time,
		expires_at: '2099-07-13T04:00:00Z'
	};
	const refuse = (code: string, status: number, message: string) => ({ status, json: { error: { code, message } } });
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page
		.context()
		.route('https://*.machines.example.test/**', (route) =>
			route.fulfill({ contentType: 'text/html', body: '<title>app</title>' })
		);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const method = request.method();
		const path = new URL(request.url()).pathname;
		const machinePath = `/api/workspaces/test/dev-machines/${machineId}`;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: userId, email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null }
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
				json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'owner' }
			});
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`))
			return route.fulfill({ json: [] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/dev-machine-policy')
			return route.fulfill({ json: { workspace_id: workspaceId, enabled: true, disk_quota_mode: 'hard' } });
		if (path === '/api/workspaces/test/agent-model-servers') return route.fulfill({ json: [] });
		if (path === machinePath) return route.fulfill({ json: machine });
		if (
			['checkouts', 'events', 'logs', 'tasks', 'resource-usage', 'services'].some((p) => path === `${machinePath}/${p}`)
		)
			return route.fulfill({ json: [] });
		if (path === `${machinePath}/ports` && method === 'GET') {
			state.lists++;
			return route.fulfill({ json: { ports: state.ports, listening: state.listening, scanned_at: time } });
		}
		if (path === `${machinePath}/ports` && method === 'POST') {
			const input = request.postDataJSON();
			state.created.push(input);
			if (state.ports.some((p) => p.port === input.port))
				return route.fulfill(refuse('PORT_EXISTS', 409, 'port already exposed'));
			if (state.reserved.includes(input.port)) return route.fulfill(refuse('PORT_RESERVED', 422, 'port is reserved'));
			if (state.ports.length >= 10) return route.fulfill(refuse('PORT_LIMIT', 422, 'port limit reached'));
			const created = appPort({
				id: `port-${input.port}`,
				port: input.port,
				label: input.label ?? null,
				listening: state.listening.includes(input.port)
			});
			state.ports = [...state.ports, created].sort((a, b) => a.port - b.port);
			return route.fulfill({ status: 201, json: created });
		}
		const portMatch = path.match(new RegExp(`^${machinePath}/ports/([^/]+)(/launch)?$`));
		if (portMatch) {
			const [, id, launch] = portMatch;
			const target = state.ports.find((p) => p.id === id);
			if (!target) return route.fulfill(refuse('NOT_FOUND', 404, 'not found'));
			if (launch && method === 'POST') {
				state.launches.push(id);
				if (state.startingLaunches > 0) {
					state.startingLaunches--;
					return route.fulfill({ status: 202, json: { status: 'starting', retry_after_seconds: 1 } });
				}
				return route.fulfill({
					status: 201,
					json: {
						status: 'ready',
						launch_url: `https://${target.host}/?ticket=redacted`,
						expires_at: '2099-01-01T00:00:00Z'
					}
				});
			}
			if (method === 'PATCH') {
				const { label } = request.postDataJSON();
				state.renamed.push({ id, label });
				target.label = label;
				return route.fulfill({ json: target });
			}
			if (method === 'DELETE') {
				state.removed.push(id);
				state.ports = state.ports.filter((p) => p.id !== id);
				return route.fulfill({ status: 204 });
			}
		}
		state.unhandled.push(`${method} ${path}`);
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});
	return state;
}

const card = (page: Page) => page.getByRole('region', { name: 'App ports' });

test('lists exposed app ports with their source and listening state', async ({ page }) => {
	const state = await fixture(page);
	await page.goto(`/test/machines/${machineId}`);
	const ports = card(page);
	await expect(ports.getByText('3/10')).toBeVisible();
	const web = ports.getByRole('listitem').filter({ hasText: 'web' });
	await expect(web.getByText(':3000')).toBeVisible();
	await expect(web.getByText('You', { exact: true })).toBeVisible();
	await expect(web.getByText('Listening', { exact: true })).toBeAttached();
	const task = ports.getByRole('listitem').filter({ hasText: ':4100' });
	await expect(task.getByRole('link', { name: 'ENG-7' })).toHaveAttribute('href', '/test/issue/ENG-7');
	await expect(task.getByText('Not listening', { exact: true })).toBeAttached();
	const agent = ports.getByRole('listitem').filter({ hasText: ':8080' });
	await expect(agent.getByText('Agent', { exact: true })).toBeVisible();
	// Only listening ports that are not exposed yet are offered as one-click chips.
	const detected = ports.getByRole('group', { name: 'Detected ports' });
	await expect(detected.getByRole('button')).toHaveCount(1);
	await detected.getByRole('button', { name: 'Expose port 5173' }).click();
	await expect(ports.getByRole('listitem').filter({ hasText: ':5173' })).toBeVisible();
	await expect(detected).toHaveCount(0);
	expect(state.created).toEqual([{ port: 5173 }]);
	expect(state.unhandled).toEqual([]);
});

test('opens a port in a new tab and waits while its host is starting', async ({ page }) => {
	const state = await fixture(page);
	state.startingLaunches = 1;
	await page.goto(`/test/machines/${machineId}`);
	const popupPromise = page.waitForEvent('popup');
	await card(page).getByRole('button', { name: 'Open web' }).click();
	const popup = await popupPromise;
	await expect.poll(() => popup.url(), { timeout: 10_000 }).toContain(host(3000));
	expect(await popup.evaluate(() => window.opener)).toBeNull();
	expect(state.launches).toEqual(['port-1', 'port-1']);
});

test('exposes a typed port and explains refusals', async ({ page }) => {
	const state = await fixture(page);
	await page.goto(`/test/machines/${machineId}`);
	const ports = card(page);
	const port = ports.getByRole('spinbutton', { name: 'Port' });
	const label = ports.getByRole('textbox', { name: 'Label' });
	const expose = ports.getByRole('button', { name: 'Expose', exact: true });
	await port.fill('80');
	await expect(expose).toBeDisabled();
	await port.fill('3000');
	await expose.click();
	await expect(ports.getByRole('alert')).toHaveText('Port 3000 is already exposed.');
	await port.fill('2222');
	await expose.click();
	await expect(ports.getByRole('alert')).toHaveText('Port 2222 is reserved for the machine itself.');
	await port.fill('9000');
	await label.fill('api docs');
	await expose.click();
	await expect(ports.getByRole('alert')).toHaveCount(0);
	await expect(ports.getByRole('listitem').filter({ hasText: 'api docs' })).toBeVisible();
	await expect(port).toHaveValue('');
	expect(state.created.at(-1)).toEqual({ port: 9000, label: 'api docs' });
});

test('stops offering exposure at the port limit', async ({ page }) => {
	const state = await fixture(page);
	state.ports = Array.from({ length: 9 }, (_, i) => appPort({ id: `p${i}`, port: 3000 + i }));
	await page.goto(`/test/machines/${machineId}`);
	const ports = card(page);
	await ports.getByRole('spinbutton', { name: 'Port' }).fill('9000');
	// Another client filled the last slot: the server refusal is shown.
	state.ports.push(appPort({ id: 'p9', port: 3009 }));
	await ports.getByRole('button', { name: 'Expose', exact: true }).click();
	await expect(ports.getByRole('alert')).toHaveText('All 10 ports are in use. Remove one first.');
	await page.reload();
	await expect(ports.getByText('10/10')).toBeVisible();
	await expect(ports.getByRole('spinbutton', { name: 'Port' })).toHaveCount(0);
	await expect(ports.getByText('All 10 ports are in use. Remove one first.')).toBeVisible();
});

test('renames inline and removes a port after confirmation', async ({ page }) => {
	const state = await fixture(page);
	await page.goto(`/test/machines/${machineId}`);
	const ports = card(page);
	await ports.getByRole('button', { name: 'Rename :8080' }).click();
	const input = ports.getByRole('textbox', { name: 'Label for :8080' });
	await input.fill('storybook');
	await input.press('Enter');
	await expect(ports.getByRole('button', { name: 'Open storybook' })).toBeVisible();
	expect(state.renamed).toEqual([{ id: 'port-3', label: 'storybook' }]);
	// Escape abandons an edit without saving.
	await ports.getByRole('button', { name: 'Rename web' }).click();
	await ports.getByRole('textbox', { name: 'Label for :3000' }).fill('other');
	await ports.getByRole('textbox', { name: 'Label for :3000' }).press('Escape');
	await expect(ports.getByRole('button', { name: 'Open web' })).toBeVisible();
	expect(state.renamed).toHaveLength(1);

	await ports.getByRole('button', { name: 'Remove web' }).click();
	const confirm = page.getByRole('alertdialog');
	await expect(confirm).toContainText('web');
	await confirm.getByRole('button', { name: 'Cancel' }).click();
	expect(state.removed).toEqual([]);
	await ports.getByRole('button', { name: 'Remove web' }).click();
	await page.getByRole('alertdialog').getByRole('button', { name: 'Remove' }).click();
	await expect(ports.getByRole('button', { name: 'Open web' })).toHaveCount(0);
	expect(state.removed).toEqual(['port-1']);
});

test('refreshes listening ports while the card is visible', async ({ page }) => {
	await page.clock.install();
	const state = await fixture(page);
	await page.goto(`/test/machines/${machineId}`);
	const ports = card(page);
	await expect(ports.getByRole('button', { name: 'Expose port 5173' })).toBeVisible();
	state.listening = [3000, 5173, 6006, 8080];
	await page.clock.fastForward(11_000);
	await expect(ports.getByRole('button', { name: 'Expose port 6006' })).toBeVisible();
});

test('shows ports read-only to a workspace admin who did not create the machine', async ({ page }) => {
	await fixture(page, { creator: '00000000-0000-0000-0000-000000000099' });
	await page.goto(`/test/machines/${machineId}`);
	const ports = card(page);
	await expect(ports.getByText(':3000')).toBeVisible();
	await expect(ports.getByRole('button', { name: 'Open web' })).toHaveCount(0);
	await expect(ports.getByRole('button', { name: 'Remove web' })).toHaveCount(0);
	await expect(ports.getByRole('spinbutton', { name: 'Port' })).toHaveCount(0);
});

for (const [name, width] of [
	['desktop', 1280],
	['phone', 390]
] as const) {
	test(`fits the ports card at ${name} width`, async ({ page }, testInfo) => {
		await page.setViewportSize({ width, height: 900 });
		await fixture(page);
		await page.goto(`/test/machines/${machineId}`);
		const ports = card(page);
		await expect(ports.getByRole('button', { name: 'Open web' })).toBeVisible();
		const overflow = await ports.evaluate((element) =>
			[element, ...element.querySelectorAll('*')].some((node) => {
				const box = node.getBoundingClientRect();
				const bounds = element.getBoundingClientRect();
				return box.width > 0 && (box.right > bounds.right + 1 || box.left < bounds.left - 1);
			})
		);
		expect(overflow).toBe(false);
		expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
		await ports.scrollIntoViewIfNeeded();
		await ports.screenshot({ path: testInfo.outputPath(`app-ports-${name}.png`) });
	});
}
