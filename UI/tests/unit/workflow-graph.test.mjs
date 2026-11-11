import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	addNext,
	retries,
	reviewBlock,
	setRetry,
	duplicateNode,
	anchors,
	crosses,
	extent,
	layering,
	leftToRight,
	nodeWidth,
	opening,
	polyline,
	roundedPath,
	routes,
	samePositions,
	scrollTo,
	tidy,
	conditionsFor,
	connect,
	graphForSave,
	iterationStep,
	newNode,
	pathOf,
	pendingDecision,
	runOverlay,
	placeIssues,
	removeNode,
	starterGraph,
	starterProfile
} from '../../src/lib/features/workflows/workflow-graph.ts';

const profiles = [
	{ id: 'impl', role: 'Implementer', behaviour: 'changes_code' },
	{ id: 'rev', role: 'Reviewer', behaviour: 'reviews' },
	// The role is only a label: this one plans, whatever it is called.
	{ id: 'plan', role: 'reviewer', behaviour: 'plans' }
];

test('a planning step runs like an iteration: finished or failed, never a verdict or a timeout', () => {
	assert.deepEqual(conditionsFor({ id: 'p', type: 'agent', profile_id: 'plan', timeout_minutes: 5 }, profiles), [
		'agent_finished',
		'agent_failed',
		'else'
	]);
});

test('a reviewer step offers verdicts, an implementer step finished/failed', () => {
	assert.deepEqual(conditionsFor({ id: 'r', type: 'agent', profile_id: 'rev' }, profiles), [
		'review_approved',
		'review_changes_requested',
		'agent_failed',
		'else'
	]);
	assert.deepEqual(
		conditionsFor({ id: 'i', type: 'agent', profile_id: 'impl', timeout_minutes: 5 }, profiles),
		['agent_finished', 'agent_failed', 'else'],
		'a coding iteration cannot time out'
	);
	assert.deepEqual(conditionsFor({ id: 'c', type: 'check', check: 'ci', timeout_minutes: 5 }, profiles), [
		'checks_passed',
		'checks_failed',
		'timeout',
		'else'
	]);
	assert.deepEqual(conditionsFor({ id: 's', type: 'start' }, profiles), ['else']);
	assert.deepEqual(conditionsFor({ id: 'e', type: 'end' }, profiles), []);
});

test('connecting picks the first unused condition and refuses invalid links', () => {
	let graph = starterGraph({ start: 'Start', agent: 'Implementer', end: 'Done' }, 'impl');
	graph = connect(graph, 'agent-1', 'end-1', profiles);
	assert.equal(graph.edges.at(-1).condition, 'agent_failed', 'agent_finished is already used');
	const unchanged = connect(graph, 'agent-1', 'start', profiles);
	assert.equal(unchanged, graph, 'nothing leads back to the start');
	assert.equal(connect(graph, 'end-1', 'agent-1', profiles), graph, 'an end has no exits');
	assert.equal(connect(graph, 'agent-1', 'end-1', profiles, 'agent_finished'), graph, 'a condition is used once');
});

test('removing a node removes its transitions; new nodes get unique ids', () => {
	const graph = starterGraph({ start: 'Start', agent: 'Implementer', end: 'Done' });
	const node = newNode(graph, 'agent', 'Another');
	assert.equal(node.id, 'agent-2');
	const last = graph.nodes.at(-1);
	assert.equal(node.position.y, 0, 'on the main path');
	assert.ok(node.position.x > last.position.x + nodeWidth(last.type), 'right of the last step');
	const removed = removeNode(graph, 'agent-1');
	assert.deepEqual(
		removed.nodes.map((n) => n.id),
		['start', 'end-1']
	);
	assert.equal(removed.edges.length, 0);
});

test('saving drops empty optional fields and keeps limits', () => {
	const graph = starterGraph({ start: 'Start', agent: ' Implementer ', end: 'Done' }, 'impl');
	graph.nodes[1].max_visits = 3;
	graph.nodes[1].prompt = '  ';
	graph.nodes[0].profile_id = 'impl';
	graph.budget = { max_iterations: 0, max_minutes: 90, on_exhausted: 'ask' };
	const saved = graphForSave(graph);
	assert.deepEqual(saved.nodes[1], {
		id: 'agent-1',
		type: 'agent',
		label: 'Implementer',
		position: { x: 316, y: 0 },
		profile_id: 'impl',
		max_visits: 3
	});
	assert.equal(saved.nodes[0].profile_id, undefined, 'only agent steps keep a profile');
	assert.deepEqual(saved.budget, { max_minutes: 90, on_exhausted: 'ask' });
});

test('issues are placed on nodes, transitions or the workflow', () => {
	const placed = placeIssues([
		{ code: 'unreachable', message: 'x', node_id: 'a' },
		{ code: 'invalid_condition', message: 'y', node_id: 'a', edge_id: 'e1' },
		{ code: 'missing_end', message: 'z' }
	]);
	assert.equal(placed.nodes.get('a').length, 1);
	assert.equal(placed.edges.get('e1')[0].code, 'invalid_condition');
	assert.equal(placed.general[0].code, 'missing_end');
});

test('the path counts each transition and keeps why it last fired', () => {
	const steps = [
		{ sequence: 1, node_id: 'start', reason: 'Task delegated' },
		{
			sequence: 2,
			node_id: 'implement',
			edge_id: 'start-implement',
			reason: 'Start → Implementer: started',
			generation: 1
		},
		{ sequence: 3, node_id: 'review', edge_id: 'implement-review', reason: 'Implementer → Reviewer: agent finished' },
		{
			sequence: 4,
			node_id: 'implement',
			edge_id: 'review-changes',
			reason: 'Reviewer → Implementer: review requested changes',
			generation: 2
		},
		{
			sequence: 5,
			node_id: 'review',
			edge_id: 'implement-review',
			reason: 'Implementer → Reviewer: agent finished again'
		}
	];
	const { visited, fired } = pathOf(steps);
	assert.deepEqual([...visited], ['start', 'implement', 'review']);
	assert.deepEqual(fired.get('implement-review'), { count: 2, reason: 'Implementer → Reviewer: agent finished again' });
	assert.equal(iterationStep(steps, 2).reason, 'Reviewer → Implementer: review requested changes');
	assert.equal(iterationStep(steps, 3), undefined);
});

const reviewLoop = {
	nodes: [
		{ id: 'start', type: 'start', label: 'Start' },
		{ id: 'implement', type: 'agent', label: 'Implementer', profile_id: 'impl', max_visits: 3 },
		{ id: 'ship', type: 'approval', label: 'Ready to ship', prompt: 'Ship it?' }
	],
	edges: [],
	budget: {}
};
const run = (patch) => ({
	graph: reviewLoop,
	node: 'implement',
	status: 'running',
	can_decide: false,
	sequence: 4,
	task_state: 'running',
	visits: { implement: 2 },
	steps: [],
	...patch
});

test('the run overlay marks the current node and keeps it visited for a Standard task without steps', () => {
	const overlay = runOverlay(run({ node: 'implement', steps: [] }));
	assert.equal(overlay.current, 'implement');
	assert.deepEqual([...overlay.visited], ['implement']);
	assert.deepEqual(overlay.visits, { implement: 2 });
	const traced = runOverlay(
		run({
			steps: [
				{ sequence: 1, node_id: 'start', reason: 'started' },
				{ sequence: 2, node_id: 'implement', edge_id: 'e1', reason: 'r' }
			]
		})
	);
	assert.deepEqual([...traced.visited], ['start', 'implement']);
	assert.equal(traced.fired.get('e1').count, 1);
});

test('a person decides an approval only while the task awaits it, and a limit while paused', () => {
	assert.equal(pendingDecision(run({ can_decide: false, status: 'paused' })), null, 'the server must allow deciding');
	assert.deepEqual(pendingDecision(run({ can_decide: true, status: 'paused' })), { kind: 'limit', sequence: 4 });
	assert.deepEqual(
		pendingDecision(run({ can_decide: true, status: 'waiting', node: 'ship', task_state: 'awaiting_review' })),
		{ kind: 'approval', sequence: 4, question: 'Ship it?' }
	);
	assert.equal(
		pendingDecision(run({ can_decide: true, status: 'waiting', node: 'ship', task_state: 'running' })),
		null,
		'an approval is decided once the task reached it'
	);
	assert.equal(
		pendingDecision(run({ can_decide: true, status: 'waiting', node: 'implement' })),
		null,
		'waiting on CI or a review is not a decision'
	);
});

test('adding the next step places it to the right and connects it on a free condition', () => {
	const graph = {
		nodes: [
			{ id: 'start', type: 'start', label: 'Start', position: { x: 0, y: 0 } },
			{ id: 'implement', type: 'agent', label: 'Implement', position: { x: 316, y: 0 } }
		],
		edges: [{ id: 'e1', source: 'start', target: 'implement', condition: 'else' }],
		budget: {}
	};
	const { graph: next, id } = addNext(graph, 'implement', 'check', 'Check', []);
	const added = next.nodes.find((n) => n.id === id);
	assert.deepEqual(added.position, { x: 696, y: 0 }, 'right of the source, on its lane');
	assert.deepEqual(next.edges.map((e) => [e.source, e.target, e.condition]).at(-1), [
		'implement',
		id,
		'agent_finished'
	]);
	const second = addNext(next, 'implement', 'end', 'Failed', []);
	const failed = second.graph.nodes.find((n) => n.id === second.id);
	assert.equal(failed.position.x, 696);
	assert.ok(failed.position.y > added.position.y, 'placed in the lane below the step already there');
	assert.deepEqual(overlaps(second.graph), []);
	assert.equal(second.graph.edges.at(-1).condition, 'agent_failed');
	assert.equal(addNext(graph, 'missing', 'end', 'End', []).id, '');
});

test('duplicating copies a step without transitions and never the start', () => {
	const graph = {
		nodes: [
			{ id: 'start', type: 'start', label: 'Start', position: { x: 0, y: 0 } },
			{ id: 'check', type: 'check', label: 'CI', check: 'ci', position: { x: 0, y: 130 } }
		],
		edges: [{ id: 'e1', source: 'start', target: 'check', condition: 'else' }],
		budget: {}
	};
	const { graph: next, id } = duplicateNode(graph, 'check');
	const copy = next.nodes.find((n) => n.id === id);
	assert.notEqual(id, 'check');
	assert.equal(copy.check, 'ci');
	assert.deepEqual(copy.position, { x: 40, y: 170 });
	assert.equal(next.edges.length, 1);
	assert.equal(duplicateNode(graph, 'start').id, '');
});

test('retry on failure loops the failure back to the step and back to Failed when turned off', () => {
	const graph = {
		nodes: [
			{ id: 'start', type: 'start', label: 'Start', position: { x: 0, y: 0 } },
			{ id: 'impl', type: 'agent', label: 'Implement', profile_id: 'impl', position: { x: 0, y: 130 } },
			{ id: 'failed', type: 'end', label: 'Failed', outcome: 'failed', position: { x: 300, y: 130 } }
		],
		edges: [
			{ id: 'e1', source: 'start', target: 'impl', condition: 'else' },
			{ id: 'e2', source: 'impl', target: 'failed', condition: 'agent_failed' }
		],
		budget: {}
	};
	const on = setRetry(graph, 'impl', true, profiles);
	assert.ok(retries(on, on.nodes[1], profiles));
	assert.equal(on.nodes[1].max_visits, 3, 'a retry loop gets a visit limit');
	assert.deepEqual(
		on.edges.filter((e) => e.source === 'impl').map((e) => [e.target, e.condition]),
		[['impl', 'agent_failed']]
	);
	const off = setRetry(on, 'impl', false, profiles);
	assert.ok(!retries(off, off.nodes[1], profiles));
	assert.deepEqual(
		off.edges.filter((e) => e.source === 'impl').map((e) => [e.target, e.condition]),
		[['failed', 'agent_failed']]
	);
	assert.equal(setRetry(graph, 'start', true, profiles), graph, 'only agent and publish steps retry');
});

test('a blank workflow starts with a profile that changes code', () => {
	const builtin = { id: 'b', behaviour: 'changes_code', builtin_key: 'implementer' };
	const other = { id: 'o', behaviour: 'changes_code' };
	assert.equal(starterProfile([other, builtin])?.id, 'b', 'the built-in implementer first');
	// A built-in implementer that was turned into a planner or a reviewer is not one.
	assert.equal(starterProfile([{ ...builtin, behaviour: 'reviews' }, other])?.id, 'o');
	assert.equal(starterProfile([{ ...builtin, behaviour: 'plans' }, { ...other, archived_at: '2026-01-01' }]), undefined);
	assert.equal(starterProfile([{ ...builtin, archived_at: '2026-01-01' }, other])?.id, 'o');
});

test('an automated review block runs the reviewer and retries if it fails', () => {
	const graph = {
		nodes: [{ id: 'start', type: 'start', label: 'Start', position: { x: 0, y: 0 } }],
		edges: [],
		budget: {}
	};
	const { graph: next, id } = reviewBlock(graph, 'Automated review', [
		...profiles,
		{ id: 'rev2', role: 'Reviewer', behaviour: 'reviews', builtin_key: 'reviewer' }
	]);
	const node = next.nodes.find((n) => n.id === id);
	assert.equal(node.profile_id, 'rev2', 'the built-in reviewer first');
	assert.equal(node.max_visits, 3);
	assert.deepEqual(
		next.edges.map((e) => [e.source, e.target, e.condition]),
		[[id, id, 'agent_failed']]
	);
});

// The built-in templates' steps and transitions (BE/internal/domain/workflow.go),
// stored top to bottom as workflows saved before the chart ran left to right.
const step = (id, type, row, extra = {}) => ({ id, type, label: id, position: { x: 0, y: row * 130 }, ...extra });
const edges = (list) => list.map(([source, target, condition]) => ({ id: `${source}-${target}-${condition}`, source, target, condition }));
const templates = {
	standard: {
		nodes: [
			step('start', 'start', 0),
			step('implement', 'agent', 1),
			step('validate', 'check', 2),
			step('publish', 'publish', 3),
			step('review', 'approval', 4),
			step('done', 'end', 5, { outcome: 'done' }),
			{ ...step('failed', 'end', 2, { outcome: 'failed' }), position: { x: 320, y: 260 } }
		],
		edges: edges([
			['start', 'implement', 'else'],
			['implement', 'validate', 'agent_finished'],
			['implement', 'failed', 'agent_failed'],
			['validate', 'publish', 'checks_passed'],
			['validate', 'publish', 'else'],
			['validate', 'failed', 'checks_failed'],
			['publish', 'review', 'published'],
			['review', 'done', 'approval_approved'],
			['review', 'implement', 'approval_denied']
		]),
		budget: {}
	},
	review_loop: {
		nodes: [
			step('start', 'start', 0),
			step('implement', 'agent', 1),
			step('publish', 'publish', 2),
			step('review', 'agent', 3),
			step('ci', 'check', 4),
			step('ship', 'approval', 5),
			step('done', 'end', 6, { outcome: 'done' }),
			{ ...step('failed', 'end', 3, { outcome: 'failed' }), position: { x: 320, y: 420 } }
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
		budget: {}
	},
	full_cycle: {
		nodes: [
			step('start', 'start', 0),
			step('implement', 'agent', 1),
			step('tests', 'check', 2),
			step('publish', 'publish', 3),
			step('review', 'agent', 4),
			step('ci', 'check', 5),
			step('ship', 'approval', 6),
			step('merge', 'merge', 7),
			step('done', 'end', 8, { outcome: 'done' }),
			{ ...step('failed', 'end', 5, { outcome: 'failed' }), position: { x: 320, y: 650 } }
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
		budget: {}
	}
};

// Pairs of steps whose cards touch (a card is about 50 high), with room to spare.
function overlaps(graph) {
	const found = [];
	for (const [i, a] of graph.nodes.entries())
		for (const b of graph.nodes.slice(i + 1))
			if (
				a.position.x < b.position.x + nodeWidth(b.type) + 40 &&
				b.position.x < a.position.x + nodeWidth(a.type) + 40 &&
				Math.abs(a.position.y - b.position.y) < 70
			)
				found.push([a.id, b.id]);
	return found;
}

// Every transition as the canvas draws it: the routed ones through their
// corners, the others straight from a step's right to the next one's left.
function lines(graph) {
	const routed = routes(graph);
	const at = (id) => graph.nodes.find((n) => n.id === id);
	return graph.edges
		.filter((e) => e.source !== e.target)
		.map((edge) => {
			const [from, to] = [at(edge.source), at(edge.target)];
			const route = routed.get(edge.id);
			if (route) {
				const { start, end } = anchors(from, to, route);
				return { edge, route, points: polyline(start, end, route) };
			}
			const a = { x: from.position.x + nodeWidth(from.type), y: from.position.y + 25 };
			const b = { x: to.position.x, y: to.position.y + 25 };
			const mid = (a.x + b.x) / 2;
			return { edge, points: [a, { x: mid, y: a.y }, { x: mid, y: b.y }, b] };
		});
}

// The transitions that run through a card other than the two they join.
function throughCards(graph) {
	const found = [];
	for (const { edge, points } of lines(graph))
		for (const node of graph.nodes) {
			if (node.id === edge.source || node.id === edge.target) continue;
			const card = { x: node.position.x, y: node.position.y, width: nodeWidth(node.type), height: 50 };
			if (points.some((point, i) => i > 0 && crosses(points[i - 1], point, card))) found.push([edge.id, node.id]);
		}
	return found;
}

const shuffled = (graph, seed) => {
	const order = (list) => list.map((item, i) => [((i + 1) * seed) % (list.length + 1), item]).sort((a, b) => a[0] - b[0]);
	return { ...graph, nodes: order(graph.nodes).map(([, node]) => node) };
};
const placed = (graph) => Object.fromEntries(graph.nodes.map((n) => [n.id, n.position]));

for (const [name, template] of Object.entries(templates)) {
	test(`arranging the ${name} template lays it out left to right without overlaps`, () => {
		const arranged = tidy(template);
		const at = (id) => arranged.nodes.find((n) => n.id === id);
		const { back } = layering(arranged);
		const routed = routes(arranged);
		for (const edge of arranged.edges) {
			if (edge.source === edge.target) continue;
			const from = at(edge.source),
				to = at(edge.target);
			if (back.has(edge.id)) {
				// A loop returns to an earlier step, on its own arc above the path.
				assert.ok(to.position.x < from.position.x, `${edge.id} loops back to the left`);
				assert.equal(routed.get(edge.id).kind, 'loop');
			} else
				assert.ok(
					to.position.x >= from.position.x + nodeWidth(from.type) + 120,
					`${edge.id} runs to the right with room for its label`
				);
		}
		assert.deepEqual(overlaps(arranged), []);
		assert.deepEqual(throughCards(arranged), []);
		// The main path is one straight line; only failed endings leave it.
		for (const node of arranged.nodes)
			assert.equal(node.position.y, node.outcome === 'failed' ? 120 : 0, `${node.id} lane`);
		const heights = [...routed.values()].filter((r) => r.kind === 'loop').map((r) => r.turns[0]);
		assert.equal(new Set(heights).size, heights.length, 'each loop has its own height');
		assert.ok(heights.every((y) => y < 0), 'above the steps');
		// Only positions change.
		assert.deepEqual(
			arranged.nodes.map(({ position, ...rest }) => rest),
			template.nodes.map(({ position, ...rest }) => rest)
		);
		assert.deepEqual(arranged.edges, template.edges);
		assert.deepEqual(tidy(arranged), arranged, 'arranging again changes nothing');
		assert.ok(samePositions(tidy(arranged), arranged));
		assert.ok(!samePositions(arranged, template));
		// The order the steps are stored in does not matter.
		for (const seed of [2, 3, 5, 7])
			assert.deepEqual(placed(tidy(shuffled(template, seed))), placed(arranged), `stored in another order (${seed})`);
	});
}

test('loops are nested by length and retries on the step itself take no arc', () => {
	const arranged = tidy(templates.full_cycle);
	const routed = routes(arranged);
	const height = (source) =>
		routed.get(arranged.edges.find((e) => e.source === source && e.target === 'implement').id).turns[0];
	assert.ok(height('tests') > height('review'), 'a longer loop runs higher');
	assert.ok(height('review') > height('ci'));
	assert.equal([...routed.values()].filter((r) => r.kind === 'loop').length, 5);
	const box = extent(arranged);
	assert.ok(box.y < height('ship'), 'the box leaves room for the loops above the steps');
	assert.equal(box.width, 176 + 7 * 240 + 176 + 8 * 140);
});

test('a failure drops under its step and runs between the lanes to the failed ending', () => {
	const arranged = tidy(templates.review_loop);
	const routed = routes(arranged);
	const of = (source, target) => routed.get(arranged.edges.find((e) => e.source === source && e.target === target).id);
	for (const source of ['implement', 'publish', 'review', 'ci', 'ship']) {
		const route = of(source, 'failed');
		assert.equal(route.kind, 'drop', source);
		assert.ok(route.turns[0] > 50 && route.turns[0] < 120, 'between the main path and the lane below');
	}
	assert.equal(of('implement', 'failed').turns[0], of('ship', 'failed').turns[0], 'they join on one line');
	assert.equal(of('ship', 'done'), undefined, 'the main path runs straight');
	assert.equal(of('implement', 'publish'), undefined);
});

test('a transition that skips steps runs under them, on a line of its own', () => {
	const graph = tidy({
		nodes: [
			step('start', 'start', 0),
			step('a', 'agent', 1),
			step('b', 'check', 2),
			step('c', 'approval', 3),
			step('done', 'end', 4, { outcome: 'done' }),
			step('failed', 'end', 4, { outcome: 'failed' })
		],
		edges: edges([
			['start', 'a', 'else'],
			['a', 'b', 'agent_finished'],
			['a', 'done', 'else'],
			['a', 'failed', 'agent_failed'],
			['b', 'c', 'checks_passed'],
			['b', 'failed', 'checks_failed'],
			['c', 'done', 'approval_approved']
		]),
		budget: {}
	});
	assert.deepEqual(throughCards(graph), []);
	const routed = routes(graph);
	const skip = routed.get('a-done-else');
	assert.equal(skip.kind, 'drop');
	assert.notEqual(skip.turns[0], routed.get('a-failed-agent_failed').turns[0], 'not on the line of the failures');
	assert.notEqual(skip.from, routed.get('a-failed-agent_failed').from, 'and it leaves the step beside them');
	assert.equal(routed.get('b-failed-checks_failed').turns[0], routed.get('a-failed-agent_failed').turns[0]);
});

test('branches of a step share its next column, one lane each', () => {
	const graph = tidy({
		nodes: [
			step('start', 'start', 0),
			step('check', 'check', 1),
			step('a', 'agent', 2),
			step('b', 'agent', 2),
			step('done', 'end', 3, { outcome: 'done' })
		],
		edges: edges([
			['start', 'check', 'else'],
			['check', 'a', 'checks_passed'],
			['check', 'b', 'checks_failed'],
			['a', 'done', 'agent_finished'],
			['b', 'done', 'agent_finished']
		]),
		budget: {}
	});
	const at = (id) => graph.nodes.find((n) => n.id === id).position;
	assert.equal(at('a').x, at('b').x);
	assert.deepEqual([at('a').y, at('b').y], [0, 120]);
	assert.deepEqual(overlaps(graph), []);
});

test('lanes follow the transitions, not the order steps are stored in', () => {
	// The failing branch is stored before the step the path goes on with.
	const graph = {
		nodes: [
			step('fix', 'agent', 2),
			step('start', 'start', 0),
			step('check', 'check', 1),
			step('ship', 'approval', 2),
			step('failed', 'end', 3, { outcome: 'failed' }),
			step('done', 'end', 3, { outcome: 'done' })
		],
		edges: edges([
			['start', 'check', 'else'],
			['check', 'fix', 'checks_failed'],
			['check', 'ship', 'checks_passed'],
			['fix', 'check', 'agent_finished'],
			['fix', 'failed', 'agent_failed'],
			['ship', 'done', 'approval_approved'],
			['ship', 'failed', 'approval_denied']
		]),
		budget: {}
	};
	const arranged = tidy(graph);
	const at = (id) => arranged.nodes.find((n) => n.id === id).position;
	assert.deepEqual([at('ship').y, at('fix').y], [0, 120], 'the hoped-for outcome stays on the main path');
	assert.deepEqual([at('done').y, at('failed').y], [0, 120]);
	assert.deepEqual(overlaps(arranged), []);
	assert.deepEqual(throughCards(arranged), []);
	for (const seed of [2, 3, 5]) assert.deepEqual(placed(tidy(shuffled(graph, seed))), placed(arranged));
	// The loop from the lane below goes round the step above it, not through it.
	const loop = routes(arranged).get('fix-check-agent_finished');
	assert.equal(loop.kind, 'loop');
	assert.equal(loop.exit, 'right');
	assert.equal(loop.enter, 'top');
});

test('a routed transition turns at right angles with rounded corners', () => {
	const route = { kind: 'drop', exit: 'bottom', enter: 'left', from: 0.5, to: 0.5, turns: [84, 500] };
	const points = polyline({ x: 100, y: 50 }, { x: 520, y: 145 }, route);
	assert.deepEqual(points, [
		{ x: 100, y: 50 },
		{ x: 100, y: 84 },
		{ x: 500, y: 84 },
		{ x: 500, y: 145 },
		{ x: 520, y: 145 }
	]);
	assert.equal(
		roundedPath(points),
		'M 100 50 L 100 76 Q 100 84 108 84 L 492 84 Q 500 84 500 92 L 500 137 Q 500 145 508 145 L 520 145'
	);
	const over = { kind: 'loop', exit: 'top', enter: 'top', from: 0.65, to: 0.35, turns: [-28] };
	assert.deepEqual(polyline({ x: 900, y: 0 }, { x: 400, y: 0 }, over), [
		{ x: 900, y: 0 },
		{ x: 900, y: -28 },
		{ x: 400, y: -28 },
		{ x: 400, y: 0 }
	]);
});

test('the task chart scrolls to its current step only once it has a width', () => {
	const chart = tidy(templates.review_loop);
	const view = { width: 260, zoom: 0.8, padding: 24 };
	assert.equal(scrollTo(chart, 'ci', { ...view, width: 0 }), undefined, 'hidden: nothing to centre yet');
	const ci = chart.nodes.find((n) => n.id === 'ci');
	assert.equal(scrollTo(chart, 'ci', view), 24 + (ci.position.x + 120) * 0.8 - 130);
	assert.equal(scrollTo(chart, 'start', view), 0, 'never before the chart');
	assert.equal(scrollTo(chart, 'missing', view), undefined);
});

test('a read-only view arranges a workflow stored top to bottom and keeps one already left to right', () => {
	const shown = leftToRight(templates.review_loop);
	assert.deepEqual(shown, tidy(templates.review_loop));
	const moved = tidy(templates.review_loop);
	moved.nodes = moved.nodes.map((n) => (n.id === 'failed' ? { ...n, position: { x: n.position.x, y: 300 } } : n));
	assert.equal(leftToRight(moved), moved, 'a hand-arranged horizontal workflow stays as saved');
});

test('the canvas opens on the whole graph when it fits and on its first steps when it would be too small', () => {
	const small = starterGraph({ start: 'Start', agent: 'Implementer', end: 'Done' });
	const fits = opening(small, { width: 1200, height: 600 });
	assert.equal(fits.zoom, 1);
	const box = extent(small);
	assert.equal(fits.x + (box.x + box.width / 2) * fits.zoom, 600, 'centred');
	const wide = tidy(templates.full_cycle);
	const phone = opening(wide, { width: 390, height: 352 });
	assert.equal(phone.zoom, 0.7, 'never smaller than readable');
	const start = wide.nodes.find((n) => n.id === 'start');
	const left = phone.x + start.position.x * phone.zoom;
	assert.ok(left >= 0 && left < 60, 'the start is at the left edge');
});
