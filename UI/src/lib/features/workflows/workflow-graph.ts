import type {
	AgentProfile,
	WorkflowCondition,
	WorkflowEdge,
	WorkflowGraph,
	WorkflowIssue,
	WorkflowNode,
	WorkflowNodeType,
	WorkflowStep,
	TaskWorkflow
} from '$lib/api/workflows';

// Pure graph helpers for the workflow editor and the task's workflow view.
// The server validates every graph (BE/internal/domain/workflow.go); these
// only offer the choices it accepts and place its answers on the canvas.

/** Node types the palette adds; a workflow has exactly one start. */
export const PALETTE: WorkflowNodeType[] = ['agent', 'check', 'approval', 'publish', 'merge', 'end'];

/** What the add-step menus offer: the node types plus ready-made blocks. */
export type Block = WorkflowNodeType | 'review';
export const BLOCKS: Block[] = ['agent', 'review', 'check', 'approval', 'publish', 'merge', 'end'];

/** Whether a step that runs the profile reviews the published pull request. */
export function reviews(profile: Pick<AgentProfile, 'behaviour'> | undefined): boolean {
	return profile?.behaviour === 'reviews';
}

/** The conditions a transition leaving the node may use, "else" last. */
export function conditionsFor(node: WorkflowNode, profiles: AgentProfile[]): WorkflowCondition[] {
	let outcomes: WorkflowCondition[] = [];
	switch (node.type) {
		case 'start':
			return ['else'];
		case 'end':
			return [];
		case 'agent': {
			const profile = profiles.find((p) => p.id === node.profile_id);
			outcomes = reviews(profile)
				? ['review_approved', 'review_changes_requested', 'agent_failed']
				: ['agent_finished', 'agent_failed'];
			break;
		}
		case 'check':
			outcomes = ['checks_passed', 'checks_failed'];
			break;
		case 'approval':
			outcomes = ['approval_approved', 'approval_denied'];
			break;
		case 'publish':
			outcomes = ['published', 'publish_failed'];
			break;
		case 'merge':
			outcomes = ['merged'];
			break;
	}
	if ((node.timeout_minutes ?? 0) > 0 && waits(node, profiles)) outcomes.push('timeout');
	return [...outcomes, 'else'];
}

/**
 * Whether the task waits at the node for something outside it (a reviewer's
 * verdict, CI, a person, a merge): only those steps can time out.
 */
export function waits(node: WorkflowNode, profiles: AgentProfile[]): boolean {
	switch (node.type) {
		case 'approval':
		case 'merge':
			return true;
		case 'check':
			return node.check === 'ci';
		case 'agent':
			return reviews(profiles.find((p) => p.id === node.profile_id));
	}
	return false;
}

function uniqueId(prefix: string, taken: Set<string>): string {
	for (let n = 1; ; n++) {
		const id = `${prefix}-${n}`;
		if (!taken.has(id)) return id;
	}
}

/** A new node of the type, placed below the lowest node unless a position is given. */
export function newNode(
	graph: WorkflowGraph,
	type: WorkflowNodeType,
	label: string,
	position?: { x: number; y: number }
): WorkflowNode {
	const lowest = graph.nodes.reduce((y, node) => Math.max(y, node.position.y), -ROW);
	const node: WorkflowNode = {
		id: uniqueId(type, new Set(graph.nodes.map((n) => n.id))),
		type,
		label,
		position: position ?? { x: -nodeWidth(type) / 2, y: lowest + ROW }
	};
	if (type === 'check') node.check = 'tests';
	if (type === 'end') node.outcome = 'done';
	return node;
}

/**
 * Adds a transition, on the given condition or the first one the source does
 * not use yet. Returns the graph unchanged when the source has none left or
 * the connection is not allowed (into the start, out of an end).
 */
export function connect(
	graph: WorkflowGraph,
	source: string,
	target: string,
	profiles: AgentProfile[],
	condition?: WorkflowCondition
): WorkflowGraph {
	const from = graph.nodes.find((n) => n.id === source);
	const to = graph.nodes.find((n) => n.id === target);
	if (!from || !to || to.type === 'start') return graph;
	const used = new Set(graph.edges.filter((e) => e.source === source).map((e) => e.condition));
	const choice = condition ?? conditionsFor(from, profiles).find((c) => !used.has(c));
	if (!choice || used.has(choice) || !conditionsFor(from, profiles).includes(choice)) return graph;
	const edge: WorkflowEdge = {
		id: uniqueId(`${source}-${target}`, new Set(graph.edges.map((e) => e.id))),
		source,
		target,
		condition: choice
	};
	return { ...graph, edges: [...graph.edges, edge] };
}

export function removeNode(graph: WorkflowGraph, id: string): WorkflowGraph {
	return {
		...graph,
		nodes: graph.nodes.filter((n) => n.id !== id),
		edges: graph.edges.filter((e) => e.source !== id && e.target !== id)
	};
}

export function removeEdge(graph: WorkflowGraph, id: string): WorkflowGraph {
	return { ...graph, edges: graph.edges.filter((e) => e.id !== id) };
}

export function updateNode(graph: WorkflowGraph, id: string, patch: Partial<WorkflowNode>): WorkflowGraph {
	return { ...graph, nodes: graph.nodes.map((n) => (n.id === id ? { ...n, ...patch } : n)) };
}

export function updateEdge(graph: WorkflowGraph, id: string, patch: Partial<WorkflowEdge>): WorkflowGraph {
	return { ...graph, edges: graph.edges.map((e) => (e.id === id ? { ...e, ...patch } : e)) };
}

/** A minimal starting graph: start, one agent step and an end. */
export function starterGraph(labels: { start: string; agent: string; end: string }, profileId?: string): WorkflowGraph {
	return {
		nodes: [
			{ id: 'start', type: 'start', label: labels.start, position: { x: 0, y: 0 } },
			{ id: 'agent-1', type: 'agent', label: labels.agent, profile_id: profileId ?? null, position: { x: 0, y: 120 } },
			{ id: 'end-1', type: 'end', label: labels.end, outcome: 'done', position: { x: 0, y: 240 } }
		],
		edges: [
			{ id: 'start-agent-1', source: 'start', target: 'agent-1', condition: 'else' },
			{ id: 'agent-1-end-1', source: 'agent-1', target: 'end-1', condition: 'agent_finished' }
		],
		budget: {}
	};
}

/** The graph as the server expects it: no empty optional fields. */
export function graphForSave(graph: WorkflowGraph): WorkflowGraph {
	return {
		nodes: graph.nodes.map((node) => {
			const out: WorkflowNode = { id: node.id, type: node.type, label: node.label.trim(), position: node.position };
			if (node.type === 'agent' && node.profile_id) out.profile_id = node.profile_id;
			if ((node.type === 'agent' || node.type === 'approval') && node.prompt?.trim()) out.prompt = node.prompt;
			if (node.type === 'check') out.check = node.check ?? 'tests';
			if (node.type === 'end') out.outcome = node.outcome ?? 'done';
			if ((node.max_visits ?? 0) > 0) out.max_visits = Math.floor(node.max_visits ?? 0);
			if ((node.timeout_minutes ?? 0) > 0) out.timeout_minutes = Math.floor(node.timeout_minutes ?? 0);
			return out;
		}),
		edges: graph.edges.map(({ id, source, target, condition }) => ({ id, source, target, condition })),
		budget: {
			...((graph.budget.max_iterations ?? 0) > 0
				? { max_iterations: Math.floor(graph.budget.max_iterations ?? 0) }
				: {}),
			...((graph.budget.max_minutes ?? 0) > 0 ? { max_minutes: Math.floor(graph.budget.max_minutes ?? 0) } : {}),
			...(graph.budget.on_exhausted ? { on_exhausted: graph.budget.on_exhausted } : {})
		}
	};
}

/** Validation issues grouped by where the canvas shows them. */
export function placeIssues(issues: WorkflowIssue[]) {
	const nodes = new Map<string, WorkflowIssue[]>();
	const edges = new Map<string, WorkflowIssue[]>();
	const general: WorkflowIssue[] = [];
	for (const issue of issues) {
		if (issue.edge_id) edges.set(issue.edge_id, [...(edges.get(issue.edge_id) ?? []), issue]);
		else if (issue.node_id) nodes.set(issue.node_id, [...(nodes.get(issue.node_id) ?? []), issue]);
		else general.push(issue);
	}
	return { nodes, edges, general };
}

export interface FiredTransition {
	count: number;
	/** Why it fired the last time, as the runner recorded it. */
	reason: string;
}

/** The path a task took: the nodes it entered and each transition that fired. */
export function pathOf(steps: WorkflowStep[]) {
	const visited = new Set<string>();
	const fired = new Map<string, FiredTransition>();
	for (const step of [...steps].sort((a, b) => a.sequence - b.sequence)) {
		visited.add(step.node_id);
		if (!step.edge_id) continue;
		const previous = fired.get(step.edge_id);
		fired.set(step.edge_id, { count: (previous?.count ?? 0) + 1, reason: step.reason });
	}
	return { visited, fired };
}

/** The step that started an iteration (task generation), for its tag. */
export function iterationStep(steps: WorkflowStep[], generation: number): WorkflowStep | undefined {
	return steps.filter((s) => s.generation === generation).sort((a, b) => b.sequence - a.sequence)[0];
}

type RunState = Pick<TaskWorkflow, 'node' | 'visits' | 'steps'>;

/** A task's run as the canvas shows it: the current node, the path and the visit counts. */
export function runOverlay(run: RunState) {
	const { visited, fired } = pathOf(run.steps);
	// A Standard task records no steps: its state is placed on one node.
	if (run.node) visited.add(run.node);
	return { current: run.node, visited, visits: run.visits, fired };
}

export type PendingDecision =
	| { kind: 'approval'; sequence: number; question: string }
	| { kind: 'limit'; sequence: number };

/**
 * The decision a person can make on a run now, matching the server's Decide:
 * an approval node once the task awaits it, or a limit the run paused at.
 * The sequence names the step the person saw, so a stale decision is refused.
 */
export function pendingDecision(
	run: Pick<TaskWorkflow, 'graph' | 'node' | 'status' | 'can_decide' | 'sequence' | 'task_state'>
): PendingDecision | null {
	if (!run.can_decide) return null;
	if (run.status === 'paused') return { kind: 'limit', sequence: run.sequence };
	const node = run.graph.nodes.find((n) => n.id === run.node);
	if (run.status === 'waiting' && node?.type === 'approval' && run.task_state === 'awaiting_review')
		return { kind: 'approval', sequence: run.sequence, question: node.prompt ?? '' };
	return null;
}

// Canvas geometry, matching WorkflowNodeCard: start and end cards are
// narrower than the steps between them.
const ROW = 130;
const COLUMN = 300;
export const nodeWidth = (type: WorkflowNodeType) => (type === 'start' || type === 'end' ? 176 : 240);

/**
 * Orders the graph from its start: each node's rank is its longest distance
 * from the start over forward transitions, and back are the transitions that
 * loop to an earlier step (a review sending work back).
 */
export function layering(graph: WorkflowGraph): { rank: Map<string, number>; back: Set<string> } {
	const outgoing = new Map<string, WorkflowEdge[]>();
	for (const edge of graph.edges) outgoing.set(edge.source, [...(outgoing.get(edge.source) ?? []), edge]);
	const state = new Map<string, 'open' | 'done'>();
	const back = new Set<string>();
	const order: string[] = [];
	const visit = (id: string) => {
		state.set(id, 'open');
		for (const edge of outgoing.get(id) ?? []) {
			const seen = state.get(edge.target);
			if (seen === 'open') back.add(edge.id);
			else if (!seen) visit(edge.target);
		}
		state.set(id, 'done');
		order.unshift(id);
	};
	const start = graph.nodes.find((n) => n.type === 'start');
	if (start) visit(start.id);
	for (const node of graph.nodes) if (!state.has(node.id)) visit(node.id);
	const rank = new Map<string, number>(graph.nodes.map((n) => [n.id, 0]));
	for (const id of order)
		for (const edge of outgoing.get(id) ?? [])
			if (!back.has(edge.id)) rank.set(edge.target, Math.max(rank.get(edge.target) ?? 0, (rank.get(id) ?? 0) + 1));
	return { rank, back };
}

/**
 * Lays the graph out top to bottom by rank, each row centred. Failed endings
 * sit in a column to the right, level with the earliest step that leads to
 * them, so the main path stays straight.
 */
export function tidy(graph: WorkflowGraph): WorkflowGraph {
	const { rank, back } = layering(graph);
	const failed = (n: WorkflowNode) => n.type === 'end' && n.outcome === 'failed';
	const rows = new Map<number, WorkflowNode[]>();
	for (const node of graph.nodes) {
		if (failed(node)) continue;
		const r = rank.get(node.id) ?? 0;
		rows.set(r, [...(rows.get(r) ?? []), node]);
	}
	const positions = new Map<string, { x: number; y: number }>();
	let widest = 0;
	for (const [r, row] of rows) {
		row.forEach((node, i) => {
			const centre = (i - (row.length - 1) / 2) * COLUMN;
			widest = Math.max(widest, Math.abs(centre) + nodeWidth(node.type) / 2);
			positions.set(node.id, { x: centre - nodeWidth(node.type) / 2, y: r * ROW });
		});
	}
	let side = 0;
	for (const node of graph.nodes.filter(failed)) {
		const from = graph.edges.filter((e) => e.target === node.id && !back.has(e.id)).map((e) => rank.get(e.source) ?? 0);
		const r = from.length > 0 ? Math.min(...from) + 1 : (rank.get(node.id) ?? 0);
		positions.set(node.id, { x: widest + 80 + side * (nodeWidth('end') + 40), y: r * ROW });
		side++;
	}
	return { ...graph, nodes: graph.nodes.map((n) => ({ ...n, position: positions.get(n.id) ?? n.position })) };
}

/**
 * Adds a step of the given type below the source and connects them on the
 * source's first free condition. Returns the new graph and the step's ID; the
 * step stays unconnected when the source has no free condition (or is an end).
 */
export function addNext(
	graph: WorkflowGraph,
	source: string,
	type: WorkflowNodeType,
	label: string,
	profiles: AgentProfile[]
): { graph: WorkflowGraph; id: string } {
	const from = graph.nodes.find((n) => n.id === source);
	if (!from) return { graph, id: '' };
	const centre = from.position.x + nodeWidth(from.type) / 2;
	// Below the source, beside any step already there.
	let x = centre - nodeWidth(type) / 2;
	const y = from.position.y + ROW;
	while (graph.nodes.some((n) => Math.abs(n.position.y - y) < ROW / 2 && Math.abs(n.position.x - x) < COLUMN - 40))
		x += COLUMN;
	const node = newNode(graph, type, label, { x, y });
	const added = { ...graph, nodes: [...graph.nodes, node] };
	return { graph: connect(added, source, node.id, profiles), id: node.id };
}

/** Copies a step (not the start) beside itself, without its transitions. */
export function duplicateNode(graph: WorkflowGraph, id: string): { graph: WorkflowGraph; id: string } {
	const node = graph.nodes.find((n) => n.id === id);
	if (!node || node.type === 'start') return { graph, id: '' };
	const copy: WorkflowNode = {
		// A JSON copy: structuredClone refuses Svelte's reactive proxies.
		...(JSON.parse(JSON.stringify(node)) as WorkflowNode),
		id: uniqueId(node.type, new Set(graph.nodes.map((n) => n.id))),
		position: { x: node.position.x + 40, y: node.position.y + 40 }
	};
	return { graph: { ...graph, nodes: [...graph.nodes, copy] }, id: copy.id };
}

/**
 * The failure a step can retry on: an agent that failed, or a pull request
 * that could not be published. Other steps fail for reasons a rerun won't fix.
 */
export function retryCondition(node: WorkflowNode, profiles: AgentProfile[]): WorkflowCondition | undefined {
	if (node.type === 'publish') return 'publish_failed';
	if (node.type === 'agent' && conditionsFor(node, profiles).includes('agent_failed')) return 'agent_failed';
	return undefined;
}

/** Whether the step runs again when it fails (a transition back to itself on its failure). */
export function retries(graph: WorkflowGraph, node: WorkflowNode, profiles: AgentProfile[]): boolean {
	const condition = retryCondition(node, profiles);
	return (
		!!condition && graph.edges.some((e) => e.source === node.id && e.target === node.id && e.condition === condition)
	);
}

/**
 * Turns "retry on failure" on or off. On: the step's failure leads back to
 * itself, guarded by its visit limit (3 when it had none). Off: the failure
 * leads to the workflow's failed ending again, when it has one.
 */
export function setRetry(graph: WorkflowGraph, id: string, on: boolean, profiles: AgentProfile[]): WorkflowGraph {
	const node = graph.nodes.find((n) => n.id === id);
	const condition = node && retryCondition(node, profiles);
	if (!node || !condition) return graph;
	const edges = graph.edges.filter((e) => !(e.source === id && e.condition === condition));
	if (on) {
		const nodes = graph.nodes.map((n) => (n.id === id && !(n.max_visits ?? 0) ? { ...n, max_visits: 3 } : n));
		const edge: WorkflowEdge = {
			id: uniqueId(`${id}-retry`, new Set(edges.map((e) => e.id))),
			source: id,
			target: id,
			condition
		};
		return { ...graph, nodes, edges: [...edges, edge] };
	}
	const failed = graph.nodes.find((n) => n.type === 'end' && n.outcome === 'failed');
	const next = { ...graph, edges };
	return failed ? connect(next, id, failed.id, profiles, condition) : next;
}

/**
 * An automated review: an agent step run by the reviewer profile (the
 * built-in one first) that reviews the published pull request and retries if
 * the review agent itself fails. Its verdict transitions are left to connect.
 */
export function reviewBlock(
	graph: WorkflowGraph,
	label: string,
	profiles: AgentProfile[],
	position?: { x: number; y: number }
): { graph: WorkflowGraph; id: string } {
	const reviewer =
		profiles.find((p) => !p.archived_at && p.builtin_key === 'reviewer' && reviews(p)) ??
		profiles.find((p) => !p.archived_at && reviews(p));
	const node: WorkflowNode = {
		...newNode(graph, 'agent', label, position),
		profile_id: reviewer?.id ?? null,
		max_visits: 3
	};
	const added = { ...graph, nodes: [...graph.nodes, node] };
	return { graph: reviewer ? setRetry(added, node.id, true, profiles) : added, id: node.id };
}
