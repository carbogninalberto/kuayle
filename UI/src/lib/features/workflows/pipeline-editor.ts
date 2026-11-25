import type {
	AgentProfile,
	PipelineCheck,
	PipelineDefinition,
	PipelineMember,
	PipelinePhase,
	WorkflowGraph,
	WorkflowIssue,
	WorkflowNode,
	WorkflowNodeType
} from '$lib/api/workflows';
import { leftToRight } from './workflow-graph.ts';

// The pipeline editor's state (KUA-284): pure edits of a pipeline definition
// and the JSON a save sends. The server validates every pipeline
// (ValidatePipeline, BE/internal/domain/pipeline.go); these helpers only
// offer the choices it accepts and never decide what is valid. Every edit
// returns a new definition and leaves its input as it was.

/** Who owns a phase besides a role, and who decides besides a person. */
export const KUAYLE = 'kuayle';
export const HUMAN = 'human';
export const LEAD = 'lead';

/** The facts a check can require, a closed list on the server, in the order the editor offers them. */
export const PIPELINE_FACTS = [
	'pr_exists',
	'pr_draft',
	'head_contains_base',
	'conflict',
	'migration_collision',
	'ci_green',
	'verdict',
	'approved',
	'merged',
	'closed',
	'overlap',
	'script'
] as const;
export type PipelineFact = (typeof PIPELINE_FACTS)[number];

/** The server's bounds, for the fields' limits. */
export const PIPELINE_LIMITS = { phases: 50, roster: 10, checks: 20, promptBytes: 4000, turns: 200, minutes: 43200 };

const ROLE_PATTERN = /^[a-z][a-z0-9_-]{0,31}$/;
const RESERVED_ROLES = [KUAYLE, HUMAN, LEAD, 'all'];

/** Whether the server takes this as a role name: agents address it as @role. */
export const validRole = (role: string) => ROLE_PATTERN.test(role) && !RESERVED_ROLES.includes(role);

/** base, else base-2, base-3… : the first one nothing has taken. */
function freeId(base: string, taken: Iterable<string>): string {
	const used = new Set(taken);
	let id = base;
	for (let n = 2; used.has(id); n++) id = `${base}-${n}`;
	return id;
}

const checkIds = (p: PipelineDefinition) => p.phases.flatMap((phase) => (phase.checks ?? []).map((c) => c.id));

/** The role that writes the code, when the roster has one. */
export const writerRole = (p: PipelineDefinition) => p.roster.find((m) => m.writer)?.role;

/** A pipeline to start from: one phase, worked by the role that writes the code. */
export function starterPipeline(label: string, profileId?: string): PipelineDefinition {
	return {
		kind: 'pipeline',
		phases: [{ id: 'implement', label, owner: 'implementer' }],
		roster: [{ role: 'implementer', profile_id: profileId ?? '', writer: true }]
	};
}

/** The editor's own copy of a pipeline (a stored one, or a draft). */
export function editablePipeline(p: PipelineDefinition): PipelineDefinition {
	return JSON.parse(JSON.stringify(p));
}

function checkForSave(check: PipelineCheck): PipelineCheck {
	const out: PipelineCheck = { id: check.id, fact: check.fact };
	if (check.fact === 'verdict' && check.role) out.role = check.role;
	if (check.fact === 'script' && check.script) out.script = check.script;
	if (check.required) out.required = true;
	return out;
}

function phaseForSave(phase: PipelinePhase): PipelinePhase {
	const out: PipelinePhase = { id: phase.id, label: phase.label.trim(), owner: phase.owner };
	if (phase.prompt?.trim()) out.prompt = phase.prompt;
	const checks = (phase.checks ?? []).map(checkForSave);
	if (checks.length) out.checks = checks;
	if (phase.guarded) out.guarded = true;
	return out;
}

function memberForSave(member: PipelineMember): PipelineMember {
	// A role without a profile goes without one: the server then reports it
	// on the role instead of refusing the whole document.
	const out = { role: member.role } as PipelineMember;
	if (member.profile_id) out.profile_id = member.profile_id;
	if (member.writer) out.writer = true;
	if (member.on_call) out.on_call = true;
	return out;
}

const whole = (value: number | undefined) => Math.floor(Number(value) || 0);

/**
 * The pipeline as a save sends it: no empty or irrelevant fields, a waiver
 * only for a required check that exists and that is not a person's (the
 * default), and no lead without a profile.
 */
export function pipelineForSave(p: PipelineDefinition): PipelineDefinition {
	const out: PipelineDefinition = {
		kind: 'pipeline',
		phases: p.phases.map(phaseForSave),
		roster: p.roster.map(memberForSave)
	};
	if (p.lead?.profile_id) out.lead = { profile_id: p.lead.profile_id };
	const required = new Set(
		p.phases.flatMap((phase) => (phase.checks ?? []).filter((c) => c.required).map((c) => c.id))
	);
	const waive = Object.fromEntries(
		Object.entries(p.autonomy?.waive ?? {}).filter(([id, who]) => who && who !== HUMAN && required.has(id))
	);
	const autonomy = {
		...(p.autonomy?.approve ? { approve: p.autonomy.approve } : {}),
		...(Object.keys(waive).length ? { waive } : {})
	};
	if (Object.keys(autonomy).length) out.autonomy = autonomy;
	const budget = {
		...(whole(p.budget?.max_turns) > 0 ? { max_turns: whole(p.budget?.max_turns) } : {}),
		...(whole(p.budget?.max_minutes) > 0 ? { max_minutes: whole(p.budget?.max_minutes) } : {}),
		...(p.budget?.on_exhausted ? { on_exhausted: p.budget.on_exhausted } : {})
	};
	if (Object.keys(budget).length) out.budget = budget;
	return out;
}

/** The kind of step a phase is drawn as: a person approves, Kuayle publishes or merges, a role works. */
export function phaseNodeType(phase: Pick<PipelinePhase, 'owner' | 'guarded'>): WorkflowNodeType {
	if (phase.owner === HUMAN) return 'approval';
	if (phase.owner === KUAYLE) return phase.guarded ? 'merge' : 'publish';
	return 'agent';
}

/**
 * The pipeline as the workflow canvas draws it: its phases in order, left to
 * right, each joined to the next. No loop edges: going back is implied by an
 * unmet check.
 */
export function pipelineGraph(p: PipelineDefinition): WorkflowGraph {
	const nodes: WorkflowNode[] = [];
	const seen = new Set<string>();
	for (const phase of p.phases) {
		if (seen.has(phase.id)) continue;
		seen.add(phase.id);
		const type = phaseNodeType(phase);
		const node: WorkflowNode = { id: phase.id, type, label: phase.label.trim() || phase.id, position: { x: 0, y: 0 } };
		if (type === 'agent') node.profile_id = p.roster.find((m) => m.role === phase.owner)?.profile_id ?? null;
		nodes.push(node);
	}
	const edges = nodes.slice(1).map((node, i) => ({
		id: `${nodes[i].id}>${node.id}`,
		source: nodes[i].id,
		target: node.id,
		condition: 'else' as const
	}));
	return leftToRight({ nodes, edges, budget: {} });
}

const guardedLast = (p: PipelineDefinition) => !!p.phases.at(-1)?.guarded;

/**
 * Adds a phase after the given one, or at the end; never after a guarded
 * merge, which stays last. The role that writes the code owns it.
 */
export function addPhase(
	p: PipelineDefinition,
	label: string,
	after?: string
): { pipeline: PipelineDefinition; id: string } {
	const id = freeId(
		'phase',
		p.phases.map((phase) => phase.id)
	);
	const owner = writerRole(p) ?? p.roster[0]?.role ?? HUMAN;
	const found = after ? p.phases.findIndex((phase) => phase.id === after) : -1;
	let at = found >= 0 ? found + 1 : p.phases.length;
	if (guardedLast(p)) at = Math.min(at, p.phases.length - 1);
	const phases = [...p.phases];
	phases.splice(at, 0, { id, label, owner });
	return { pipeline: { ...p, phases }, id };
}

export function updatePhase(p: PipelineDefinition, id: string, patch: Partial<PipelinePhase>): PipelineDefinition {
	return { ...p, phases: p.phases.map((phase) => (phase.id === id ? { ...phase, ...patch } : phase)) };
}

function withoutWaivers(p: PipelineDefinition, ids: string[]): PipelineDefinition {
	const waive = { ...(p.autonomy?.waive ?? {}) };
	if (!ids.some((id) => id in waive)) return p;
	for (const id of ids) delete waive[id];
	return { ...p, autonomy: { ...p.autonomy, waive } };
}

/** Removes a phase, its checks and their waivers. */
export function removePhase(p: PipelineDefinition, id: string): PipelineDefinition {
	const phase = p.phases.find((candidate) => candidate.id === id);
	const next = { ...p, phases: p.phases.filter((candidate) => candidate.id !== id) };
	return withoutWaivers(
		next,
		(phase?.checks ?? []).map((c) => c.id)
	);
}

/** Whether a phase can move one place earlier (-1) or later (1): a guarded merge stays last. */
export function canMove(p: PipelineDefinition, id: string, delta: -1 | 1): boolean {
	const i = p.phases.findIndex((phase) => phase.id === id);
	const j = i + delta;
	return i >= 0 && j >= 0 && j < p.phases.length && !p.phases[i].guarded && !p.phases[j].guarded;
}

export function movePhase(p: PipelineDefinition, id: string, delta: -1 | 1): PipelineDefinition {
	if (!canMove(p, id, delta)) return p;
	const i = p.phases.findIndex((phase) => phase.id === id);
	const phases = [...p.phases];
	[phases[i], phases[i + delta]] = [phases[i + delta], phases[i]];
	return { ...p, phases };
}

/** Adds a required CI check to a phase, with an ID no other check of the pipeline has. */
export function addCheck(p: PipelineDefinition, phaseId: string): { pipeline: PipelineDefinition; id: string } {
	const id = freeId('ci-green', checkIds(p));
	const check: PipelineCheck = { id, fact: 'ci_green', required: true };
	return {
		pipeline: updatePhase(p, phaseId, { checks: [...(p.phases.find((ph) => ph.id === phaseId)?.checks ?? []), check] }),
		id
	};
}

/**
 * Changes a check. A new fact keeps only what that fact takes: a verdict
 * names the first role that does not write the code, a script starts with no
 * command.
 */
export function updateCheck(
	p: PipelineDefinition,
	phaseId: string,
	checkId: string,
	patch: Partial<PipelineCheck>
): PipelineDefinition {
	const phase = p.phases.find((candidate) => candidate.id === phaseId);
	if (!phase) return p;
	const checks = (phase.checks ?? []).map((check) => {
		if (check.id !== checkId) return check;
		const next: PipelineCheck = { ...check, ...patch };
		if (patch.fact !== undefined && patch.fact !== check.fact) {
			delete next.role;
			delete next.script;
			const reviewer = p.roster.find((m) => !m.writer)?.role;
			if (next.fact === 'verdict' && reviewer) next.role = reviewer;
			if (next.fact === 'script') next.script = '';
		}
		return next;
	});
	return updatePhase(p, phaseId, { checks });
}

/** Removes a check and its waiver. */
export function removeCheck(p: PipelineDefinition, phaseId: string, checkId: string): PipelineDefinition {
	const phase = p.phases.find((candidate) => candidate.id === phaseId);
	const next = updatePhase(p, phaseId, { checks: (phase?.checks ?? []).filter((c) => c.id !== checkId) });
	return withoutWaivers(next, [checkId]);
}

/** Who may waive a check: a person, unless the lead was given it. */
export const waiver = (p: PipelineDefinition, checkId: string): 'human' | 'lead' =>
	p.autonomy?.waive?.[checkId] === LEAD ? LEAD : HUMAN;

export function setWaiver(p: PipelineDefinition, checkId: string, who: 'human' | 'lead'): PipelineDefinition {
	const waive = { ...(p.autonomy?.waive ?? {}) };
	if (who === LEAD) waive[checkId] = LEAD;
	else delete waive[checkId];
	return { ...p, autonomy: { ...p.autonomy, waive } };
}

const ROLE_BY_BEHAVIOUR: Record<string, string> = {
	reviews: 'reviewer',
	changes_code: 'implementer',
	plans: 'planner'
};

/** Adds a role played by the profile, named after what the profile does. */
export function addRole(
	p: PipelineDefinition,
	profile: Pick<AgentProfile, 'id' | 'behaviour'>
): { pipeline: PipelineDefinition; role: string } {
	const role = freeId(
		ROLE_BY_BEHAVIOUR[profile.behaviour] ?? 'member',
		p.roster.map((m) => m.role)
	);
	return { pipeline: { ...p, roster: [...p.roster, { role, profile_id: profile.id }] }, role };
}

/**
 * Changes the role at a position of the roster. A rename carries the phases
 * the role owns and the verdicts it gives, unless another role has, or had,
 * that name: while a name is typed, two roles may share one for a moment.
 */
export function updateRole(p: PipelineDefinition, index: number, patch: Partial<PipelineMember>): PipelineDefinition {
	const member = p.roster[index];
	if (!member) return p;
	const roster = p.roster.map((m, i) => (i === index ? { ...m, ...patch } : m));
	const from = member.role,
		to = patch.role;
	const others = p.roster.filter((_, i) => i !== index).map((m) => m.role);
	if (to === undefined || to === from || others.includes(from) || others.includes(to)) return { ...p, roster };
	return {
		...p,
		roster,
		phases: p.phases.map((phase) => ({
			...phase,
			owner: phase.owner === from ? to : phase.owner,
			...(phase.checks
				? { checks: phase.checks.map((c) => (c.fact === 'verdict' && c.role === from ? { ...c, role: to } : c)) }
				: {})
		}))
	};
}

/** Removes a role. What still names it is left for the validator to point out. */
export function removeRole(p: PipelineDefinition, index: number): PipelineDefinition {
	return { ...p, roster: p.roster.filter((_, i) => i !== index) };
}

/** Makes the role at a position the one that writes the code; a writer is never only on call. */
export function setWriter(p: PipelineDefinition, index: number): PipelineDefinition {
	return {
		...p,
		roster: p.roster.map((m, i) => {
			const { writer: _writer, on_call: onCall, ...rest } = m;
			return i === index ? { ...rest, writer: true } : { ...rest, ...(onCall ? { on_call: true } : {}) };
		})
	};
}

/** Sets the lead's profile, or removes the lead: what it approved or could waive goes back to a person. */
export function setLead(p: PipelineDefinition, profileId: string | null): PipelineDefinition {
	if (profileId) return { ...p, lead: { profile_id: profileId } };
	const { lead: _lead, ...rest } = p;
	const waive = Object.fromEntries(Object.entries(p.autonomy?.waive ?? {}).filter(([, who]) => who !== LEAD));
	return {
		...rest,
		autonomy: { ...p.autonomy, ...(p.autonomy?.approve === LEAD ? { approve: HUMAN } : {}), waive }
	};
}

const ROSTER_CODES = ['invalid_role', 'duplicate_role', 'missing_profile', 'invalid_writer', 'writer_count'];
const SETTINGS_CODES = ['invalid_autonomy', 'invalid_budget'];

/** The tab of the editor that changes what a validator issue is about. */
export function issueSection(issue: WorkflowIssue): 'phases' | 'roles' | 'settings' {
	if (issue.node_id) return 'phases';
	if (ROSTER_CODES.includes(issue.code)) return 'roles';
	if (SETTINGS_CODES.includes(issue.code)) return 'settings';
	return 'phases';
}
