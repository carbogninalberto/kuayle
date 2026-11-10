import { expect, test, type Page } from '@playwright/test';

const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'member' };

async function mockApi(page: Page) {
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: 'user-1', name: 'Test User', display_name: 'Test User', email: 'test@example.com', avatar_url: null }
			});
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
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
					team_workflow_sort_overrides: {}
				}
			});
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		return route.fulfill({ json: [] });
	});
}

test('the MCP settings page gives copy-paste setup for each client', async ({ page, context }) => {
	await context.grantPermissions(['clipboard-read', 'clipboard-write']);
	await mockApi(page);
	await page.goto('/test/settings/tokens');
	await page.getByRole('link', { name: 'MCP server' }).click();
	await expect(page).toHaveURL(/\/test\/settings\/mcp$/);
	await expect(page.getByRole('heading', { name: 'MCP server' })).toBeVisible();

	const url = `${new URL(page.url()).origin}/mcp?workspace=test`;
	await expect(page.getByLabel('Server URL')).toContainText(url);
	await expect(page.getByRole('link', { name: 'Create a token' })).toHaveAttribute('href', '/test/settings/tokens');

	// Claude Code is shown first; its command can be copied as is.
	const command = `claude mcp add --transport http kuayle "${url}" --header "Authorization: Bearer kuayle_pat_YOUR_TOKEN"`;
	const claudeCode = page.getByRole('tabpanel', { name: 'Claude Code' });
	await expect(claudeCode).toContainText(command);
	await claudeCode.getByRole('button', { name: 'Copy' }).click();
	expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(command);

	// The other clients are reachable by keyboard and name their file.
	await page.getByRole('tab', { name: 'Claude Code' }).focus();
	await page.keyboard.press('ArrowRight');
	await expect(page.getByRole('tabpanel', { name: 'Claude Desktop' })).toContainText('claude_desktop_config.json');
	await expect(page.getByRole('tabpanel', { name: 'Claude Desktop' })).toContainText('mcp-remote');
	await page.getByRole('tab', { name: 'Cursor' }).click();
	await expect(page.getByRole('tabpanel', { name: 'Cursor' })).toContainText('~/.cursor/mcp.json');
	await expect(page.getByRole('tabpanel', { name: 'Cursor' })).toContainText('"Authorization": "Bearer kuayle_pat_YOUR_TOKEN"');
	await page.getByRole('tab', { name: 'Codex' }).click();
	await expect(page.getByRole('tabpanel', { name: 'Codex' })).toContainText('~/.codex/config.toml');
	await expect(page.getByRole('tabpanel', { name: 'Codex' })).toContainText('bearer_token_env_var = "KUAYLE_TOKEN"');
	await expect(page.getByRole('tabpanel', { name: 'Codex' })).toContainText('KUAYLE_TOKEN environment variable');
});

test('the MCP settings page fits a phone without horizontal scrolling', async ({ page }) => {
	await page.setViewportSize({ width: 360, height: 740 });
	await mockApi(page);
	await page.goto('/test/settings/mcp');
	await expect(page.getByRole('heading', { name: 'MCP server' })).toBeVisible();
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
