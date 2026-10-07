import { expect, test, type Page } from '@playwright/test';
const key = 'kuayle_release_notice_dismissed';
const releaseUrl = 'https://github.com/carbogninalberto/kuayle/releases/tag/v99.0.0';
const release = {
	tag_name: 'v99.0.0',
	html_url: releaseUrl,
	body: '## Improvements\n\nWorkspace updates.',
	published_at: '2026-10-05T00:00:00Z',
	prerelease: false,
	force_upgrade: false,
	minimum_supported_version: null as string | null,
	upgrade_url: releaseUrl
};
function deferred() {
	let resolve!: () => void;
	const promise = new Promise<void>((done) => {
		resolve = done;
	});
	return { promise, resolve };
}
async function setup(page: Page, role: string | null = 'owner') {
	const state = {
		roles: { alpha: role, beta: 'member' } as Record<string, string | null>,
		authenticated: true,
		authGate: null as ReturnType<typeof deferred> | null,
		workspaceGate: null as ReturnType<typeof deferred> | null,
		teamsGate: null as ReturnType<typeof deferred> | null,
		releases: [release],
		teams: [] as { id: string; name: string; key: string }[],
		workspaceRequests: 0,
		releaseRequests: 0
	};
	await page.routeWebSocket('**/api/workspaces/*/ws', () => {});
	await page.route(
		'https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json',
		(route) => {
			state.releaseRequests++;
			return route.fulfill({ json: state.releases });
		}
	);
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path === '/api/auth/me') {
			await state.authGate?.promise;
			return route.fulfill(
				state.authenticated
					? {
							json: {
								id: '00000000-0000-0000-0000-000000000001',
								email: 'test@example.com',
								name: 'Test User',
								display_name: 'Test User',
								avatar_url: null,
								is_sysadmin: true
							}
						}
					: { status: 401, json: { error: { message: 'Unauthorized' } } }
			);
		}
		if (path === '/api/auth/logout') {
			state.authenticated = false;
			return route.fulfill({ json: {} });
		}
		if (path === '/api/workspaces')
			return route.fulfill({
				json: Object.entries(state.roles).map(([slug, role]) => ({
					id: slug,
					slug,
					name: `${slug} workspace`,
					current_user_role: role
				}))
			});
		const match = path.match(/^\/api\/workspaces\/(alpha|beta)$/);
		if (match) {
			state.workspaceRequests++;
			const slug = match[1],
				role = state.roles[slug];
			await state.workspaceGate?.promise;
			return route.fulfill({ json: { id: slug, slug, name: `${slug} workspace`, current_user_role: role } });
		}
		if (path === '/api/system/update-status') return route.fulfill({ json: { enabled: false, running: false } });
		if (path === '/api/preferences')
			return route.fulfill({
				json: {
					font_size: 'default',
					pointer_cursors: true,
					theme_mode: 'dark',
					light_theme: 'light',
					dark_theme: 'dark',
					workflow_sort_mode: 'default',
					workflow_sort_order: [],
					team_workflow_sort_overrides: {},
					issues_group_by: 'status'
				}
			});
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path.endsWith('/teams')) {
			await state.teamsGate?.promise;
			return route.fulfill({ json: state.teams });
		}
		if (/\/workspaces\/(alpha|beta)\/(teams|projects|labels|members|views)$/.test(path))
			return route.fulfill({ json: [] });
		if (path.endsWith('/issues')) return route.fulfill({ json: { issues: [], total: 0 } });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});
	return state;
}
const notice = (page: Page) =>
	page.getByRole('dialog').filter({ has: page.getByRole('heading', { name: 'v99.0.0', exact: true }) });
const dismissed = (page: Page) => page.evaluate((key) => localStorage.getItem(key), key);
async function refresh(page: Page, resource = 'workspace') {
	await page.evaluate(
		(resource) => window.dispatchEvent(new CustomEvent('app:refresh', { detail: { resources: [resource] } })),
		resource
	);
}
// Exercise SvelteKit client navigation without first dismissing the modal.
async function navigate(page: Page, href: string) {
	await page.evaluate((href) => {
		const a = document.createElement('a');
		a.href = href;
		document.body.append(a);
		a.click();
		a.remove();
	}, href);
	await expect(page).toHaveURL(new RegExp(href + '$'));
}
for (const role of ['owner', 'admin']) {
	test(`${role} sees release only after authentication and membership resolve`, async ({ page }) => {
		const state = await setup(page, role);
		state.authGate = deferred();
		state.workspaceGate = deferred();
		await page.goto('/alpha/settings/version');
		await expect.poll(() => state.releaseRequests).toBe(1);
		await expect(notice(page)).toHaveCount(0);
		state.authGate.resolve();
		await expect.poll(() => state.workspaceRequests).toBe(1);
		await expect(notice(page)).toHaveCount(0);
		state.workspaceGate.resolve();
		await expect(notice(page)).toBeVisible();
		await expect(notice(page).getByRole('link', { name: 'Release', exact: true })).toHaveAttribute('href', releaseUrl);
		await expect(notice(page).getByText('Workspace updates.')).toBeVisible();
		if (role === 'owner') {
			await page.screenshot({ path: '../tmp/reports/issue-62-popup-desktop.png' });
			await page.setViewportSize({ width: 390, height: 844 });
			await expect(notice(page)).toBeVisible();
			await page.screenshot({ path: '../tmp/reports/issue-62-popup-mobile.png' });
		}
	});
}
for (const role of ['member', 'guest', null, 'unknown']) {
	test(`suppresses ordinary release for ${role} even with sysadmin access`, async ({ page }) => {
		await setup(page, role);
		await page.goto('/alpha/settings/version');
		await expect(page.getByRole('link', { name: 'Open', exact: true })).toBeVisible();
		await expect(notice(page)).toHaveCount(0);
		expect(await dismissed(page)).toBeNull();
	});
}
test('role refresh closes without dismissal and promotion reopens', async ({ page }) => {
	const state = await setup(page);
	await page.goto('/alpha/settings/version');
	await expect(notice(page)).toBeVisible();
	state.roles.alpha = 'member';
	state.workspaceGate = deferred();
	await refresh(page);
	await expect.poll(() => state.workspaceRequests).toBe(2);
	await expect(notice(page)).toHaveCount(0);
	expect(await dismissed(page)).toBeNull();
	state.workspaceGate.resolve();
	state.workspaceGate = null;
	await expect(notice(page)).toHaveCount(0);
	state.roles.alpha = 'admin';
	await refresh(page, 'members');
	await expect(notice(page)).toBeVisible();
	expect(await dismissed(page)).toBeNull();
});
test('workspace switching never reuses another role or dismisses release', async ({ page }) => {
	const state = await setup(page);
	await page.goto('/alpha/settings/version');
	await expect(notice(page)).toBeVisible();
	state.workspaceGate = deferred();
	await navigate(page, '/beta/settings/version');
	await expect.poll(() => state.workspaceRequests).toBe(2);
	await expect(notice(page)).toHaveCount(0);
	expect(await dismissed(page)).toBeNull();
	state.workspaceGate.resolve();
	state.workspaceGate = null;
	await expect(page.getByRole('link', { name: 'Open', exact: true })).toBeVisible();
	await expect(notice(page)).toHaveCount(0);
	await navigate(page, '/alpha/settings/version');
	await expect(notice(page)).toBeVisible();
});
for (const method of ['button', 'escape', 'close']) {
	test(`intentional ${method} dismissal survives refresh and reload`, async ({ page }) => {
		await setup(page);
		await page.goto('/alpha/settings/version');
		await expect(notice(page)).toBeVisible();
		if (method === 'escape') await page.keyboard.press('Escape');
		else
			await notice(page)
				.getByRole('button', { name: method === 'button' ? 'Dismiss' : 'Close', exact: true })
				.click();
		await expect(notice(page)).toHaveCount(0);
		expect(await dismissed(page)).toBe(release.tag_name);
		await refresh(page);
		await page.reload();
		await expect(page.getByRole('link', { name: 'Open', exact: true })).toBeVisible();
		await expect(notice(page)).toHaveCount(0);
	});
}
test('logout closes an open notice without dismissal', async ({ page }) => {
	const state = await setup(page, 'member');
	await page.goto('/alpha/inbox');
	await page.getByRole('button', { name: 'T', exact: true }).click();
	state.roles.alpha = 'owner';
	await refresh(page);
	await expect(notice(page)).toBeVisible();
	// Complete the logout action while the release dialog is open.
	await page.getByRole('button', { name: 'Log out', exact: true }).dispatchEvent('click');
	await expect(page).toHaveURL(/\/login$/);
	await expect(notice(page)).toHaveCount(0);
	expect(await dismissed(page)).toBeNull();
});
for (const role of ['owner', 'member', 'guest', null]) {
	test(`required upgrade stays blocking for ${role}`, async ({ page }) => {
		const state = await setup(page, role);
		state.releases = [{ ...release, force_upgrade: true, minimum_supported_version: '99.0.0' }];
		await page.goto('/alpha/settings/version');
		const required = page.getByRole('alertdialog');
		await expect(required).toBeVisible();
		await expect(required.getByRole('link', { name: 'Open release' })).toHaveAttribute('href', releaseUrl);
		await page.keyboard.press('Escape');
		await expect(required).toBeVisible();
		await expect(notice(page)).toHaveCount(0);
	});
}
test('required upgrade remains visible without authentication', async ({ page }) => {
	const state = await setup(page);
	state.authenticated = false;
	state.releases = [{ ...release, force_upgrade: true, minimum_supported_version: '99.0.0' }];
	await page.goto('/login');
	await expect(page.getByRole('alertdialog')).toBeVisible();
});

test('a delayed owner response cannot overwrite a newer member refresh', async ({ page }) => {
	const state = await setup(page, 'member');
	await page.goto('/alpha/settings/version');
	await expect(page.getByRole('link', { name: 'Open', exact: true })).toBeVisible();
	state.roles.alpha = 'owner';
	const oldRequest = deferred();
	state.workspaceGate = oldRequest;
	await refresh(page);
	await expect.poll(() => state.workspaceRequests).toBe(2);
	state.workspaceGate = null;
	state.roles.alpha = 'member';
	const newerResponse = page.waitForResponse((r) => r.url().endsWith('/api/workspaces/alpha'));
	await refresh(page);
	await (await newerResponse).finished();
	await expect.poll(() => state.workspaceRequests).toBe(3);
	const oldResponse = page.waitForResponse((r) => r.url().endsWith('/api/workspaces/alpha'));
	oldRequest.resolve();
	await (await oldResponse).finished();
	await page.evaluate(
		() => new Promise<void>((done) => requestAnimationFrame(() => requestAnimationFrame(() => done())))
	);
	await expect(notice(page)).toHaveCount(0);
	expect(await dismissed(page)).toBeNull();
});

test('prerelease preference and version selection remain intact', async ({ page }) => {
	const state = await setup(page);
	state.releases = [
		{
			...release,
			tag_name: 'v100.0.0-beta.1',
			prerelease: true,
			html_url: 'https://github.com/carbogninalberto/kuayle/releases/tag/v100.0.0-beta.1'
		},
		release
	];
	await page.goto('/alpha/settings/version');
	await expect(notice(page)).toBeVisible();
	await notice(page).getByRole('button', { name: 'Show pre-releases' }).click();
	await expect(page.getByRole('dialog').getByRole('heading', { name: 'v100.0.0-beta.1', exact: true })).toBeVisible();
	await page.reload();
	await expect(page.getByRole('dialog').getByRole('heading', { name: 'v100.0.0-beta.1', exact: true })).toBeVisible();
});

test('an older release does not interrupt an owner', async ({ page }) => {
	const state = await setup(page);
	state.releases = [{ ...release, tag_name: 'v0.0.1' }];
	await page.goto('/alpha/settings/version');
	await expect(page.getByRole('link', { name: 'Open', exact: true })).toBeVisible();
	await expect(page.getByRole('dialog')).toHaveCount(0);
});

test('required upgrade discovered from an ordinary popup is not dismissed', async ({ page }) => {
	const state = await setup(page);
	await page.goto('/alpha/settings/version');
	await expect(notice(page)).toBeVisible();
	state.releases = [{ ...release, force_upgrade: true, minimum_supported_version: '99.0.0' }];
	await notice(page).getByRole('button', { name: 'Check again', exact: true }).click();
	await expect(page.getByRole('alertdialog')).toBeVisible();
	await expect(notice(page)).toHaveCount(0);
	expect(await dismissed(page)).toBeNull();
});

test('a failed membership refresh closes the notice until a successful retry', async ({ page }) => {
	await setup(page);
	await page.goto('/alpha/settings/version');
	await expect(notice(page)).toBeVisible();
	await page.route('**/api/workspaces/alpha', (route) =>
		route.fulfill({ status: 503, json: { error: { message: 'Unavailable' } } })
	);
	await refresh(page);
	await expect(notice(page)).toHaveCount(0);
	// A transient server failure is not a revocation; the user stays in place.
	await expect(page).toHaveURL(/\/alpha\/settings\/version$/);
	expect(await dismissed(page)).toBeNull();
	await page.unroute('**/api/workspaces/alpha');
	await refresh(page);
	await expect(notice(page)).toBeVisible();
});

test('a superseded failed membership refresh does not override a successful retry', async ({ page }) => {
	const state = await setup(page);
	await page.goto('/alpha/settings/version');
	await expect(notice(page)).toBeVisible();
	const failure = deferred();
	let failedRequests = 0;
	await page.route('**/api/workspaces/alpha', async (route) => {
		failedRequests++;
		await failure.promise;
		return route.fulfill({ status: 503, json: { error: { message: 'Unavailable' } } });
	});
	await refresh(page);
	await expect.poll(() => failedRequests).toBe(1);
	await expect(notice(page)).toHaveCount(0);
	// Start the retry while the failing request is still in flight, then let the
	// stale failure resolve before the retry. It must not discard the workspace.
	await page.unroute('**/api/workspaces/alpha');
	const retry = deferred();
	state.workspaceGate = retry;
	const before = state.workspaceRequests;
	await refresh(page);
	await expect.poll(() => state.workspaceRequests).toBe(before + 1);
	failure.resolve();
	await page.waitForTimeout(250);
	retry.resolve();
	await expect(notice(page)).toBeVisible();
	await expect(page).toHaveURL(/\/alpha\/settings\/version$/);
	expect(await dismissed(page)).toBeNull();
});

test('overlapping membership refresh does not discard the initial team navigation', async ({ page }) => {
	const state = await setup(page);
	state.teams = [{ id: '00000000-0000-0000-0000-000000000003', name: 'Engineering', key: 'ENG' }];
	const initialMembership = deferred();
	state.workspaceGate = initialMembership;
	await page.goto('/alpha/inbox');
	await expect.poll(() => state.workspaceRequests).toBe(1);
	state.workspaceGate = null;
	state.roles.alpha = 'member';
	const freshResponse = page.waitForResponse((r) => r.url().endsWith('/api/workspaces/alpha'));
	await refresh(page, 'members');
	await (await freshResponse).finished();
	const oldResponse = page.waitForResponse((r) => r.url().endsWith('/api/workspaces/alpha'));
	initialMembership.resolve();
	await (await oldResponse).finished();
	await expect(page.getByText('Engineering', { exact: true })).toBeVisible();
	await expect(notice(page)).toHaveCount(0);
	expect(await dismissed(page)).toBeNull();
});

test('slow teams cannot restore an obsolete owner role in navigation', async ({ page }) => {
	const state = await setup(page);
	state.teamsGate = deferred();
	await page.goto('/alpha/inbox');
	await expect(notice(page)).toBeVisible();
	state.roles.alpha = 'guest';
	const response = page.waitForResponse((r) => r.url().endsWith('/api/workspaces/alpha'));
	await refresh(page, 'members');
	await (await response).finished();
	await expect(notice(page)).toHaveCount(0);
	const teamsResponse = page.waitForResponse((r) => r.url().endsWith('/api/workspaces/alpha/teams'));
	state.teamsGate.resolve();
	await (await teamsResponse).finished();
	await page.evaluate(
		() => new Promise<void>((done) => requestAnimationFrame(() => requestAnimationFrame(() => done())))
	);
	await expect(page.getByRole('button', { name: 'T', exact: true })).toBeVisible();
	await expect(page.locator('a[href="/alpha/machines"]')).toHaveCount(0);
	expect(await dismissed(page)).toBeNull();
});
