import { test, expect, type Locator, type Page } from '@playwright/test';
import type { PipelineDefinition, WorkflowIssue } from '../../src/lib/api/workflows';

// The pipeline editor (KUA-284), with the server's pipelines on: a pipeline
// is built and saved through the workflow save route with the validator's
// issues shown where they are fixed, and a blank pipeline and the pipeline
// templates (KUA-290) are created from the new-workflow dialog. Graph
// workflows were removed: only pipelines are offered, and the built-in
// Standard flow is read only.
const time = '2026-11-25T00:00:00Z';

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
const profiles = [
	profile('p-impl', 'Implementer', 'changes_code'),
	profile('p-rev', 'Reviewer', 'reviews'),
	profile('p-sec', 'Security', 'reviews')
];

const team = (): PipelineDefinition => ({
	kind: 'pipeline',
	phases: [
		{ id: 'implement', label: 'Implement', owner: 'implementer' },
		{
			id: 'pr',
			label: 'Pull request',
			owner: 'kuayle',
			checks: [{ id: 'pr-exists', fact: 'pr_exists', required: true }]
		},
		{
			id: 'review',
			label: 'Review',
			owner: 'implementer',
			checks: [{ id: 'reviewer', fact: 'verdict', role: 'reviewer', required: true }]
		},
		{ id: 'approval', label: 'Approval', owner: 'human', checks: [{ id: 'owner', fact: 'approved', required: true }] },
		{ id: 'merge', label: 'Merge', owner: 'kuayle', guarded: true }
	],
	roster: [
		{ role: 'implementer', profile_id: 'p-impl', writer: true },
		{ role: 'reviewer', profile_id: 'p-rev' }
	],
	autonomy: { approve: 'human' },
	budget: { max_turns: 40, max_minutes: 2880, on_exhausted: 'ask' }
});

// The built-in Standard flow's drawn graph, as the server still lists it.
const standardGraph = {
	nodes: [
		{ id: 'start', type: 'start', label: 'Start', position: { x: 0, y: 0 } },
		{ id: 'implement', type: 'agent', label: 'Implement', position: { x: 200, y: 0 } },
		{ id: 'done', type: 'end', label: 'Done', outcome: 'done', position: { x: 400, y: 0 } }
	],
	edges: [],
	budget: {}
};
const standardDescription =
	'Implement, validate, publish a pull request and wait for human review, with follow-ups on CI failures, change requests and comments.';

const summary = (id: string, name: string, graph: unknown, extra: Record<string, unknown> = {}) => ({
	id,
	name,
	description: '',
	builtin: false,
	customized: false,
	latest_version: 1,
	version_id: `${id}-v1`,
	archived: false,
	graph,
	updated_at: time,
	...extra
});

// What the validator answers: the issues the real one would report for the
// mistakes these tests make, located as it locates them.
function validate(graph: PipelineDefinition): WorkflowIssue[] {
	const issues: WorkflowIssue[] = [];
	if (graph.roster.some((m) => !/^[a-z][a-z0-9_-]{0,31}$/.test(m.role)))
		issues.push({ code: 'invalid_role', message: 'roles use lowercase letters' });
	for (const phase of graph.phases)
		for (const check of phase.checks ?? [])
			if (check.fact === 'script' && !check.script?.trim())
				issues.push({
					code: 'invalid_check',
					node_id: phase.id,
					message: `${phase.label}: a script check runs a command`
				});
	return issues;
}

type Default = { scope: string; scope_id: string; workflow_id: string | null };
type Options = {
	pipelines?: boolean;
	defaults?: Default[];
};

async function fixture(page: Page, options: Options = {}) {
	const standard = () =>
		summary('standard', 'Standard', standardGraph, {
			builtin: true,
			latest_version: 0,
			version_id: null,
			description: standardDescription,
			updated_at: undefined
		});
	const state = {
		writes: [] as { method: string; path: string; body: Record<string, unknown> }[],
		created: null as Record<string, unknown> | null,
		defaults: options.defaults ?? ([] as Default[])
	};
	const list = () => ({
		workflows: [
			standard(),
			summary('team', 'Team', team())
		],
		defaults: state.defaults,
		can_manage: true,
		pipelines_enabled: options.pipelines ?? true
	});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const method = request.method();
		const w = '/api/workspaces/test';
		const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user-1', name: 'Owner', email: 'owner@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === w) return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === `${w}/agent-profiles`)
			return route.fulfill({
				json: {
					profiles,
					can_manage: true,
					harnesses: ['claude-code'],
					behaviours: ['plans', 'changes_code', 'reviews']
				}
			});
		if (path === `${w}/workflows/validate`)
			return route.fulfill({ json: { issues: validate(request.postDataJSON().graph), warnings: [] } });
		if (method !== 'GET' && path.startsWith(`${w}/workflows`))
			state.writes.push({ method, path, body: request.postDataJSON() ?? {} });
		if (path === `${w}/workflows`) {
			if (method === 'POST') {
				const body = request.postDataJSON();
				// The server builds a pipeline template's pipeline; any one shows here.
				const graph = String(body.template ?? '').endsWith('_pipeline') ? team() : body.graph;
				state.created = summary('new-1', body.name, graph, { description: body.description });
				return route.fulfill({ status: 201, json: state.created });
			}
			return route.fulfill({ json: list() });
		}
		if (path === `${w}/workflow-defaults` && method === 'PUT') {
			const body = request.postDataJSON();
			state.writes.push({ method, path, body });
			state.defaults = [...state.defaults.filter((d) => d.scope !== body.scope || d.scope_id !== body.scope_id), body];
			return route.fulfill({ json: list() });
		}
		if (path === `${w}/workflows/team`) {
			if (method === 'PUT')
				return route.fulfill({ json: summary('team', 'Team', request.postDataJSON().graph, { latest_version: 2 }) });
			return route.fulfill({ json: { ...summary('team', 'Team', team()), versions: [] } });
		}
		if (path === `${w}/workflows/standard`) return route.fulfill({ json: { ...standard(), versions: [] } });
		if (path === `${w}/workflows/new-1` && state.created)
			return route.fulfill({ json: { ...state.created, versions: [] } });
		return route.fulfill({ json: [] });
	});
	return state;
}

// Picks an option of the select named field inside scope; the options open in a portal.
async function choose(scope: Locator, field: string, option: string) {
	await scope.getByRole('combobox', { name: field, exact: true }).click();
	await scope.page().getByRole('option', { name: option, exact: true }).click();
}

test('a pipeline is built in the editor and saved with the server issues shown inline', async ({ page }) => {
	const state = await fixture(page);
	await page.goto('/test/settings/workflows/team');
	const editor = page.getByTestId('pipeline-editor');
	await expect(editor).toBeVisible();
	await expect(page.getByTestId('pipeline-view')).toHaveCount(0);

	// The phases in a row on the canvas, joined without loop edges or labels.
	for (const id of ['implement', 'pr', 'review', 'approval', 'merge'])
		await expect(editor.getByTestId(`workflow-node-${id}`)).toBeVisible();
	await expect(editor.getByTestId('workflow-node-review')).toContainText('implementer · Checks: 1');
	await expect(editor.locator('.svelte-flow__edge')).toHaveCount(4);
	await expect(editor.locator('.svelte-flow__edge-label')).toHaveCount(0);

	// Clicking a phase on the canvas opens its form.
	await editor.getByTestId('workflow-node-pr').click();
	const form = editor.getByTestId('pipeline-phase-form');
	await expect(form.getByLabel('Name', { exact: true })).toHaveValue('Pull request');

	// A command check without its command: the validator's issue shows on the phase.
	await editor.getByTestId('pipeline-phase-item-review').click();
	await expect(form.getByLabel('Name', { exact: true })).toHaveValue('Review');
	await form.getByTestId('pipeline-add-check').click();
	const added = form.getByTestId('pipeline-check-ci-green');
	await expect(added).toBeVisible();
	await choose(added, 'Requires', 'A command exits 0');
	await expect(form.getByTestId('pipeline-phase-issues')).toContainText('a command check needs a command');
	await expect(editor.getByTestId('pipeline-problems-trigger')).toBeVisible();
	await added.getByLabel('Command').fill('npm test');
	await expect(form.getByTestId('pipeline-phase-issues')).toHaveCount(0);
	await added.getByTestId('check-ci-green-required').getByText('Advisory').click();
	await expect(added.getByRole('combobox', { name: 'Who may waive' })).toHaveCount(0);

	// A new phase goes before the guarded merge.
	await editor.getByTestId('pipeline-phase-item-approval').click();
	await editor.getByTestId('pipeline-add-phase').click();
	await form.getByLabel('Name', { exact: true }).fill('Security review');
	await expect(editor.getByTestId('workflow-node-phase')).toContainText('Security review');

	// The roster: a third role, played by the Security profile; a bad name is reported on the tab.
	await editor.getByTestId('pipeline-tab-roles').click();
	const roster = editor.getByTestId('pipeline-roster');
	await roster.getByTestId('pipeline-add-role').click();
	await page.getByRole('menuitem', { name: /Security/ }).click();
	const role = roster.getByTestId('pipeline-role-2');
	await role.getByLabel('Role', { exact: true }).fill('Security');
	await expect(roster.getByTestId('pipeline-roster-issues')).toContainText('Role names use lowercase letters');
	await role.getByLabel('Role', { exact: true }).fill('security');
	await expect(roster.getByTestId('pipeline-roster-issues')).toHaveCount(0);
	await role.getByRole('switch', { name: 'On call' }).click();

	// The new phase requires the security role's verdict, waivable by a person.
	await editor.getByTestId('pipeline-tab-phases').click();
	await editor.getByTestId('pipeline-phase-item-phase').click();
	await form.getByTestId('pipeline-add-check').click();
	const verdict = form.getByTestId('pipeline-check-ci-green-2');
	await choose(verdict, 'Requires', 'A role approves');
	await choose(verdict, 'From role', 'security');

	// Settings: a smaller budget.
	await editor.getByTestId('pipeline-tab-settings').click();
	await editor.getByLabel('Maximum turns').fill('25');

	await editor.getByTestId('pipeline-save').click();
	await expect(page.getByText('Saved version 2')).toBeVisible();
	expect(state.writes.map((write) => `${write.method} ${write.path}`)).toEqual([
		'PUT /api/workspaces/test/workflows/team'
	]);
	const saved = state.writes[0].body;
	expect(saved.expected_version).toBe(1);
	expect(saved.graph).toEqual({
		kind: 'pipeline',
		phases: [
			{ id: 'implement', label: 'Implement', owner: 'implementer' },
			{
				id: 'pr',
				label: 'Pull request',
				owner: 'kuayle',
				checks: [{ id: 'pr-exists', fact: 'pr_exists', required: true }]
			},
			{
				id: 'review',
				label: 'Review',
				owner: 'implementer',
				checks: [
					{ id: 'reviewer', fact: 'verdict', role: 'reviewer', required: true },
					{ id: 'ci-green', fact: 'script', script: 'npm test' }
				]
			},
			{
				id: 'approval',
				label: 'Approval',
				owner: 'human',
				checks: [{ id: 'owner', fact: 'approved', required: true }]
			},
			{
				id: 'phase',
				label: 'Security review',
				owner: 'implementer',
				checks: [{ id: 'ci-green-2', fact: 'verdict', role: 'security', required: true }]
			},
			{ id: 'merge', label: 'Merge', owner: 'kuayle', guarded: true }
		],
		roster: [
			{ role: 'implementer', profile_id: 'p-impl', writer: true },
			{ role: 'reviewer', profile_id: 'p-rev' },
			{ role: 'security', profile_id: 'p-sec', on_call: true }
		],
		autonomy: { approve: 'human' },
		budget: { max_turns: 25, max_minutes: 2880, on_exhausted: 'ask' }
	});
});

test('a blank pipeline is created from the new-workflow dialog', async ({ page }) => {
	const state = await fixture(page);
	await page.goto('/test/settings/workflows');
	await page.getByTestId('workflow-new').click();
	const dialog = page.getByTestId('new-workflow-form');
	await dialog.getByLabel('Name').fill('Team flow');
	await choose(dialog, 'Start from', 'Blank pipeline (experimental)');
	await dialog.getByRole('button', { name: 'Create' }).click();
	await expect(page).toHaveURL(/\/test\/settings\/workflows\/new-1$/);
	await expect(page.getByTestId('pipeline-editor')).toBeVisible();
	expect(state.writes[0].body).toEqual({
		name: 'Team flow',
		description: '',
		graph: {
			kind: 'pipeline',
			phases: [{ id: 'implement', label: 'Implement', owner: 'implementer' }],
			roster: [{ role: 'implementer', profile_id: 'p-impl', writer: true }]
		}
	});
});

// The pipeline versions of the graph templates (KUA-290) are built by the
// server from the template's name; each opens in the pipeline editor.
for (const [option, template] of [
	['Review loop (pipeline)', 'review_loop_pipeline'],
	['Full cycle (pipeline)', 'full_cycle_pipeline'],
	['Standard (pipeline)', 'standard_pipeline']
] as const) {
	test(`a workflow is created from the ${option} template`, async ({ page }) => {
		const state = await fixture(page);
		await page.goto('/test/settings/workflows');
		await page.getByTestId('workflow-new').click();
		const dialog = page.getByTestId('new-workflow-form');
		await dialog.getByLabel('Name').fill('Team flow');
		await choose(dialog, 'Start from', option);
		await expect(dialog.getByTestId('new-workflow-template-hint')).toContainText('Kuayle merges');
		await dialog.getByRole('button', { name: 'Create' }).click();
		await expect(page).toHaveURL(/\/test\/settings\/workflows\/new-1$/);
		await expect(page.getByTestId('pipeline-editor')).toBeVisible();
		expect(state.writes.map((write) => `${write.method} ${write.path}`)).toEqual([
			'POST /api/workspaces/test/workflows'
		]);
		expect(state.writes[0].body).toEqual({ name: 'Team flow', description: '', template });
	});
}

test('the new-workflow dialog offers only pipelines', async ({ page }) => {
	await fixture(page);
	await page.goto('/test/settings/workflows');
	await page.getByTestId('workflow-new').click();
	await page.getByRole('combobox', { name: 'Start from' }).click();
	await expect(page.getByRole('option')).toHaveText([
		'Review loop (pipeline)',
		'Full cycle (pipeline)',
		'Standard (pipeline)',
		'Blank pipeline (experimental)'
	]);
});

// Standard is the built-in flow every task without a workflow runs: it is
// read only, never edited, copied or deleted, and its page describes it.
test('the built-in Standard flow is read only', async ({ page }) => {
	const state = await fixture(page);
	await page.goto('/test/settings/workflows');
	const rows = page.getByTestId('workflow-row');
	await expect(rows).toHaveCount(2);
	const standard = rows.filter({ hasText: 'Standard' });
	await expect(standard.getByText('Built-in')).toBeVisible();
	await expect(standard.getByRole('button')).toHaveCount(0);
	await expect(standard.getByRole('link', { name: 'Edit Standard' })).toHaveCount(0);
	const team = rows.filter({ hasText: 'Team' });
	await expect(team.getByRole('link', { name: 'Edit Team' })).toBeVisible();
	await expect(team.getByRole('button', { name: 'Duplicate Team' })).toBeVisible();
	await expect(team.getByRole('button', { name: 'Delete Team' })).toBeVisible();
	await expect(team.getByRole('button', { name: 'Make Team the default' })).toBeVisible();

	await standard.getByRole('link', { name: 'Open Standard' }).click();
	await expect(page).toHaveURL(/\/test\/settings\/workflows\/standard$/);
	const view = page.getByTestId('standard-workflow-view');
	await expect(view.getByRole('heading', { name: 'Standard' })).toBeVisible();
	await expect(view.getByText('Read only')).toBeVisible();
	await expect(view.getByTestId('standard-workflow-description')).toHaveText(standardDescription);
	await expect(view.getByTestId('standard-workflow-notice')).toContainText('create a pipeline');
	await expect(page.getByTestId('pipeline-editor')).toHaveCount(0);
	await expect(page.getByRole('region', { name: /canvas/i })).toHaveCount(0);
	expect(state.writes).toEqual([]);
});

test('with pipelines off nothing offers to create or copy one, and a pipeline stays read only', async ({ page }) => {
	await fixture(page, { pipelines: false });
	await page.goto('/test/settings/workflows');
	await expect(page.getByTestId('workflow-row')).toHaveCount(2);
	await expect(page.getByTestId('workflow-new')).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Duplicate Team' })).toHaveCount(0);
	await page.goto('/test/settings/workflows/team');
	await expect(page.getByTestId('pipeline-view')).toBeVisible();
	await expect(page.getByTestId('pipeline-editor')).toHaveCount(0);
});

// KUA-291: a pipeline becomes a scope's default like any workflow while the
// server has pipelines on; off, it is not offered, and a scope whose default
// already is one still shows it.
test('a pipeline is chosen as the workspace default only while pipelines are on', async ({ page }) => {
	const state = await fixture(page);
	await page.goto('/test/settings/workflows');
	const defaults = page.getByTestId('workflow-defaults');
	const workspace = defaults.getByRole('combobox', { name: 'Workspace' });
	await expect(workspace).toHaveText('Standard');
	await workspace.click();
	await expect(page.getByRole('option')).toHaveText(['Standard', 'Team']);
	await page.getByRole('option', { name: 'Team' }).click();
	await expect(workspace).toHaveText('Team');
	expect(state.writes).toEqual([
		{
			method: 'PUT',
			path: '/api/workspaces/test/workflow-defaults',
			body: { scope: 'workspace', scope_id: 'workspace-1', workflow_id: 'team' }
		}
	]);
});

test('with pipelines off a default offers no pipeline but still shows the one it has', async ({ page }) => {
	await fixture(page, { pipelines: false });
	await page.goto('/test/settings/workflows');
	const workspace = page.getByTestId('workflow-defaults').getByRole('combobox', { name: 'Workspace' });
	await workspace.click();
	await expect(page.getByRole('option')).toHaveText(['Standard']);
	await page.keyboard.press('Escape');

	await fixture(page, {
		pipelines: false,
		defaults: [{ scope: 'workspace', scope_id: 'workspace-1', workflow_id: 'team' }]
	});
	await page.reload();
	await expect(workspace).toHaveText('Team');
});
