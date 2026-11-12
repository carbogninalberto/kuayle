import { test, expect, type Page } from '@playwright/test';

// The workflow editor warns, on the step it concerns, when a planning
// profile is given a step that tells it to implement (KUA-134, seen on
// KUA-122). A warning never holds a save back. Served from mocked API routes
// that warn the way the server does.
const time = '2026-11-10T00:00:00Z';
const shots = process.env.KUA134_SHOTS;

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
const profiles = [profile('p-orch', 'Orchestrator', 'plans'), profile('p-impl', 'Cheap Implementer', 'changes_code')];
const graph = () => ({
	nodes: [
		{ id: 'start', type: 'start', label: 'Start', position: { x: 0, y: 0 } },
		{
			id: 'plan',
			type: 'agent',
			label: 'Implementer',
			profile_id: 'p-orch',
			prompt: 'List the changes needed, then implement them. Run the relevant tests.',
			position: { x: 280, y: 0 }
		},
		{ id: 'code', type: 'agent', label: 'Code', profile_id: 'p-impl', prompt: 'Implement the plan.', position: { x: 560, y: 0 } },
		{ id: 'done', type: 'end', label: 'Done', outcome: 'done', position: { x: 840, y: 0 } }
	],
	edges: [
		{ id: 'e1', source: 'start', target: 'plan', condition: 'else' },
		{ id: 'e2', source: 'plan', target: 'code', condition: 'agent_finished' },
		{ id: 'e3', source: 'code', target: 'done', condition: 'agent_finished' }
	],
	budget: {}
});

// The server's rule, in short: a planning profile whose step is named like an
// implementing role, or whose prompt orders it to implement.
function warningsOf(sent: ReturnType<typeof graph>) {
	const warnings: any[] = [];
	for (const node of sent.nodes) {
		if (node.type !== 'agent' || profiles.find((p) => p.id === node.profile_id)?.behaviour !== 'plans') continue;
		const warn = (what: string) =>
			warnings.push({
				code: 'planning_step_implements',
				node_id: node.id,
				message: `${node.label}: the profile "Orchestrator" only plans, but ${what}`
			});
		if (/\bimplement\w*/i.test(node.label)) warn('the step\'s name says "Implementer"');
		if (/(^|[.,;]\s*)(then\s+)?implement\b/i.test(node.prompt ?? '')) warn('its prompt tells it to "then implement them"');
	}
	return warnings;
}

async function fixture(page: Page) {
	const state = { saves: [] as any[], graph: graph() };
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
		const summary = () => ({
			id: 'cheap',
			name: 'Cheap Workflow',
			description: '',
			builtin: false,
			customized: false,
			latest_version: 4 + state.saves.length,
			version_id: 'cheap-v4',
			archived: false,
			graph: state.graph,
			updated_at: time
		});
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user-1', name: 'Owner', email: 'owner@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/agent-profiles')
			return route.fulfill({
				json: {
					profiles,
					can_manage: true,
					harnesses: ['claude-code'],
					model_harnesses: ['claude-code'],
					model_formats: {},
					behaviours: ['plans', 'changes_code', 'reviews']
				}
			});
		if (path === '/api/workspaces/test/workflows/validate')
			return route.fulfill({ json: { issues: [], warnings: warningsOf(request.postDataJSON().graph) } });
		if (path === '/api/workspaces/test/workflows')
			return route.fulfill({ json: { workflows: [summary()], defaults: [], can_manage: true } });
		if (path === '/api/workspaces/test/workflows/cheap' && request.method() === 'PUT') {
			const input = request.postDataJSON();
			state.saves.push(input);
			state.graph = input.graph;
			return route.fulfill({ json: summary() });
		}
		if (path === '/api/workspaces/test/workflows/cheap') return route.fulfill({ json: { ...summary(), versions: [] } });
		return route.fulfill({ json: [] });
	});
	return state;
}

for (const size of [
	{ name: 'desktop', width: 1440, height: 900 },
	{ name: 'phone', width: 390, height: 844 }
]) {
	test(`${size.name}: a planning step told to implement is warned about on the step, and still saves`, async ({
		page
	}) => {
		const state = await fixture(page);
		await page.setViewportSize({ width: size.width, height: size.height });
		await page.goto('/test/settings/workflows/cheap');
		const editor = page.getByTestId('workflow-editor');
		await expect(editor).toBeVisible();
		const text = 'This profile only plans, but the step tells it to implement';
		// On the step's card, not as a problem to fix: the other steps have none.
		const card = editor.getByTestId('workflow-node-plan');
		await expect(card.getByLabel(text)).toBeVisible();
		await expect(editor.getByTestId('workflow-node-warning-icon')).toHaveCount(1);
		await expect(editor.getByTestId('workflow-problems-trigger')).toHaveCount(0);

		// Selecting the step says it once, although its name and its prompt both earn it.
		await card.click();
		const inspector = editor.getByTestId('workflow-node-inspector');
		const warning = inspector.getByTestId('workflow-node-warning');
		await expect(warning).toHaveText(text);
		if (shots && size.name === 'desktop') await page.screenshot({ path: `${shots}/5-workflow-warning.png` });

		// It does not block the save.
		await page.locator('#workflow-name').fill('Cheap Workflow 5');
		await editor.getByTestId('workflow-save').click();
		await expect.poll(() => state.saves.length).toBe(1);
		await expect(card.getByLabel(text)).toBeVisible();

		// Asking for a plan under another name clears it.
		await inspector.getByLabel('Name', { exact: true }).fill('Plan');
		await expect(warning).toBeVisible();
		const prompt = inspector.getByTestId('workflow-node-prompt').getByRole('textbox');
		await prompt.fill('List the changes needed as a plan for the next agent.');
		await expect(warning).toHaveCount(0);
		await expect(editor.getByTestId('workflow-node-warning-icon')).toHaveCount(0);
	});
}
