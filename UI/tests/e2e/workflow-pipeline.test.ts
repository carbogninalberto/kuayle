import { test, expect, type Page } from '@playwright/test';

// An experimental pipeline (KUA-252) has no editor yet: its page shows it read
// only, with a notice, its phases and its roles, and nothing to save.
const time = '2026-11-24T00:00:00Z';

const profile = (id: string, name: string, behaviour: string) => ({
	id,
	workspace_id: 'workspace-1',
	name,
	role: name,
	behaviour,
	harness: 'claude-code',
	model: '',
	effort: '',
	instructions: '',
	skill_ids: [],
	created_at: time,
	updated_at: time
});

const pipeline = {
	kind: 'pipeline',
	phases: [
		{ id: 'implement', label: 'Implement', owner: 'implementer' },
		{ id: 'pr', label: 'Pull request', owner: 'kuayle', checks: [{ id: 'pr-exists', fact: 'pr_exists', required: true }] },
		{
			id: 'review',
			label: 'Review',
			owner: 'implementer',
			checks: [{ id: 'reviewer', fact: 'verdict', role: 'reviewer', required: true }]
		},
		{ id: 'merge', label: 'Merge', owner: 'kuayle', guarded: true }
	],
	roster: [
		{ role: 'implementer', profile_id: 'p-impl', writer: true },
		{ role: 'reviewer', profile_id: 'p-rev' }
	],
	autonomy: { approve: 'human' },
	budget: { max_turns: 40, max_minutes: 2880, on_exhausted: 'ask' }
};

async function fixture(page: Page) {
	const writes: string[] = [];
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (request.method() !== 'GET' && path.includes('/workflows')) writes.push(`${request.method()} ${path}`);
		const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user-1', name: 'Owner', email: 'owner@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/agent-profiles')
			return route.fulfill({
				json: {
					profiles: [profile('p-impl', 'Implementer', 'changes_code'), profile('p-rev', 'Reviewer', 'reviews')],
					can_manage: true,
					harnesses: ['claude-code'],
					behaviours: ['plans', 'changes_code', 'reviews']
				}
			});
		if (path === '/api/workspaces/test/workflows/team')
			return route.fulfill({
				json: {
					id: 'team',
					name: 'Team',
					description: '',
					builtin: false,
					customized: false,
					latest_version: 1,
					version_id: 'team-v1',
					archived: false,
					graph: pipeline,
					updated_at: time,
					versions: []
				}
			});
		return route.fulfill({ json: [] });
	});
	return writes;
}

test('a pipeline is shown read only with a notice', async ({ page }) => {
	const writes = await fixture(page);
	await page.goto('/test/settings/workflows/team');
	const view = page.getByTestId('pipeline-view');
	await expect(view).toBeVisible();
	await expect(view.getByRole('heading', { name: 'Team' })).toBeVisible();
	await expect(view.getByTestId('pipeline-notice')).toContainText('Pipelines are experimental');
	await expect(view.getByTestId('pipeline-phase-review')).toContainText('verdict · reviewer · required');
	await expect(view.getByTestId('pipeline-phase-merge')).toContainText('4.');
	await expect(view.getByTestId('pipeline-role-implementer')).toContainText('Implementer');
	await expect(view.getByTestId('pipeline-role-implementer')).toContainText('Writes code');
	await expect(view.getByTestId('pipeline-role-reviewer')).not.toContainText('Writes code');
	await expect(page.getByTestId('workflow-editor')).toHaveCount(0);
	await expect(page.getByTestId('workflow-save')).toHaveCount(0);
	await expect(view.getByRole('link', { name: 'Agents workflows' })).toHaveAttribute('href', '/test/settings/workflows');
	expect(writes).toEqual([]);
});
