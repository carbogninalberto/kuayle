import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	addNext,
	retries,
	reviewBlock,
	setRetry,
	duplicateNode,
	dropsDown,
	extent,
	layering,
	leftToRight,
	loopOffsets,
	nodeWidth,
	opening,
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

for (const [name, template] of Object.entries(templates)) {
	test(`arranging the ${name} template lays it out left to right without overlaps`, () => {
		const arranged = tidy(template);
		const at = (id) => arranged.nodes.find((n) => n.id === id);
		const { back } = layering(arranged);
		for (const edge of arranged.edges) {
			if (edge.source === edge.target) continue;
			const from = at(edge.source),
				to = at(edge.target);
			if (back.has(edge.id)) {
				// A loop returns to an earlier step, on its own arc above the path.
				assert.ok(to.position.x < from.position.x, `${edge.id} loops back to the left`);
				assert.ok(loopOffsets(arranged).get(edge.id) > 0);
			} else
				assert.ok(
					to.position.x >= from.position.x + nodeWidth(from.type) + 120,
					`${edge.id} runs to the right with room for its label`
				);
		}
		assert.deepEqual(overlaps(arranged), []);
		// The main path is one straight line; only failed endings leave it.
		for (const node of arranged.nodes)
			assert.equal(node.position.y, node.outcome === 'failed' ? 120 : 0, `${node.id} lane`);
		const offsets = [...loopOffsets(arranged).values()];
		assert.equal(new Set(offsets).size, offsets.length, 'each loop has its own height');
		// Only positions change.
		assert.deepEqual(
			arranged.nodes.map(({ position, ...rest }) => rest),
			template.nodes.map(({ position, ...rest }) => rest)
		);
		assert.deepEqual(arranged.edges, template.edges);
		assert.deepEqual(tidy(arranged), arranged, 'arranging again changes nothing');
	});
}

test('loops are nested by length and retries on the step itself take no arc', () => {
	const arranged = tidy(templates.full_cycle);
	const offsets = loopOffsets(arranged);
	const id = (source) => arranged.edges.find((e) => e.source === source && e.target === 'implement').id;
	assert.ok(offsets.get(id('tests')) < offsets.get(id('review')));
	assert.ok(offsets.get(id('review')) < offsets.get(id('ci')));
	assert.equal(offsets.size, 5);
	const box = extent(arranged);
	assert.ok(box.y <= -Math.max(...offsets.values()), 'the box leaves room for the loops above the steps');
	assert.equal(box.width, 176 + 7 * 240 + 176 + 8 * 140);
});

test('a failure leaves its step downwards, towards the failed ending in the lane below', () => {
	const arranged = tidy(templates.review_loop);
	const at = (id) => arranged.nodes.find((n) => n.id === id);
	assert.ok(dropsDown(at('implement'), at('failed')));
	assert.ok(dropsDown(at('ship'), at('failed')));
	assert.ok(!dropsDown(at('ship'), at('done')));
	assert.ok(!dropsDown(at('implement'), at('publish')));
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
