import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';

function machine(id: string, name: string, status: string, extra: Record<string, unknown> = {}) {
	return {
		id,
		workspace_id: workspaceId,
		routing_key: id.replaceAll('-', '').slice(0, 20),
		name,
		status,
		desired_status: status,
		generation: 1,
		repo_provider: 'github',
		repo_owner: 'kuayle',
		repo_name: 'kuayle-platform-monorepo',
		base_branch: 'main',
		working_branch: `kuayle/${name}-with-a-long-working-branch-name`,
		machine_size: 'medium',
		profile: 'developer',
		cpu_millis: 4000,
		memory_mb: 8192,
		disk_gb: 40,
		pids_limit: 1024,
		environment_builder: false,
		created_at: '2026-10-30T10:00:00Z',
		updated_at: '2026-10-30T10:00:00Z',
		...extra
	};
}

const runningId = '00000000-0000-0000-0000-0000000000a1';
const stoppedId = '00000000-0000-0000-0000-0000000000a2';
const spawningId = '00000000-0000-0000-0000-0000000000a3';
const limitedId = '00000000-0000-0000-0000-0000000000a4';
const destroyedId = '00000000-0000-0000-0000-0000000000b1';
const destroyedTwoId = '00000000-0000-0000-0000-0000000000b2';
const failedId = '00000000-0000-0000-0000-0000000000b3';
const expiredId = '00000000-0000-0000-0000-0000000000b4';

const activeMachines = [
	machine(runningId, 'task-3f2b9c1e-8d4a-4b6f-9e21-7c5d0a1b2c3d', 'running'),
	machine(stoppedId, 'task-9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d', 'stopped', {
		pending_cpu_millis: 8000,
		pending_memory_mb: 16384,
		pending_disk_gb: 80
	}),
	machine(spawningId, 'Frontend pairing box', 'tearing_down', { repo_owner: undefined, repo_name: undefined, working_branch: undefined }),
	machine(limitedId, 'task-0b1c2d3e-4f5a-4b6c-8d7e-9f0a1b2c3d4e', 'running', {
		resource_limit_kind: 'memory',
		resource_limit_message: 'The agent container hit its 8 GB memory limit and was restarted by the runtime.',
		resource_limit_at: '2026-10-30T10:05:00Z'
	})
];
const endedMachines = [
	machine(destroyedId, 'task-1d2c3b4a-5f6e-4d8c-9b0a-1f2e3d4c5b6a', 'destroyed', { destroyed_at: '2026-10-30T11:00:00Z' }),
	machine(destroyedTwoId, 'task-7e6d5c4b-3a2f-4e0d-9c8b-7a6f5e4d3c2b', 'destroyed', { destroyed_at: '2026-10-30T11:30:00Z' }),
	machine(failedId, 'task-5b4a3c2d-1e0f-4a9b-8c7d-6e5f4a3b2c1d', 'failed', {
		last_error_code: 'spawn_failed',
		last_error_message: 'Container failed to start'
	}),
	machine(expiredId, 'nightly-integration', 'expired')
];

async function mockApi(page: Page, options: { role?: string; paged?: boolean } = {}) {
	const state = {
		machines: [...activeMachines, ...endedMachines] as Array<ReturnType<typeof machine>>,
		purgePayloads: [] as Array<Record<string, unknown>>
	};
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: '00000000-0000-0000-0000-000000000001', email: 'owner@example.com', name: 'Owner', display_name: 'Owner', avatar_url: null }
			});
		if (path === '/api/preferences') return route.fulfill({ json: { theme_mode: 'dark', dark_theme: 'dark', light_theme: 'light' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test')
			return route.fulfill({ json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: options.role ?? 'owner' } });
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`))
			return route.fulfill({ json: [] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/dev-machines' && request.method() === 'GET') {
			if (!options.paged)
				return route.fulfill({ json: { data: state.machines, total_count: state.machines.length, page: 1, has_more: false } });
			// Active machines fill page 1; ended machines are on page 2.
			const pageNum = Number(new URL(request.url()).searchParams.get('page') ?? '1');
			const pages = [state.machines.filter((entry) => !['destroyed', 'failed', 'expired'].includes(entry.status)), state.machines.filter((entry) => ['destroyed', 'failed', 'expired'].includes(entry.status))];
			const data = pages[pageNum - 1] ?? [];
			return route.fulfill({ json: { data, total_count: state.machines.length, page: pageNum, has_more: pageNum === 1 && pages[1].length > 0 } });
		}
		if (path === '/api/workspaces/test/dev-machines/bulk/permanent-delete' && request.method() === 'POST') {
			const payload = request.postDataJSON() as { machine_ids?: string[] };
			state.purgePayloads.push(payload);
			// The failed machine still holds runtime resources and is kept.
			const removed = (payload.machine_ids ?? []).filter((id) => options.paged || id !== failedId);
			state.machines = state.machines.filter((entry) => !removed.includes(entry.id));
			return route.fulfill({
				json: { count: removed.length, skipped: 1, skipped_active: 0, skipped_runtime: 1, skipped_recent: 0 }
			});
		}
		if (path === '/api/workspaces/test/dev-machine-policy')
			return route.fulfill({
				json: { workspace_id: workspaceId, enabled: true, max_concurrent_machines: 20, max_machines_per_user: 20, max_daily_agent_runs: 25, max_disk_gb: 100, allowed_providers: [], allowed_repositories: [], allow_custom_providers: false, disk_quota_mode: 'hard' }
			});
		if (path === '/api/workspaces/test/github/status') return route.fulfill({ json: { installed: false, repos: [] } });
		if (path === '/api/workspaces/test/dev-machine-scope-setting') return route.fulfill({ json: { workspace_id: workspaceId } });
		if (path === '/api/workspaces/test/dev-machine-environments') return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});
	return state;
}

test('clears destroyed, failed and expired machines at once and reports the ones it kept', async ({ page }) => {
	const state = await mockApi(page);
	await page.goto('/test/settings/dev-machines');

	const ended = page.getByRole('region', { name: 'Destroyed, failed and expired' });
	await expect(ended.getByRole('link', { name: /task-1d2c3b4a/ })).toBeVisible();
	await expect(page.getByRole('region', { name: 'Active machines' }).getByRole('link', { name: /task-1d2c3b4a/ })).toHaveCount(0);

	await ended.getByRole('button', { name: 'Clear' }).click();
	await page.getByRole('button', { name: 'Clear 4 machines' }).click();

	await expect.poll(() => state.purgePayloads.length).toBe(1);
	expect(state.purgePayloads[0]).toEqual({
		machine_ids: [destroyedId, destroyedTwoId, failedId, expiredId],
		older_than_days: 0,
		include_failed: true,
		include_expired: true
	});
	await expect(page.getByText('Removed 3 machines')).toBeVisible();
	await expect(page.getByText('1 kept: a network or volume is still attached; tear it down first.')).toBeVisible();
	await expect(ended.getByRole('link', { name: /task-1d2c3b4a/ })).toHaveCount(0);
	await expect(ended.getByRole('link', { name: /task-5b4a3c2d/ })).toBeVisible();
	await expect(page.getByRole('link', { name: /task-3f2b9c1e/ })).toBeVisible();
});

test('clearing every machine on a later page returns to a page that still has machines', async ({ page }) => {
	await mockApi(page, { paged: true });
	await page.goto('/test/settings/dev-machines');
	await page.getByRole('button', { name: 'Next' }).click();
	const ended = page.getByRole('region', { name: 'Destroyed, failed and expired' });
	await expect(ended.getByRole('link', { name: /task-1d2c3b4a/ })).toBeVisible();
	await ended.getByRole('button', { name: 'Clear' }).click();
	await page.getByRole('button', { name: 'Clear 4 machines' }).click();
	await expect(page.getByText('Removed 4 machines')).toBeVisible();
	await expect(page.getByRole('region', { name: 'Active machines' }).getByRole('link', { name: /task-3f2b9c1e/ })).toBeVisible();
});

test('hides the clear action from members who cannot purge machines', async ({ page }) => {
	await mockApi(page, { role: 'member' });
	await page.goto('/test/settings/dev-machines');
	const ended = page.getByRole('region', { name: 'Destroyed, failed and expired' });
	await expect(ended.getByRole('link', { name: /task-1d2c3b4a/ })).toBeVisible();
	await expect(ended.getByRole('button', { name: 'Clear' })).toHaveCount(0);
});

for (const width of [390, 1280]) {
	test(`keeps machine statuses whole and names truncated at ${width}px`, async ({ page }) => {
		await page.setViewportSize({ width, height: 1100 });
		await mockApi(page);
		await page.goto('/test/settings/dev-machines');
		const rows = page.getByTestId('machine-row');
		await expect(rows).toHaveCount(activeMachines.length + endedMachines.length);
		if (process.env.MACHINE_LIST_SCREENSHOTS)
			await page.screenshot({ path: `${process.env.MACHINE_LIST_SCREENSHOTS}/machines-${width}.png`, fullPage: true });

		const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
		expect(overflow, 'the page does not scroll horizontally').toBeLessThanOrEqual(0);

		for (let index = 0; index < (await rows.count()); index++) {
			const row = rows.nth(index);
			const badge = row.locator('[data-slot="badge"]').first();
			const rowBox = (await row.boundingBox())!;
			const badgeBox = (await badge.boundingBox())!;
			const clipped = await badge.evaluate((element) => element.scrollWidth > element.clientWidth);
			expect(clipped, `status badge in row ${index} is not clipped`).toBe(false);
			expect(badgeBox.x).toBeGreaterThanOrEqual(rowBox.x);
			expect(badgeBox.x + badgeBox.width).toBeLessThanOrEqual(rowBox.x + rowBox.width + 0.5);
			const name = row.getByTestId('machine-name');
			const fullName = await name.getAttribute('title');
			expect(fullName, 'a truncated name keeps its full text as a title').toBeTruthy();
			const nameBox = (await name.boundingBox())!;
			expect(nameBox.x + nameBox.width, 'the name stays clear of the status').toBeLessThanOrEqual(badgeBox.x + 0.5);
		}

		// Statuses line up in one column within each section.
		for (const section of ['Active machines', 'Destroyed, failed and expired']) {
			const badges = page.getByRole('region', { name: section }).locator('[data-testid="machine-row"] [data-slot="badge"]');
			const lefts = await badges.evaluateAll((elements) => elements.map((element) => Math.round(element.getBoundingClientRect().left)));
			expect(new Set(lefts).size, `${section} statuses share a column`).toBe(1);
		}
		await expect(page.getByText('Resize pending')).toBeVisible();
		await expect(page.getByText(/hit its 8 GB memory limit/)).toBeVisible();
	});
}
