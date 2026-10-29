import { test, expect, type Page } from '@playwright/test';

const tools = () => [
	{ name: 'list_teams', description: 'List the teams the user can access.', requires_approval: false, changes_data: false, available: true, enabled: true },
	{ name: 'create_issue', description: 'Create a new issue.', requires_approval: false, changes_data: true, available: true, enabled: true },
	{ name: 'delegate_issue', description: 'Ask the user to approve starting a coding agent.', requires_approval: true, changes_data: false, available: true, enabled: false },
	{ name: 'link_repository', description: 'Set the GitHub repository coding agents use.', requires_approval: false, changes_data: true, available: false, enabled: true }
];

async function fixture(page: Page, role: 'admin' | 'member') {
	const state = { tools: tools(), puts: [] as Array<{ name: string; enabled: boolean }> };
	const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: role };
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		const method = route.request().method();
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user-1', name: 'Ada', display_name: 'Ada', email: 'ada@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/assistant-capabilities')
			return route.fulfill({ json: { tools: state.tools, can_manage: role === 'admin' } });
		const toggle = path.match(/^\/api\/workspaces\/test\/assistant-capabilities\/(\w+)$/);
		if (toggle && method === 'PUT') {
			if (role !== 'admin') return route.fulfill({ status: 403, json: { error: { code: 'FORBIDDEN', message: 'Forbidden' } } });
			const enabled = route.request().postDataJSON().enabled as boolean;
			state.puts.push({ name: toggle[1], enabled });
			const tool = state.tools.find((entry) => entry.name === toggle[1])!;
			tool.enabled = enabled;
			return route.fulfill({ json: tool });
		}
		return route.fulfill({ json: [] });
	});
	return state;
}

test('admins see every tool and switch tools on and off', async ({ page }) => {
	const state = await fixture(page, 'admin');
	await page.goto('/test/settings/assistant');
	await expect(page.getByRole('heading', { name: 'Assistant', exact: true })).toBeVisible();
	await expect(page.getByRole('link', { name: 'Assistant' })).toBeVisible();
	await expect(page.getByRole('tab', { name: 'Tools', exact: true })).toHaveAttribute('aria-selected', 'true');
	await expect(page.getByRole('tab', { name: 'Skills', exact: true })).toBeEnabled();
	await expect(page.getByRole('tab', { name: 'Custom tools', exact: true })).toBeEnabled();

	const list = page.getByRole('list');
	await expect(list.getByRole('listitem')).toHaveCount(4);
	await expect(page.getByText('3 of 4 enabled')).toBeVisible();
	const delegate = list.getByRole('listitem', { name: 'Delegate issue' });
	await expect(delegate.getByText('Approval required')).toBeVisible();
	await expect(delegate.getByText('Ask the user to approve starting a coding agent.')).toBeVisible();
	// Tools without a chat label show their name.
	await expect(list.getByRole('listitem', { name: 'link_repository' }).getByText('Not available on this server')).toBeVisible();
	await expect(list.getByRole('listitem', { name: 'Create issue' }).getByText('Changes data')).toBeVisible();

	const create = page.getByRole('switch', { name: 'Enable Create issue' });
	await expect(create).toBeChecked();
	await create.click();
	await expect.poll(() => state.puts).toEqual([{ name: 'create_issue', enabled: false }]);
	await expect(create).not.toBeChecked();
	await expect(page.getByText('2 of 4 enabled')).toBeVisible();

	const delegateSwitch = page.getByRole('switch', { name: 'Enable Delegate issue' });
	await expect(delegateSwitch).not.toBeChecked();
	await delegateSwitch.click();
	await expect.poll(() => state.puts.at(-1)).toEqual({ name: 'delegate_issue', enabled: true });
	await expect(delegateSwitch).toBeChecked();
	await page.screenshot({ path: test.info().outputPath('assistant-settings-admin.png'), fullPage: true });

	await page.setViewportSize({ width: 390, height: 844 });
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test('members see the tools read-only', async ({ page }) => {
	const state = await fixture(page, 'member');
	await page.goto('/test/settings/assistant');
	await expect(page.getByText('Only workspace owners and admins can turn tools on or off.')).toBeVisible();
	await expect(page.getByRole('listitem')).toHaveCount(4);
	await expect(page.getByRole('switch')).toHaveCount(0);
	await expect(page.getByRole('listitem', { name: 'Delegate issue' }).getByText('Disabled', { exact: true })).toBeVisible();
	await expect(page.getByRole('listitem', { name: 'List teams' }).getByText('Enabled', { exact: true })).toBeVisible();
	expect(state.puts).toEqual([]);
});
