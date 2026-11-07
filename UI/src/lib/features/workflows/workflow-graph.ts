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

export function isReviewer(role: string | undefined): boolean {
	return role === 'reviewer' || role === 'security_reviewer';
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
			outcomes = isReviewer(profile?.role)
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
			return isReviewer(profiles.find((p) => p.id === node.profile_id)?.role);
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
	const lowest = graph.nodes.reduce((y, node) => Math.max(y, node.position.y), -120);
	const node: WorkflowNode = {
		id: uniqueId(type, new Set(graph.nodes.map((n) => n.id))),
		type,
		label,
		position: position ?? { x: 0, y: lowest + 120 }
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
			...((graph.budget.max_iterations ?? 0) > 0 ? { max_iterations: Math.floor(graph.budget.max_iterations ?? 0) } : {}),
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
