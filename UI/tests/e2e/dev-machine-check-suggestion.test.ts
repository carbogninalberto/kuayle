import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const repository = { id: '00000000-0000-0000-0000-000000000030', github_repo_id: 1001, full_name: 'kuayle/app', default_branch: 'main', is_active: true };

async function mockApi(page: Page, suggestion: (url: URL) => { status: number; json: unknown }) {
	const role = 'owner';
	const state = { saved: [] as Record<string, unknown>[], suggestions: [] as string[] };
	const policy: Record<string, unknown> = {
		workspace_id: workspaceId,
		enabled: true,
		max_concurrent_machines: 5,
		max_machines_per_user: 2,
		max_daily_agent_runs: 25,
		max_disk_gb: 100,
		allowed_providers: ['opencode'],
		allowed_repositories: [],
		allow_custom_providers: false,
		allow_unattended_automations: false,
		disk_quota_mode: 'hard',
		size_options: {
			cpu_millis: [1000, 2000, 4000, 6000, 8000, 12000, 16000],
			memory_mb: [2048, 4096, 8192, 12288, 16384, 24576, 32768, 49152, 65536],
			disk_gb: Array.from({ length: 41 }, (_, index) => 20 + index * 2)
		}
	};
	await page.routeWebSocket('**/ws', () => {});
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: {
					id: '00000000-0000-0000-0000-000000000001',
					email: 'owner@example.com',
					name: 'Owner',
					display_name: 'Owner',
					avatar_url: null
				}
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
				json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: role }
			});
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`))
			return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/dev-machine-policy') {
			if (request.method() === 'PATCH') {
				const input = request.postDataJSON();
				state.saved.push(input);
				Object.assign(policy, input);
			}
			return route.fulfill({ json: policy });
		}
		if (path === '/api/workspaces/test/github/status')
			return route.fulfill({ json: { configured: true, installed: true, global_app: false, repos: [repository] } });
		if (path === '/api/workspaces/test/dev-machine-check-suggestion') {
			const url = new URL(request.url());
			state.suggestions.push(url.search);
			return route.fulfill(suggestion(url));
		}
		if (path === '/api/workspaces/test/dev-machine-scope-setting') {
			if (request.method() === 'DELETE') return route.fulfill({ status: 204, body: '' });
			return route.fulfill({ json: { workspace_id: workspaceId } });
		}
		if (path === '/api/workspaces/test/dev-machine-environments') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/dev-machines')
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path === '/api/workspaces/test/agent-automations' || path === '/api/workspaces/test/agent-automation-runs')
			return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});
	return state;
}

test('admins fill the check command from a suggestion, then save it', async ({ page }) => {
	const state = await mockApi(page, () => ({
		status: 200,
		json: { command: ['sh', '-c', '(cd BE && go build ./... && go vet ./...) && (cd UI && npm ci && npm run check)'], basis: ['BE/go.mod', 'UI/package.json'] }
	}));
	await page.goto('/test/settings/dev-machines?tab=policy');
	const suggest = page.getByRole('button', { name: 'Suggest a check' });
	await expect(suggest).toBeDisabled();
	await expect(page.getByText('Choose a repository to get a suggested check.')).toBeVisible();

	await page.getByRole('combobox').filter({ hasText: 'No default repository' }).click();
	await page.getByRole('option', { name: 'kuayle/app' }).click();
	await suggest.click();
	const field = page.getByRole('textbox', { name: 'Test command' });
	await expect(field).toHaveValue('["sh","-c","(cd BE && go build ./... && go vet ./...) && (cd UI && npm ci && npm run check)"]');
	await expect(page.getByText('Suggested from BE/go.mod, UI/package.json. Review it, then save.')).toBeVisible();
	expect(state.suggestions).toEqual([`?repository_id=${repository.id}`]);
	expect(state.saved).toEqual([]);
});

test('a suggestion explains when nothing is recognised or GitHub fails', async ({ page }) => {
	let reply = { status: 200, json: null as unknown };
	await mockApi(page, () => reply);
	await page.goto('/test/settings/dev-machines?tab=policy');
	await page.getByRole('combobox').filter({ hasText: 'No default repository' }).click();
	await page.getByRole('option', { name: 'kuayle/app' }).click();
	const field = page.getByRole('textbox', { name: 'Test command' });
	await field.fill('["make","test"]');
	await page.getByRole('button', { name: 'Suggest a check' }).click();
	await expect(page.getByText("No check could be suggested from this repository's files. Enter one yourself.")).toBeVisible();
	await expect(field).toHaveValue('["make","test"]');
	reply = { status: 502, json: { error: { code: 'REPOSITORY_UNREADABLE', message: 'x' } } };
	await page.getByRole('button', { name: 'Suggest a check' }).click();
	await expect(page.getByText('The repository could not be read from GitHub. Try again later.')).toBeVisible();
	await expect(field).toHaveValue('["make","test"]');
});
