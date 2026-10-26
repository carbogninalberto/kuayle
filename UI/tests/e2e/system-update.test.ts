import { readFileSync } from 'node:fs';
import { expect, test, type Page, type WebSocketRoute } from '@playwright/test';

type UpdateStatus = Record<string, unknown>;

async function setup(page: Page, status: UpdateStatus = { enabled: true, running: false, state: 'idle' }) {
	const state = { status, running: true, updateRequests: 0, socket: null as WebSocketRoute | null };
	await page.routeWebSocket('**/api/workspaces/*/ws', (ws) => {
		state.socket = ws;
	});
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: 'u1', email: 'admin@example.com', name: 'Admin', display_name: 'Admin', avatar_url: null, is_sysadmin: true }
			});
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: 'alpha', slug: 'alpha', name: 'Alpha', current_user_role: 'owner' }] });
		if (path === '/api/workspaces/alpha')
			return route.fulfill({ json: { id: 'alpha', slug: 'alpha', name: 'Alpha', current_user_role: 'owner' } });
		if (path === '/api/system/update-status') return route.fulfill({ json: state.status });
		if (path === '/api/system/update') {
			state.updateRequests++;
			return route.fulfill({ status: 202, json: { running: true, message: 'System update started' } });
		}
		if (path === '/api/public/system/update-activity') return route.fulfill({ json: { running: state.running } });
		if (path === '/api/preferences') return route.fulfill({ json: {} });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (/\/workspaces\/alpha\/(teams|projects|labels|members|views)$/.test(path)) return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});
	return state;
}

const systemUpdate = (page: Page) => page.locator('#system-update');

test('confirms an update without showing the shell command', async ({ page }) => {
	const state = await setup(page);
	await page.goto('/alpha/settings/version');
	const section = systemUpdate(page);
	await expect(section.getByText('Connected')).toBeVisible();
	await expect(section.getByText('No update has run since the updater started')).toBeVisible();
	await expect(page.getByText('selfhosting/update.sh')).toHaveCount(0);

	state.status = { enabled: true, running: true, state: 'running', started_at: '2026-10-26T10:00:00Z' };
	await section.getByRole('button', { name: 'Run update' }).click();
	const dialog = page.getByRole('dialog', { name: 'Run system update?' });
	await expect(dialog).toBeVisible();
	await expect(dialog.getByText('selfhosting/update.sh')).toHaveCount(0);
	await dialog.getByRole('button', { name: 'Start update' }).click();
	await expect(dialog).toHaveCount(0);
	expect(state.updateRequests).toBe(1);
	await expect(section.getByText('System update is running.').first()).toBeVisible();

	state.status = { enabled: true, running: false, state: 'succeeded', finished_at: '2026-10-26T10:05:00Z' };
	await expect(section.getByText('Completed successfully')).toBeVisible({ timeout: 10_000 });
});

test('shows the failure reason with expandable output', async ({ page }) => {
	await setup(page, {
		enabled: true,
		running: false,
		state: 'failed',
		message: 'Last update failed',
		finished_at: '2026-10-26T10:05:00Z',
		error: 'Release update failed: manifest unknown',
		output: 'Pulling ghcr.io/carbogninalberto/kuayle-api:v9.9.9\nRelease update failed: manifest unknown\n'
	});
	await page.goto('/alpha/settings/version');
	const section = systemUpdate(page);
	await expect(section.getByText('Failed', { exact: true })).toBeVisible();
	await expect(section.getByText('Release update failed: manifest unknown', { exact: true })).toBeVisible();
	const details = section.getByRole('button', { name: 'Failure details' });
	await expect(details).toHaveAttribute('aria-expanded', 'false');
	await expect(section.getByText(/Pulling ghcr\.io/)).toBeHidden();
	await details.press('Enter');
	await expect(details).toHaveAttribute('aria-expanded', 'true');
	await expect(section.getByText(/Pulling ghcr\.io/)).toBeVisible();
});

test('a started update redirects every client to the upgrade page and back', async ({ page }) => {
	const state = await setup(page);
	await page.goto('/alpha/settings/version?tab=1');
	await expect(systemUpdate(page).getByText('Connected')).toBeVisible();
	await expect.poll(() => state.socket).not.toBeNull();

	state.socket!.send(JSON.stringify({ type: 'system.update.started', payload: null }));
	await expect(page).toHaveURL(/\/upgrading\.html\?return=%2Falpha%2Fsettings%2Fversion%3Ftab%3D1$/);
	await expect(page.getByRole('heading', { name: 'Upgrade in progress' })).toBeVisible();

	state.running = false;
	await expect(page).toHaveURL(/\/alpha\/settings\/version\?tab=1$/, { timeout: 10_000 });
});

test('upgrade page logo renders on deep links and ignores foreign return targets', async ({ page }) => {
	const html = readFileSync(new URL('../../static/upgrading.html', import.meta.url), 'utf8');
	// The maintenance proxy answers every path, including /favicon.svg, with this page.
	await page.route('**/*', (route) => route.fulfill({ body: html, contentType: 'text/html' }));
	await page.goto('/alpha/settings/version?return=https://evil.example/');
	const logo = page.locator('.logo');
	await expect(logo).toBeVisible();
	expect(await logo.evaluate((el) => el.getBoundingClientRect().width)).toBe(32);

	await page.getByRole('button', { name: 'Try again' }).click();
	await expect(page).toHaveURL(/\/alpha\/settings\/version\?return=https:\/\/evil\.example\/$/);
});
