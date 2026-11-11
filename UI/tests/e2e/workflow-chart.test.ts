import { expect, test, type Locator, type Page } from '@playwright/test';

// The workflow chart runs left to right: in Settings → Workflows (a new
// workflow from each template, one saved top to bottom, and "Tidy up" after
// moving steps) and on a task, where it scrolls sideways on a narrow screen.

const time = '2026-11-01T00:00:00Z';
const sizes = [
	{ name: 'desktop', width: 1440, height: 900 },
	{ name: 'phone', width: 390, height: 844 }
];

type Node = Record<string, unknown> & { id: string; type: string; position: { x: number; y: number } };
type Edge = { id: string; source: string; target: string; condition: string };
type Graph = { nodes: Node[]; edges: Edge[]; budget: Record<string, unknown> };

// Where the server places a built-in graph's nodes (workflowAt in
// BE/internal/domain/workflow.go): a column per step, lane 1 under the path.
const at = (column: number, lane = 0, inset = false) => ({
	x: (column > 0 ? 316 + (column - 1) * 380 : 0) + (inset ? 32 : 0),
	y: lane * 120
});
const edges = (list: string[][]): Edge[] =>
	list.map(([source, target, condition]) => ({ id: `${source}-${target}-${condition}`, source, target, condition }));

const templates: Record<string, Graph> = {
	// "Copy of Standard" (standardCopy in BE/internal/service/workflow.go): the
	// built-in graph plus Publish → Failed, which moves Failed under the review.
	standard: {
		nodes: [
			{ id: 'start', type: 'start', label: 'Start', position: at(0) },
			{ id: 'implement', type: 'agent', label: 'Implement', position: at(1) },
			{ id: 'validate', type: 'check', label: 'Validate', check: 'tests', position: at(2) },
			{ id: 'publish', type: 'publish', label: 'Publish pull request', position: at(3) },
			{ id: 'review', type: 'approval', label: 'Human review', prompt: 'Review the pull request', position: at(4) },
			{ id: 'done', type: 'end', label: 'Done', outcome: 'done', position: at(5) },
			{ id: 'failed', type: 'end', label: 'Failed', outcome: 'failed', position: at(4, 1, true) }
		],
		edges: edges([
			['start', 'implement', 'else'],
			['implement', 'validate', 'agent_finished'],
			['implement', 'failed', 'agent_failed'],
			['validate', 'publish', 'checks_passed'],
			['validate', 'publish', 'else'],
			['validate', 'failed', 'checks_failed'],
			['publish', 'review', 'published'],
			['publish', 'failed', 'publish_failed'],
			['review', 'done', 'approval_approved'],
			['review', 'implement', 'approval_denied']
		]),
		budget: {}
	},
	review_loop: {
		nodes: [
			{ id: 'start', type: 'start', label: 'Start', position: at(0) },
			{ id: 'implement', type: 'agent', label: 'Implementer', profile_id: 'p-impl', position: at(1) },
			{ id: 'publish', type: 'publish', label: 'Open pull request', position: at(2) },
			{ id: 'review', type: 'agent', label: 'Reviewer', profile_id: 'p-rev', max_visits: 3, position: at(3) },
			{ id: 'ci', type: 'check', label: 'CI', check: 'ci', timeout_minutes: 120, position: at(4) },
			{ id: 'ship', type: 'approval', label: 'Ready to ship', prompt: 'Ready to ship', position: at(5) },
			{ id: 'done', type: 'end', label: 'Done', outcome: 'done', position: at(6) },
			{ id: 'failed', type: 'end', label: 'Failed', outcome: 'failed', position: at(6, 1) }
		],
		edges: edges([
			['start', 'implement', 'else'],
			['implement', 'publish', 'agent_finished'],
			['implement', 'failed', 'agent_failed'],
			['publish', 'review', 'published'],
			['publish', 'failed', 'publish_failed'],
			['review', 'ci', 'review_approved'],
			['review', 'implement', 'review_changes_requested'],
			['review', 'failed', 'agent_failed'],
			['ci', 'ship', 'checks_passed'],
			['ci', 'implement', 'checks_failed'],
			['ci', 'failed', 'timeout'],
			['ship', 'done', 'approval_approved'],
			['ship', 'failed', 'approval_denied']
		]),
		budget: { max_iterations: 8 }
	},
	full_cycle: {
		nodes: [
			{ id: 'start', type: 'start', label: 'Start', position: at(0) },
			{ id: 'implement', type: 'agent', label: 'Implement', profile_id: 'p-impl', max_visits: 5, position: at(1) },
			{ id: 'tests', type: 'check', label: 'Tests', check: 'tests', position: at(2) },
			{ id: 'publish', type: 'publish', label: 'Open pull request', position: at(3) },
			{ id: 'review', type: 'agent', label: 'Review', profile_id: 'p-rev', max_visits: 6, position: at(4) },
			{ id: 'ci', type: 'check', label: 'CI', check: 'ci', timeout_minutes: 120, position: at(5) },
			{ id: 'ship', type: 'approval', label: 'Ready to ship', prompt: 'Ready to merge?', position: at(6) },
			{ id: 'merge', type: 'merge', label: 'Merge', position: at(7) },
			{ id: 'done', type: 'end', label: 'Done', outcome: 'done', position: at(8) },
			{ id: 'failed', type: 'end', label: 'Failed', outcome: 'failed', position: at(6, 1, true) }
		],
		edges: edges([
			['start', 'implement', 'else'],
			['implement', 'tests', 'agent_finished'],
			['implement', 'implement', 'agent_failed'],
			['tests', 'publish', 'checks_passed'],
			['tests', 'implement', 'checks_failed'],
			['publish', 'review', 'published'],
			['publish', 'implement', 'publish_failed'],
			['review', 'ci', 'review_approved'],
			['review', 'implement', 'review_changes_requested'],
			['review', 'review', 'agent_failed'],
			['ci', 'ship', 'checks_passed'],
			['ci', 'implement', 'checks_failed'],
			['ci', 'failed', 'timeout'],
			['ship', 'merge', 'approval_approved'],
			['ship', 'implement', 'approval_denied'],
			['merge', 'done', 'merged']
		]),
		budget: { max_iterations: 10 }
	}
};

// A graph as workflows were saved before: top to bottom, the failed ending to
// the right.
const vertical = (graph: Graph): Graph => ({
	...graph,
	nodes: graph.nodes.map((node, row) => ({
		...node,
		position: node.outcome === 'failed' ? { x: 320, y: 420 } : { x: 0, y: row * 120 }
	}))
});
// The full cycle (its implementer and reviewer retry themselves) with a
// transition from the tests straight to the go-ahead, past three steps.
const withSkip = (): Graph => ({
	...templates.full_cycle,
	edges: [...templates.full_cycle.edges, ...edges([['tests', 'ship', 'else']])]
});

const profile = (id: string, name: string, behaviour: string, builtin: string) => ({
	id,
	workspace_id: 'workspace-1',
	name,
	role: name,
	behaviour,
	builtin_key: builtin,
	harness: 'codex',
	model: '',
	effort: '',
	instructions: '',
	skill_ids: [],
	created_at: time,
	updated_at: time
});
const profiles = [
	profile('p-impl', 'Implementer', 'changes_code', 'implementer'),
	profile('p-rev', 'Reviewer', 'reviews', 'reviewer')
];

const summary = (id: string, name: string, graph: Graph) => ({
	id,
	name,
	description: '',
	builtin: false,
	customized: false,
	latest_version: 1,
	version_id: `${id}-v1`,
	archived: false,
	graph,
	updated_at: time
});

// Settings → Workflows. Creating from a template answers with the server's
// graph for it; a blank workflow keeps the graph the page sent.
async function mockSettings(page: Page, stored: Record<string, Graph> = {}) {
	const saved = new Map(Object.entries(stored).map(([id, graph]) => [id, summary(id, id, graph)]));
	const saves: Array<{ graph: Graph }> = [];
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
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
					harnesses: ['codex'],
					model_harnesses: ['codex'],
					model_formats: {},
					behaviours: ['plans', 'changes_code', 'reviews']
				}
			});
		if (path === '/api/workspaces/test/workflows/validate') return route.fulfill({ json: { issues: [] } });
		if (path === '/api/workspaces/test/workflows' && request.method() === 'POST') {
			const input = request.postDataJSON();
			const id = `wf-${saved.size + 1}`;
			const created = summary(id, input.name, input.template ? templates[input.template] : input.graph);
			saved.set(id, created);
			return route.fulfill({ status: 201, json: created });
		}
		if (path === '/api/workspaces/test/workflows')
			return route.fulfill({ json: { workflows: [...saved.values()], defaults: [], can_manage: true } });
		const one = path.match(/^\/api\/workspaces\/test\/workflows\/([^/]+)$/);
		if (one && request.method() === 'PUT') {
			const input = request.postDataJSON();
			saves.push(input);
			const next = { ...saved.get(one[1])!, graph: input.graph, latest_version: 2 };
			saved.set(one[1], next);
			return route.fulfill({ json: next });
		}
		if (one && saved.has(one[1])) return route.fulfill({ json: { ...saved.get(one[1]), versions: [] } });
		return route.fulfill({ json: [] });
	});
	return { saves };
}

type Box = { x: number; y: number; width: number; height: number };
const touching = (a: Box, b: Box) =>
	a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;

async function boxes(scope: Locator, graph: Graph) {
	const found = new Map<string, Box>();
	for (const node of graph.nodes) {
		const card = scope.getByTestId(`workflow-node-${node.id}`);
		await expect(card).toBeAttached();
		found.set(node.id, (await card.boundingBox())!);
	}
	return found;
}

// The chart as a person reads it: every transition but a loop back runs to the
// right, steps on the path share one line, and no card or label covers another.
async function expectLeftToRight(scope: Locator, graph: Graph, loops: string[][]) {
	await expect(async () => {
		const at = await boxes(scope, graph);
		const isLoop = (edge: Edge) => loops.some(([source, target]) => source === edge.source && target === edge.target);
		for (const edge of graph.edges) {
			if (edge.source === edge.target) continue;
			const from = at.get(edge.source)!,
				to = at.get(edge.target)!;
			if (isLoop(edge)) expect(to.x, `${edge.id} loops back`).toBeLessThan(from.x);
			else expect(to.x, `${edge.id} runs right`).toBeGreaterThan(from.x + from.width);
		}
		const path = graph.nodes.filter((n) => n.outcome !== 'failed').map((n) => at.get(n.id)!);
		for (const box of path) expect(Math.abs(box.y - path[0].y)).toBeLessThan(2);
		const cards = [...at.entries()];
		for (const [i, [a, boxA]] of cards.entries())
			for (const [b, boxB] of cards.slice(i + 1)) expect(touching(boxA, boxB), `${a} overlaps ${b}`).toBe(false);
		// No transition runs through a card other than the two it joins.
		const drawn = await scope.evaluate((root, list) => {
			const cards = [...root.querySelectorAll<HTMLElement>('[data-testid^="workflow-node-"]')].map((el) => ({
				id: el.dataset.testid!.slice('workflow-node-'.length),
				box: el.getBoundingClientRect()
			}));
			const through = new Set<string>();
			let checked = 0;
			for (const group of root.querySelectorAll<SVGGElement>('.svelte-flow__edge')) {
				const edge = list.find((e) => e.id === group.dataset.id);
				const path = group.querySelector<SVGPathElement>('.svelte-flow__edge-path');
				const matrix = path?.getScreenCTM();
				if (!edge || !path || !matrix) continue;
				checked++;
				for (let along = 0; along <= path.getTotalLength(); along += 3) {
					const point = path.getPointAtLength(along).matrixTransform(matrix);
					for (const card of cards)
						if (
							card.id !== edge.source &&
							card.id !== edge.target &&
							point.x > card.box.left &&
							point.x < card.box.right &&
							point.y > card.box.top &&
							point.y < card.box.bottom
						)
							through.add(`${edge.id} through ${card.id}`);
				}
			}
			return { checked, through: [...through] };
		}, graph.edges);
		expect(drawn.checked).toBe(graph.edges.filter((e) => e.source !== e.target).length);
		expect(drawn.through).toEqual([]);
		const labels = await scope.locator('.svelte-flow__edge-label').all();
		expect(labels.length).toBeGreaterThan(0);
		const labelBoxes: Array<{ text: string; box: Box }> = [];
		for (const label of labels) labelBoxes.push({ text: await label.innerText(), box: (await label.boundingBox())! });
		for (const [i, a] of labelBoxes.entries()) {
			for (const [id, card] of cards) expect(touching(a.box, card), `"${a.text}" covers ${id}`).toBe(false);
			for (const b of labelBoxes.slice(i + 1))
				expect(touching(a.box, b.box), `"${a.text}" covers "${b.text}"`).toBe(false);
		}
	}).toPass({ timeout: 10_000 });
}

// Transitions leave a step on its right and enter the next on its left.
async function expectSideHandles(scope: Locator, from: string, to: string) {
	const handle = (id: string, kind: string) =>
		scope.locator(`.svelte-flow__node[data-id="${id}"] .svelte-flow__handle.${kind}:not(.workflow-loop-handle)`);
	const source = (await handle(from, 'source').boundingBox())!;
	const target = (await handle(to, 'target').boundingBox())!;
	const fromCard = (await scope.getByTestId(`workflow-node-${from}`).boundingBox())!;
	const toCard = (await scope.getByTestId(`workflow-node-${to}`).boundingBox())!;
	expect(Math.abs(source.x + source.width / 2 - (fromCard.x + fromCard.width))).toBeLessThan(6);
	expect(Math.abs(target.x + target.width / 2 - toCard.x)).toBeLessThan(6);
	expect(Math.abs(source.y + source.height / 2 - (fromCard.y + fromCard.height / 2))).toBeLessThan(6);
}

const loopsOf: Record<string, string[][]> = {
	standard: [['review', 'implement']],
	review_loop: [
		['review', 'implement'],
		['ci', 'implement']
	],
	full_cycle: [
		['tests', 'implement'],
		['publish', 'implement'],
		['review', 'implement'],
		['ci', 'implement'],
		['ship', 'implement']
	]
};
const options: Record<string, string> = {
	standard: 'Copy of Standard',
	review_loop: 'Review loop',
	full_cycle: 'Full cycle',
	blank: 'Blank'
};

for (const size of sizes) {
	test.describe(`${size.name} width`, () => {
		test.use({ viewport: { width: size.width, height: size.height } });

		for (const template of ['standard', 'review_loop', 'full_cycle', 'blank']) {
			test(`a new workflow from the ${template} template opens left to right`, async ({ page }) => {
				await mockSettings(page);
				await page.goto('/test/settings/workflows');
				await page.getByTestId('workflow-new').click();
				const form = page.getByTestId('new-workflow-form');
				await form.getByLabel('Name').fill('Shipping');
				await form.locator('#new-workflow-template').click();
				await page.getByRole('option', { name: options[template], exact: true }).click();
				await form.getByRole('button', { name: 'Create', exact: true }).click();

				const editor = page.getByTestId('workflow-editor');
				await expect(editor).toBeVisible();
				const graph: Graph =
					template === 'blank'
						? {
								nodes: [
									{ id: 'start', type: 'start', position: at(0) },
									{ id: 'agent-1', type: 'agent', position: at(1) },
									{ id: 'end-1', type: 'end', position: at(2) }
								],
								// The IDs the page gives a blank workflow's transitions.
								edges: [
									{ id: 'start-agent-1', source: 'start', target: 'agent-1', condition: 'else' },
									{ id: 'agent-1-end-1', source: 'agent-1', target: 'end-1', condition: 'agent_finished' }
								],
								budget: {}
							}
						: templates[template];
				await expectLeftToRight(editor, graph, loopsOf[template] ?? []);
				await expectSideHandles(editor, graph.nodes[0].id, graph.nodes[1].id);
				// It opens at a readable size with the start in view, whatever its length.
				const canvas = (await editor.getByRole('region', { name: 'Workflow canvas' }).boundingBox())!;
				const start = (await editor.getByTestId('workflow-node-start').boundingBox())!;
				expect(start.x).toBeGreaterThanOrEqual(canvas.x);
				expect(start.x + start.width).toBeLessThanOrEqual(canvas.x + canvas.width);
				expect(start.width).toBeGreaterThanOrEqual(176 * 0.7 - 1);
				await expect(editor.getByTestId('workflow-save')).toBeDisabled();
				// It is already arranged: tidying up changes nothing.
				await editor.getByRole('toolbar').getByRole('button', { name: 'Tidy up', exact: true }).click();
				await expectLeftToRight(editor, graph, loopsOf[template] ?? []);
				await expect(editor.getByTestId('workflow-save')).toBeDisabled();
				await expect(editor.getByText('Unsaved changes')).toHaveCount(0);
			});
		}

		test('a workflow saved top to bottom opens arranged, and is saved that way with the next edit', async ({
			page
		}) => {
			const stored = vertical(withSkip());
			const { saves } = await mockSettings(page, { legacy: stored });
			await page.goto('/test/settings/workflows/legacy');
			const editor = page.getByTestId('workflow-editor');
			await expect(editor).toBeVisible();
			// Loops go over the top and the skipping transition under the steps it passes.
			await expectLeftToRight(editor, stored, loopsOf.full_cycle);
			await expectSideHandles(editor, 'start', 'implement');
			// Arranging it for display is not a change.
			await expect(editor.getByTestId('workflow-save')).toBeDisabled();
			await expect(editor.getByText('Unsaved changes')).toHaveCount(0);
			expect(saves).toEqual([]);

			await editor.getByLabel('Name').fill('Legacy, renamed');
			await editor.getByTestId('workflow-save').click();
			await expect.poll(() => saves.length).toBe(1);
			const sent = saves[0].graph;
			expect(sent.edges).toEqual(stored.edges);
			expect(sent.nodes.map(({ position: _, ...rest }) => rest)).toEqual(
				stored.nodes.map(({ position: _, ...rest }) => rest)
			);
			expect(sent.nodes.map((n) => n.position)).toEqual(templates.full_cycle.nodes.map((n) => n.position));
		});

		test('tidying up puts moved steps back in line, and the arrangement is saved', async ({ page }) => {
			const moved: Record<string, { x: number; y: number }> = {
				publish: { x: 700, y: 260 },
				ci: { x: 1500, y: -220 }
			};
			const stored: Graph = {
				...templates.review_loop,
				nodes: templates.review_loop.nodes.map((n) => ({ ...n, position: moved[n.id] ?? n.position }))
			};
			const { saves } = await mockSettings(page, { moved: stored });
			await page.goto('/test/settings/workflows/moved');
			const editor = page.getByTestId('workflow-editor');
			// Steps already running left to right stay where the person put them.
			const before = await boxes(editor, stored);
			expect(before.get('publish')!.y).toBeGreaterThan(before.get('implement')!.y + before.get('implement')!.height);
			expect(before.get('ci')!.y).toBeLessThan(before.get('implement')!.y);

			const arrange = editor.getByRole('toolbar').getByRole('button', { name: 'Tidy up', exact: true });
			await arrange.focus();
			await page.keyboard.press('Enter');
			await expectLeftToRight(editor, stored, loopsOf.review_loop);
			// The whole chart is brought into view.
			const canvas = (await editor.getByRole('region', { name: 'Workflow canvas' }).boundingBox())!;
			await expect(async () => {
				for (const id of ['start', 'done', 'failed']) {
					const box = (await editor.getByTestId(`workflow-node-${id}`).boundingBox())!;
					expect(box.x).toBeGreaterThanOrEqual(canvas.x);
					expect(box.x + box.width).toBeLessThanOrEqual(canvas.x + canvas.width + 1);
				}
			}).toPass();

			await editor.getByTestId('workflow-save').click();
			await expect.poll(() => saves.length).toBe(1);
			const sent = saves[0].graph;
			// Only positions changed.
			expect(sent.edges).toEqual(stored.edges);
			expect(sent.nodes.map(({ position: _, ...rest }) => rest)).toEqual(
				stored.nodes.map(({ position: _, ...rest }) => rest)
			);
			expect(sent.nodes.map((n) => n.position)).toEqual(templates.review_loop.nodes.map((n) => n.position));

			// Undo brings the earlier arrangement back.
			await editor.getByRole('toolbar').getByRole('button', { name: 'Undo', exact: true }).click();
			await expect(async () => {
				const undone = await boxes(editor, stored);
				expect(undone.get('publish')!.y).toBeGreaterThan(undone.get('implement')!.y + undone.get('implement')!.height);
			}).toPass();
		});

		test("a task's workflow runs left to right and scrolls sideways when it does not fit", async ({ page }) => {
			await mockTask(page);
			await page.goto('/test/assistant?conversation=chat-1');
			const agents = page.getByRole('button', { name: 'Agents', exact: true }).first();
			await expect(agents).toBeVisible();
			const view = page.getByTestId('task-workflow');
			if (!(await view.isVisible())) await agents.click();
			await expect(view).toBeVisible();

			// The task's version was saved top to bottom; the view arranges it.
			const chart = view.getByTestId('task-workflow-chart');
			await expectLeftToRight(chart, vertical(templates.review_loop), loopsOf.review_loop);
			const widths = await chart.evaluate((el) => ({ scroll: el.scrollWidth, client: el.clientWidth }));
			expect(widths.scroll).toBeGreaterThan(widths.client);

			// It opens on the step the task is at, and the page itself never scrolls sideways.
			const frame = (await chart.boundingBox())!;
			const inView = async (id: string) => {
				const box = (await chart.getByTestId(`workflow-node-${id}`).boundingBox())!;
				return box.x >= frame.x && box.x + box.width <= frame.x + frame.width;
			};
			await expect(chart.getByTestId('workflow-node-ci')).toHaveAttribute('data-current', 'true');
			await expect.poll(() => inView('ci')).toBe(true);
			expect(await inView('start')).toBe(false);
			expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

			// The scroll area is reached and scrolled with the keyboard.
			await expect(chart).toHaveRole('group');
			await expect(chart).toHaveAccessibleName('Workflow');
			await chart.focus();
			await expect(chart).toBeFocused();
			const from = await chart.evaluate((el) => el.scrollLeft);
			await page.keyboard.press('ArrowRight');
			await expect.poll(() => chart.evaluate((el) => el.scrollLeft)).toBeGreaterThan(from);

			// Scrolling sideways reaches both ends.
			await chart.evaluate((el) => el.scrollTo({ left: el.scrollWidth }));
			await expect.poll(() => inView('done')).toBe(true);
			await chart.evaluate((el) => el.scrollTo({ left: 0 }));
			await expect.poll(() => inView('start')).toBe(true);
		});
	});
}

// An issue conversation whose coding task follows a workflow and waits on CI.
async function mockTask(page: Page) {
	const conversation = {
		id: 'chat-1',
		workspace_id: 'workspace-1',
		creator_id: 'user-1',
		issue_id: 'issue-1',
		team_id: 'team-1',
		title: 'Implement search',
		created_at: time,
		updated_at: time
	};
	const task = {
		id: 'task-1',
		workspace_id: 'workspace-1',
		conversation_id: 'chat-1',
		creator_id: 'user-1',
		issue_id: 'issue-1',
		team_id: 'team-1',
		request_id: 'request-1',
		provider: 'codex',
		model: '',
		thinking: '',
		prompt: 'Implement and test issue search',
		state: 'awaiting_review',
		reason: 'Waiting for CI',
		generation: 1,
		failure_count: 0,
		checkout_id: 'checkout-1',
		run_id: 'run-1',
		workflow_version_id: 'legacy-v1',
		workflow_node: 'ci',
		workflow_status: 'waiting',
		created_at: time,
		updated_at: time
	};
	const step = (sequence: number, node: string, from?: string, condition?: string) => ({
		task_id: 'task-1',
		sequence,
		node_id: node,
		node_type: 'agent',
		visit: 1,
		from_node: from ?? null,
		edge_id: from ? `${from}-${node}-${condition}` : null,
		condition: condition ?? null,
		reason: '',
		detail: '',
		entered_at: time
	});
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.routeWebSocket('**/conversations/chat-1/stream*', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user-1', name: 'Owner', email: 'owner@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path.endsWith('/assistant/status')) return route.fulfill({ json: { configured: true, model: 'assistant-model' } });
		if (path.endsWith('/conversations')) return route.fulfill({ json: [conversation] });
		if (path.endsWith('/task-options'))
			return route.fulfill({
				json: {
					providers: [{ id: 'codex', display_name: 'Codex', supported_modes: ['autonomous'], custom: false }],
					model_servers: []
				}
			});
		if (path.endsWith('/tasks')) return route.fulfill({ json: { tasks: [task] } });
		if (path.endsWith('/conversations/chat-1'))
			return route.fulfill({
				json: {
					conversation,
					issue: { id: 'issue-1', identifier: 'ENG-1', title: 'Implement search', status: 'todo', description: '' },
					messages: [],
					agent_messages: [],
					cursor: 0,
					has_more: false
				}
			});
		if (path.endsWith('/agent-tasks/task-1/workflow'))
			return route.fulfill({
				json: {
					workflow_id: 'legacy',
					name: 'Review loop',
					version: 1,
					builtin: false,
					graph: vertical(templates.review_loop),
					node: 'ci',
					status: 'waiting',
					visits: { start: 1, implement: 1, publish: 1, review: 1, ci: 1 },
					iterations: 1,
					sequence: 5,
					steps: [
						step(1, 'start'),
						step(2, 'implement', 'start', 'else'),
						step(3, 'publish', 'implement', 'agent_finished'),
						step(4, 'review', 'publish', 'published'),
						step(5, 'ci', 'review', 'review_approved')
					],
					profiles,
					can_decide: false,
					task_state: 'awaiting_review',
					task_reason: 'Waiting for CI',
					task_generation: 1
				}
			});
		if (path.includes('/agent-tasks/task-1')) {
			if (path.endsWith('/history')) return route.fulfill({ json: { transitions: [] } });
			if (path.endsWith('/runs')) return route.fulfill({ json: { runs: [] } });
			if (path.endsWith('/live-messages')) return route.fulfill({ json: { messages: [] } });
			if (path.endsWith('/sessions')) return route.fulfill({ json: { sessions: [] } });
			if (path.endsWith('/logs')) return route.fulfill({ json: { logs: [] } });
			if (path.endsWith('/publication')) return route.fulfill({ json: { step: null } });
			if (path.endsWith('/validation')) return route.fulfill({ json: null });
			if (path.endsWith('/signals')) return route.fulfill({ json: [] });
			return route.fulfill({ json: task });
		}
		if (path === '/api/workspaces/test/agent-messages') return route.fulfill({ json: { messages: [] } });
		return route.fulfill({ json: [] });
	});
}
