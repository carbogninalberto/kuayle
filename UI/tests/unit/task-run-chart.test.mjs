import { test } from 'node:test';
import assert from 'node:assert/strict';
import { taskRunChart } from '../../src/lib/features/workflows/task-run-chart.ts';

const pipeline = {
	kind: 'pipeline',
	phases: [
		{ id: 'write', label: 'Implement', owner: 'writer' },
		{ id: 'review', label: 'Peer review', owner: 'reviewer' },
		{ id: 'merge', label: 'Merge', owner: 'kuayle', guarded: true }
	],
	roster: [
		{ role: 'writer', profile_id: 'writer-profile', writer: true },
		{ role: 'reviewer', profile_id: 'reviewer-profile' }
	]
};

const run = { node: 'review', steps: [], visits: {}, task_state: 'running', status: 'running', task_reason: '' };

test('the run draws every phase left to right and highlights the persisted phase with or without a lead', () => {
	for (const graph of [pipeline, { ...pipeline, lead: { profile_id: 'lead-profile' } }]) {
		const chart = taskRunChart({ ...run, graph });
		assert.deepEqual(chart.graph.nodes.map((node) => node.label), ['Implement', 'Peer review', 'Merge']);
		assert.deepEqual(chart.graph.edges.map(({ source, target }) => [source, target]), [
			['write', 'review'], ['review', 'merge']
		]);
		const [write, review, merge] = chart.graph.nodes.map((node) => node.position);
		assert.ok(write.x < review.x && review.x < merge.x);
		assert.equal(write.y, merge.y);
		assert.equal(chart.overlay.current, 'review');
		assert.equal(chart.overlay.focus, 'review');
		// The lead may revisit an earlier phase. This does not create a loop or
		// mark intervening phases complete.
		const earlier = taskRunChart({ ...run, graph, node: 'write' });
		assert.equal(earlier.overlay.current, 'write');
		assert.deepEqual(earlier.graph.edges, chart.graph.edges);
		assert.equal(earlier.overlay.visited.has('review'), false);
	}
});

test('a finished pipeline keeps all its phases and centres its last one without an active step', () => {
	const chart = taskRunChart({ ...run, graph: pipeline, node: 'merge', task_state: 'completed', status: 'done' });
	assert.equal(chart.overlay.current, null);
	assert.equal(chart.overlay.focus, 'merge');
	assert.deepEqual(chart.graph.nodes.map((node) => node.id), ['write', 'review', 'merge']);
});
