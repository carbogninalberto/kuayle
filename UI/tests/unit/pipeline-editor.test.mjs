import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	PIPELINE_FACTS,
	addCheck,
	addPhase,
	addRole,
	canMove,
	editablePipeline,
	issueSection,
	movePhase,
	phaseNodeType,
	pipelineForSave,
	pipelineGraph,
	removeCheck,
	removePhase,
	removeRole,
	setLead,
	setWaiver,
	setWriter,
	starterPipeline,
	updateCheck,
	updatePhase,
	updateRole,
	validRole,
	waiver,
	writerRole
} from '../../src/lib/features/workflows/pipeline-editor.ts';

// The pipeline editor's state (KUA-284): pure edits of a pipeline definition
// and the JSON the server's ValidatePipeline (BE/internal/domain/pipeline.go)
// is given on save.

const team = () => ({
	kind: 'pipeline',
	phases: [
		{ id: 'implement', label: 'Implement', owner: 'implementer', prompt: 'Build it' },
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
				{ id: 'uiux', fact: 'verdict', role: 'uiux' }
			]
		},
		{ id: 'approval', label: 'Approval', owner: 'human', checks: [{ id: 'owner', fact: 'approved', required: true }] },
		{ id: 'merge', label: 'Merge', owner: 'kuayle', guarded: true }
	],
	roster: [
		{ role: 'implementer', profile_id: 'p-impl', writer: true },
		{ role: 'reviewer', profile_id: 'p-rev' },
		{ role: 'uiux', profile_id: 'p-ux', on_call: true }
	],
	autonomy: { approve: 'human', waive: { reviewer: 'human' } },
	budget: { max_turns: 40, max_minutes: 2880, on_exhausted: 'ask' }
});

test('the facts offered are the closed list the server accepts', () => {
	assert.deepEqual([...PIPELINE_FACTS].sort(), [
		'approved',
		'ci_green',
		'closed',
		'conflict',
		'head_contains_base',
		'merged',
		'migration_collision',
		'overlap',
		'pr_draft',
		'pr_exists',
		'script',
		'verdict'
	]);
});

test('a starter pipeline is one phase owned by the writer, valid as saved', () => {
	const p = starterPipeline('Implement', 'p-impl');
	assert.deepEqual(pipelineForSave(p), {
		kind: 'pipeline',
		phases: [{ id: 'implement', label: 'Implement', owner: 'implementer' }],
		roster: [{ role: 'implementer', profile_id: 'p-impl', writer: true }]
	});
	assert.equal(writerRole(p), 'implementer');
});

test('a saved pipeline round-trips through the editor unchanged', () => {
	const saved = pipelineForSave(team());
	assert.deepEqual(pipelineForSave(editablePipeline(saved)), saved);
	// The editor works on its own copy.
	const original = team();
	const copy = editablePipeline(original);
	copy.phases[0].label = 'Changed';
	assert.equal(original.phases[0].label, 'Implement');
});

test('saving drops empty and irrelevant fields instead of sending them', () => {
	let p = editablePipeline(team());
	p = updatePhase(p, 'implement', { label: '  Implement  ', prompt: '   ' });
	p = updateCheck(p, 'pr', 'pr-exists', { required: false });
	p = { ...p, budget: { max_turns: 0, max_minutes: 12.7, on_exhausted: undefined } };
	const out = pipelineForSave(p);
	assert.deepEqual(out.phases[0], { id: 'implement', label: 'Implement', owner: 'implementer' });
	assert.deepEqual(out.phases[1].checks, [{ id: 'pr-exists', fact: 'pr_exists' }]);
	assert.deepEqual(out.budget, { max_minutes: 12 });
	assert.equal('lead' in out, false);
});

test('a waiver is kept only for a required check that still exists, and the default is a person', () => {
	let p = setLead(editablePipeline(team()), 'p-lead');
	p = setWaiver(p, 'reviewer', 'lead');
	p = setWaiver(p, 'owner', 'lead');
	p = setWaiver(p, 'pr-exists', 'lead');
	assert.equal(waiver(p, 'reviewer'), 'lead');
	assert.equal(waiver(p, 'uiux'), 'human');
	p = updateCheck(p, 'approval', 'owner', { required: false });
	p = removeCheck(p, 'pr', 'pr-exists');
	assert.deepEqual(pipelineForSave(p).autonomy, { approve: 'human', waive: { reviewer: 'lead' } });
	p = setWaiver(p, 'reviewer', 'human');
	assert.deepEqual(pipelineForSave(p).autonomy, { approve: 'human' });
});

test('removing the lead hands its approvals and waivers back to a person', () => {
	let p = setLead(editablePipeline(team()), 'p-lead');
	p = { ...p, autonomy: { ...p.autonomy, approve: 'lead' } };
	p = setWaiver(p, 'reviewer', 'lead');
	assert.deepEqual(pipelineForSave(p).lead, { profile_id: 'p-lead' });
	p = setLead(p, null);
	const out = pipelineForSave(p);
	assert.equal('lead' in out, false);
	assert.deepEqual(out.autonomy, { approve: 'human' });
});

test('a new phase goes after the selected one, never after the guarded merge', () => {
	let p = editablePipeline(team());
	const afterReview = addPhase(p, 'New phase', 'review');
	assert.deepEqual(
		afterReview.pipeline.phases.map((phase) => phase.id),
		['implement', 'pr', 'review', afterReview.id, 'approval', 'merge']
	);
	const atEnd = addPhase(p, 'New phase');
	assert.deepEqual(atEnd.pipeline.phases.at(-2).id, atEnd.id);
	assert.equal(atEnd.pipeline.phases.find((phase) => phase.id === atEnd.id).owner, 'implementer');
	// IDs stay unique.
	const again = addPhase(atEnd.pipeline, 'New phase');
	assert.notEqual(again.id, atEnd.id);
	p = starterPipeline('Implement', 'p');
	assert.deepEqual(
		addPhase(p, 'Next').pipeline.phases.map((phase) => phase.label),
		['Implement', 'Next']
	);
});

test('phases move within the pipeline but the guarded merge stays last', () => {
	const p = editablePipeline(team());
	assert.equal(canMove(p, 'implement', -1), false);
	assert.equal(canMove(p, 'approval', 1), false);
	assert.equal(canMove(p, 'merge', -1), false);
	assert.equal(canMove(p, 'pr', -1), true);
	assert.deepEqual(
		movePhase(p, 'pr', -1).phases.map((phase) => phase.id),
		['pr', 'implement', 'review', 'approval', 'merge']
	);
	assert.equal(movePhase(p, 'approval', 1), p);
});

test('removing a phase takes its checks and their waivers with it', () => {
	let p = setLead(editablePipeline(team()), 'p-lead');
	p = setWaiver(p, 'reviewer', 'lead');
	p = removePhase(p, 'review');
	assert.deepEqual(
		p.phases.map((phase) => phase.id),
		['implement', 'pr', 'approval', 'merge']
	);
	assert.equal(pipelineForSave(p).autonomy.waive, undefined);
});

test('a new check has an ID unique across the pipeline', () => {
	let p = editablePipeline(team());
	const first = addCheck(p, 'implement');
	const second = addCheck(first.pipeline, 'pr');
	assert.notEqual(first.id, second.id);
	const ids = second.pipeline.phases.flatMap((phase) => phase.checks ?? []).map((check) => check.id);
	assert.equal(new Set(ids).size, ids.length);
	assert.deepEqual(
		second.pipeline.phases[0].checks.find((c) => c.id === first.id),
		{ id: first.id, fact: 'ci_green', required: true }
	);
});

test('changing a fact keeps only what that fact takes', () => {
	let p = editablePipeline(team());
	p = updateCheck(p, 'review', 'reviewer', { fact: 'script' });
	assert.deepEqual(p.phases[2].checks[0], { id: 'reviewer', fact: 'script', script: '', required: true });
	p = updateCheck(p, 'review', 'reviewer', { script: 'npm test' });
	p = updateCheck(p, 'review', 'reviewer', { fact: 'verdict' });
	// A verdict names a role that does not write: the first one.
	assert.deepEqual(p.phases[2].checks[0], { id: 'reviewer', fact: 'verdict', role: 'reviewer', required: true });
	p = updateCheck(p, 'review', 'reviewer', { fact: 'ci_green' });
	assert.deepEqual(pipelineForSave(p).phases[2].checks[0], { id: 'reviewer', fact: 'ci_green', required: true });
});

test('roles: one writer, renames follow their phases and checks, removal leaves the rest to the validator', () => {
	// Roles are edited by position: while a name is being typed, two may share it.
	let p = editablePipeline(team());
	p = setWriter(p, 2);
	assert.equal(writerRole(p), 'uiux');
	assert.deepEqual(
		p.roster.map((m) => [m.role, !!m.writer, !!m.on_call]),
		[
			['implementer', false, false],
			['reviewer', false, false],
			['uiux', true, false]
		]
	);
	p = setWriter(p, 0);
	p = updateRole(p, 0, { role: 'builder' });
	assert.deepEqual(
		p.phases.map((phase) => phase.owner),
		['builder', 'kuayle', 'builder', 'human', 'kuayle']
	);
	p = updateRole(p, 1, { role: 'critic' });
	assert.equal(p.phases[2].checks[0].role, 'critic');
	// A name another role has, or had, is not a rename of that role's references.
	p = updateRole(p, 1, { role: 'uiux' });
	assert.equal(p.phases[2].checks[0].role, 'critic');
	assert.equal(p.phases[2].checks[1].role, 'uiux');
	p = updateRole(p, 1, { role: 'critic' });
	assert.equal(p.phases[2].checks[1].role, 'uiux');
	p = updateRole(p, 1, { profile_id: 'p-other', on_call: true });
	assert.deepEqual(p.roster[1], { role: 'critic', profile_id: 'p-other', on_call: true });
	const added = addRole(p, { id: 'p-rev', behaviour: 'reviews' });
	assert.equal(added.role, 'reviewer');
	assert.equal(added.pipeline.roster.length, 4);
	assert.equal(addRole(added.pipeline, { id: 'p-rev', behaviour: 'reviews' }).role, 'reviewer-2');
	p = removeRole(added.pipeline, 0);
	assert.equal(writerRole(p), undefined);
	assert.equal(p.phases[0].owner, 'builder');
	// A role without a profile is sent without one, which the server reports on the role.
	assert.equal('profile_id' in pipelineForSave(updateRole(p, 0, { profile_id: '' })).roster[0], false);
});

test('role names follow the server rule', () => {
	assert.equal(validRole('reviewer'), true);
	assert.equal(validRole('ui-ux_2'), true);
	for (const bad of ['', 'Reviewer', '2nd', 'kuayle', 'human', 'lead', 'all', 'a'.repeat(33), 'with space'])
		assert.equal(validRole(bad), false, bad);
});

test('the canvas draws the phases in a row, left to right, with no loop edges', () => {
	const graph = pipelineGraph(team());
	assert.deepEqual(
		graph.nodes.map((node) => [node.id, node.type, node.label]),
		[
			['implement', 'agent', 'Implement'],
			['pr', 'publish', 'Pull request'],
			['review', 'agent', 'Review'],
			['approval', 'approval', 'Approval'],
			['merge', 'merge', 'Merge']
		]
	);
	assert.equal(graph.nodes[0].profile_id, 'p-impl');
	assert.deepEqual(
		graph.edges.map((edge) => `${edge.source}>${edge.target}`),
		['implement>pr', 'pr>review', 'review>approval', 'approval>merge']
	);
	const xs = graph.nodes.map((node) => node.position.x);
	assert.deepEqual(
		xs,
		[...xs].sort((a, b) => a - b)
	);
	assert.equal(new Set(xs).size, xs.length);
	assert.equal(new Set(graph.nodes.map((node) => node.position.y)).size, 1);
	// A phase without a name still shows on the canvas.
	assert.equal(pipelineGraph(updatePhase(team(), 'pr', { label: ' ' })).nodes[1].label, 'pr');
});

test('a phase is drawn as the kind of step its owner makes it', () => {
	assert.equal(phaseNodeType({ owner: 'human' }), 'approval');
	assert.equal(phaseNodeType({ owner: 'kuayle' }), 'publish');
	assert.equal(phaseNodeType({ owner: 'kuayle', guarded: true }), 'merge');
	assert.equal(phaseNodeType({ owner: 'reviewer' }), 'agent');
});

test('validator issues land on the tab that edits what they name', () => {
	assert.equal(issueSection({ code: 'invalid_check', node_id: 'review', message: '' }), 'phases');
	assert.equal(issueSection({ code: 'writer_count', message: '' }), 'roles');
	assert.equal(issueSection({ code: 'duplicate_role', message: '' }), 'roles');
	assert.equal(issueSection({ code: 'missing_profile', message: '' }), 'roles');
	assert.equal(issueSection({ code: 'invalid_autonomy', message: '' }), 'settings');
	assert.equal(issueSection({ code: 'invalid_budget', message: '' }), 'settings');
	assert.equal(issueSection({ code: 'pipeline_size', message: '' }), 'phases');
});
