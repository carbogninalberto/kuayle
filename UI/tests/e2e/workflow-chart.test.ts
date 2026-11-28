import { expect, test, type Locator, type Page, type WebSocketRoute } from '@playwright/test';
import { openPanel } from './side-panel';

// A task's run chart runs left to right, pans, zooms, fits, centres the step
// the run is at and expands full-screen, with a minimap where there is room
// for one. Graph workflows were removed, and the graph editor with them; the
// chart is the one pipelines and the built-in Standard flow are drawn on.
// These runs are drawn from a graph of steps (as the Standard flow is), which
// exercises the routed transitions a pipeline's chart never has.

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
			{ id: 'publish', type: 'publish', label: 'Open pull request', position: at(2) },
			{ id: 'review', type: 'agent', label: 'Review', profile_id: 'p-rev', max_visits: 6, position: at(3) },
			{ id: 'ci', type: 'check', label: 'CI', check: 'ci', timeout_minutes: 120, position: at(4) },
			{ id: 'ship', type: 'approval', label: 'Ready to ship', prompt: 'Ready to merge?', position: at(5) },
			{ id: 'merge', type: 'merge', label: 'Merge', position: at(6) },
			{ id: 'done', type: 'end', label: 'Done', outcome: 'done', position: at(7) },
			{ id: 'failed', type: 'end', label: 'Failed', outcome: 'failed', position: at(5, 1, true) }
		],
		edges: edges([
			['start', 'implement', 'else'],
			['implement', 'publish', 'agent_finished'],
			['implement', 'implement', 'agent_failed'],
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

const loopsOf: Record<string, string[][]> = {
	review_loop: [
		['review', 'implement'],
		['ci', 'implement']
	],
	full_cycle: [
		['publish', 'implement'],
		['review', 'implement'],
		['ci', 'implement'],
		['ship', 'implement']
	]
};
for (const size of sizes) {
	test.describe(`${size.name} width`, () => {
		test.use({ viewport: { width: size.width, height: size.height } });
		test("a task's workflow runs left to right and pans when it does not fit", async ({ page }) => {
			const { chart } = await openTaskChart(page);

			// The task's version was saved top to bottom; the view arranges it.
			await expectLeftToRight(chart, vertical(templates.review_loop), loopsOf.review_loop);

			// It opens on the step the task is at, and the page itself never scrolls sideways.
			await expect(chart.getByTestId('workflow-node-ci')).toHaveAttribute('data-current', 'true');
			await expect.poll(() => inView(chart, 'ci')).toBe(true);
			expect(await inView(chart, 'start')).toBe(false);
			expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
			// The side panel's short chart draws no minimap: it would cover it.
			await expect(chart.locator('.svelte-flow__minimap')).toHaveCount(0);

			// It is reached with the keyboard, and its arrow keys pan it.
			await expect(chart).toHaveRole('group');
			await expect(chart).toHaveAccessibleName('Workflow');
			await chart.focus();
			await expect(chart).toBeFocused();
			const from = await settled(chart, 'ci');
			await page.keyboard.press('ArrowLeft');
			await expect.poll(() => left(chart, 'ci')).toBeGreaterThan(from + 40);

			// Dragging pans it too, as far as the first step.
			await expect(async () => {
				await drag(page, chart, 1);
				expect(await inView(chart, 'start')).toBe(true);
			}).toPass();

			// F fits the whole graph back in.
			await chart.focus();
			await page.keyboard.press('f');
			await expect.poll(async () => (await inView(chart, 'start')) && (await inView(chart, 'done'))).toBe(true);
			expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
		});

		test("a task's chart centres the step the run is at, on request and never against the viewer", async ({ page }) => {
			const { chart, advance } = await openTaskChart(page);
			const tools = chart.getByRole('toolbar', { name: 'Chart tools' });
			await expect(tools).toBeVisible();
			for (const name of ['Zoom out', 'Zoom in', 'Fit to view', 'Centre on current step', 'Expand chart'])
				await expect(tools.getByRole('button', { name, exact: true })).toBeVisible();
			const centre = tools.getByRole('button', { name: 'Centre on current step', exact: true });

			// It opens centred on the current step, so there is nothing to centre.
			await expect.poll(() => offCentre(chart, 'ci')).toBeLessThanOrEqual(8);
			await expect(centre).toBeDisabled();

			// Zooming out and back in from the toolbar keeps the step centred.
			const wide = (await chart.getByTestId('workflow-node-ci').boundingBox())!.width;
			await tools.getByRole('button', { name: 'Zoom out', exact: true }).click();
			// Zoom in scales from wherever the view is, so it waits for zoom out to
			// finish. The animation ends exactly on the new zoom; a pixel short of it
			// is still moving (and leaves zoom in more than a pixel off).
			await expect
				.poll(async () => Math.abs((await chart.getByTestId('workflow-node-ci').boundingBox())!.width - wide / 1.2))
				.toBeLessThanOrEqual(0.1);
			await tools.getByRole('button', { name: 'Zoom in', exact: true }).click();
			await expect
				.poll(async () => Math.abs((await chart.getByTestId('workflow-node-ci').boundingBox())!.width - wide))
				.toBeLessThanOrEqual(1);
			await expect.poll(() => offCentre(chart, 'ci')).toBeLessThanOrEqual(8);

			// Fit shows the whole graph; the step is then off-centre and Centre brings it back.
			await tools.getByRole('button', { name: 'Fit to view', exact: true }).click();
			await expect(centre).toBeEnabled();
			await expect.poll(async () => (await inView(chart, 'start')) && (await inView(chart, 'done'))).toBe(true);
			await centre.click();
			await expect.poll(() => offCentre(chart, 'ci')).toBeLessThanOrEqual(8);
			await expect(centre).toBeDisabled();

			// Its keyboard shortcut: C centres again after the viewer panned away.
			await expect(chart).toHaveAttribute('aria-keyshortcuts', /\bC\b/);
			await drag(page, chart, 1);
			await expect(centre).toBeEnabled();
			await chart.focus();
			await page.keyboard.press('c');
			await expect.poll(() => offCentre(chart, 'ci')).toBeLessThanOrEqual(8);
			await expect(centre).toBeDisabled();

			// Once the viewer has moved it, a resize does not snap it back.
			await drag(page, chart, -1);
			await expect(centre).toBeEnabled();
			const moved = await settled(chart, 'ci');
			await page.setViewportSize({ width: size.width - 20, height: size.height });
			await page.setViewportSize({ width: size.width, height: size.height });
			// Nothing should move: give a re-centre time to happen before checking it did not.
			await page.waitForTimeout(500);
			expect(Math.abs((await left(chart, 'ci')) - moved)).toBeLessThanOrEqual(2);
			await expect(centre).toBeEnabled();

			// The next step does take the view there.
			await advance('implement');
			await expect(chart.getByTestId('workflow-node-implement')).toHaveAttribute('data-current', 'true');
			await expect.poll(() => offCentre(chart, 'implement')).toBeLessThanOrEqual(8);
			await expect(centre).toBeDisabled();
		});

		test("a task's chart expands full-screen with its tools and closes back to Expand", async ({ page }) => {
			const { chart } = await openTaskChart(page);
			const expand = chart
				.getByRole('toolbar', { name: 'Chart tools' })
				.getByRole('button', { name: 'Expand chart', exact: true });
			await expand.click();

			const dialog = page.getByRole('dialog', { name: 'Review loop workflow' });
			await expect(dialog).toBeVisible();
			const big = dialog.getByTestId('task-workflow-chart-expanded');
			await expect(big).toHaveRole('group');
			await expect(big).toHaveAccessibleName('Workflow');
			// It zooms in as it opens: measure it once it has.
			await animationsEnd(dialog);
			const dialogBox = (await dialog.boundingBox())!;
			expect(dialogBox.width).toBeGreaterThanOrEqual(size.width - 1);
			expect(dialogBox.height).toBeGreaterThanOrEqual(size.height - 1);

			// The same chart and tools, opened on the current step.
			const tools = big.getByRole('toolbar', { name: 'Chart tools' });
			const centre = tools.getByRole('button', { name: 'Centre on current step', exact: true });
			await expect(big.getByTestId('workflow-node-ci')).toHaveAttribute('data-current', 'true');
			await expect.poll(() => offCentre(big, 'ci')).toBeLessThanOrEqual(8);
			await expect(centre).toBeDisabled();
			await expect(tools.getByRole('button', { name: 'Expand chart' })).toHaveCount(0);
			// A minimap where there is room for one.
			const minimap = big.getByRole('img', { name: 'Workflow minimap' });
			if (size.name === 'phone') await expect(minimap).toBeHidden();
			else await expect(minimap).toBeVisible();

			await tools.getByRole('button', { name: 'Fit to view', exact: true }).click();
			await expect.poll(async () => (await inView(big, 'start')) && (await inView(big, 'done'))).toBe(true);
			await expect(centre).toBeEnabled();
			await centre.click();
			await expect.poll(() => offCentre(big, 'ci')).toBeLessThanOrEqual(8);

			// Esc closes it and focus goes back to Expand.
			await page.keyboard.press('Escape');
			await expect(dialog).toBeHidden();
			await expect(expand).toBeFocused();
		});
	});
}

// Opens the issue conversation's agent panel on the task's running chart.
async function openTaskChart(page: Page, options?: Parameters<typeof mockTask>[1]) {
	const { advance } = await mockTask(page, options);
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByTestId('panel-toggle')).toBeVisible();
	const view = page.getByTestId('task-workflow');
	if (!(await view.isVisible())) await openPanel(page, 'Agents');
	await expect(view).toBeVisible();
	const chart = view.getByTestId('task-workflow-chart');
	await expect(chart.getByTestId(`workflow-node-${options?.at ?? 'ci'}`)).toBeVisible();
	return { view, chart, advance };
}

const nodeBox = async (chart: Locator, id: string) => (await chart.getByTestId(`workflow-node-${id}`).boundingBox())!;
const left = async (chart: Locator, id: string) => (await nodeBox(chart, id)).x - (await chart.boundingBox())!.x;

// Where the step is once the view has stopped moving: two reads 100 ms apart agree.
async function settled(chart: Locator, id: string) {
	let last = Number.NaN;
	await expect
		.poll(
			async () => {
				const now = await left(chart, id);
				const still = Math.abs(now - last) < 0.5;
				last = now;
				return still;
			},
			{ intervals: [100] }
		)
		.toBe(true);
	return last;
}

// Waits for the element's own CSS animations (a dialog zooming in as it
// opens) to end, so that it is measured where it comes to rest.
async function animationsEnd(element: Locator) {
	await element.evaluate((el) => Promise.allSettled(el.getAnimations().map((animation) => animation.finished)));
}

// Whether the step is wholly inside the chart.
async function inView(chart: Locator, id: string) {
	const frame = (await chart.getByRole('region').boundingBox())!;
	const box = await nodeBox(chart, id);
	return (
		box.x >= frame.x &&
		box.x + box.width <= frame.x + frame.width &&
		box.y >= frame.y &&
		box.y + box.height <= frame.y + frame.height
	);
}

// How far the step's centre is from the chart's.
async function offCentre(chart: Locator, id: string) {
	const frame = (await chart.getByRole('region').boundingBox())!;
	const box = await nodeBox(chart, id);
	return Math.hypot(
		box.x + box.width / 2 - (frame.x + frame.width / 2),
		box.y + box.height / 2 - (frame.y + frame.height / 2)
	);
}

// Drags half a viewport so repeated gestures cannot skip past a visible step (1: to the
// right, showing what is left of the view; -1: to the left).
async function drag(page: Page, chart: Locator, direction: 1 | -1) {
	const frame = (await chart.getByRole('region').boundingBox())!;
	const y = frame.y + frame.height - 12;
	const [from, to] = [frame.x + frame.width / 4, frame.x + frame.width * 3 / 4];
	await page.mouse.move(direction === 1 ? from : to, y);
	await page.mouse.down();
	await page.mouse.move(direction === 1 ? to : from, y, { steps: 8 });
	await page.mouse.up();
}

// An issue conversation whose coding task follows a workflow. advance()
// updates the run as the server would announce it.
async function mockTask(page: Page, { at = 'ci', fullCycle = false } = {}) {
	const graph = vertical(fullCycle ? templates.full_cycle : templates.review_loop);
	const sockets: WebSocketRoute[] = [];
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
		reason: fullCycle ? 'Waiting for merge' : 'Waiting for CI',
		generation: 1,
		failure_count: 0,
		checkout_id: 'checkout-1',
		run_id: 'run-1',
		workflow_version_id: 'legacy-v1',
		workflow_node: at,
		workflow_status: 'waiting',
		created_at: time,
		updated_at: time
	};
	const step = (sequence: number, node: string, from?: string, condition?: string) => ({
		task_id: 'task-1',
		sequence,
		node_id: node,
		node_type: graph.nodes.find((step) => step.id === node)?.type,
		visit: 1,
		from_node: from ?? null,
		edge_id: from ? `${from}-${node}-${condition}` : null,
		condition: condition ?? null,
		reason: '',
		detail: '',
		entered_at: time
	});
	const steps = fullCycle
		? [
				step(1, 'start'),
				step(2, 'implement', 'start', 'else'),
				step(3, 'publish', 'implement', 'agent_finished'),
				step(4, 'review', 'publish', 'published'),
				step(5, 'ci', 'review', 'review_approved'),
				step(6, 'ship', 'ci', 'checks_passed'),
				step(7, 'merge', 'ship', 'approval_approved')
			]
		: [
				step(1, 'start'),
				step(2, 'implement', 'start', 'else'),
				step(3, 'publish', 'implement', 'agent_finished'),
				step(4, 'review', 'publish', 'published'),
				step(5, 'ci', 'review', 'review_approved')
			];
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.routeWebSocket('**/conversations/chat-1/stream*', (socket) => void sockets.push(socket));
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user-1', name: 'Owner', email: 'owner@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path.endsWith('/assistant/status'))
			return route.fulfill({ json: { configured: true, model: 'assistant-model' } });
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
					name: fullCycle ? 'Full cycle' : 'Review loop',
					version: 1,
					builtin: false,
					graph,
					node: task.workflow_node,
					status: task.workflow_status,
					visits: Object.fromEntries(steps.map((step) => [step.node_id, 1])),
					iterations: 1,
					sequence: steps.length,
					steps: steps.map((step, i) =>
						i < steps.length - 1 || task.state === 'completed'
							? { ...step, finished_at: time, outcome: steps[i + 1]?.condition ?? 'task_completed' }
							: step
					),
					profiles,
					can_decide: false,
					task_state: task.state,
					task_reason: task.reason,
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
			if (path.endsWith('/signals')) return route.fulfill({ json: [] });
			return route.fulfill({ json: task });
		}
		if (path === '/api/workspaces/test/agent-messages') return route.fulfill({ json: { messages: [] } });
		return route.fulfill({ json: [] });
	});
	let cursor = 0;
	return {
		// Waits for the conversation's stream to have connected before announcing.
		async advance(to: string, status = 'running') {
			await expect.poll(() => sockets.length).toBeGreaterThan(0);
			task.workflow_node = to;
			task.workflow_status = status;
			task.updated_at = '2026-11-01T00:05:00Z';
			if (status === 'done') {
				task.state = 'completed';
				task.reason = 'Pull request #12 was merged on GitHub';
			}
			const event = { id: `event-${++cursor}`, conversation_id: 'chat-1', cursor, type: 'task.state_changed', payload: task };
			sockets.at(-1)!.send(JSON.stringify(event));
		}
	};
}

test('a task finished by merging shows its completed workflow path', async ({ page }, testInfo) => {
	await page.setViewportSize({ width: 1440, height: 900 });
	const { view, chart, advance } = await openTaskChart(page, { at: 'merge', fullCycle: true });
	const merge = chart.getByTestId('workflow-node-merge');
	const done = chart.getByTestId('workflow-node-done');
	const edge = (id: string) => chart.locator(`.svelte-flow__edge[data-id="${id}"]`);
	const mergeEdge = edge('merge-done-merged');
	const failureEdge = edge('ci-failed-timeout');
	await expect(merge).toHaveAttribute('data-current', 'true');
	await expect(mergeEdge).toHaveClass(/workflow-edge-idle/);
	await expect(failureEdge).toHaveClass(/workflow-edge-idle/);

	// GitHub finishes the task while its persisted cursor still names Merge.
	await advance('merge', 'done');
	await expect(view.getByTestId('task-workflow-status')).toHaveText('Done');
	await expect(view.getByText(/Current step/)).toHaveCount(0);
	await expect(chart.locator('[data-current="true"]')).toHaveCount(0);
	await expect(merge).toHaveClass(/border-primary\/50/);
	await expect(done).toHaveClass(/border-primary\/50/);
	await expect.poll(() => offCentre(chart, 'done')).toBeLessThanOrEqual(8);
	await expect(mergeEdge).toHaveClass(/workflow-edge-fired/);
	await expect(mergeEdge).not.toHaveClass(/\banimated\b/);
	await expect(failureEdge).toHaveCount(0);
	await expect(chart.getByTestId('workflow-node-failed')).toHaveCount(0);
	await page.screenshot({ path: testInfo.outputPath('completed-workflow-panel.png'), animations: 'disabled' });

	await chart.getByRole('button', { name: 'Expand chart' }).click();
	const expanded = page.getByTestId('task-workflow-chart-expanded');
	await expect.poll(() => offCentre(expanded, 'done')).toBeLessThanOrEqual(8);
	await page.screenshot({ path: testInfo.outputPath('completed-workflow.png'), animations: 'disabled' });
});
