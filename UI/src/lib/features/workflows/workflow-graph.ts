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

/** A new node of the type, placed right of the last step unless a position is given. */
export function newNode(
	graph: WorkflowGraph,
	type: WorkflowNodeType,
	label: string,
	position?: { x: number; y: number }
): WorkflowNode {
	const right = graph.nodes.reduce((x, node) => Math.max(x, node.position.x + nodeWidth(node.type) + GAP), 0);
	const node: WorkflowNode = {
		id: uniqueId(type, new Set(graph.nodes.map((n) => n.id))),
		type,
		label,
		position: position ?? { x: right, y: 0 }
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

/** A minimal starting graph, left to right: start, one agent step and an end. */
export function starterGraph(labels: { start: string; agent: string; end: string }, profileId?: string): WorkflowGraph {
	return tidy({
		nodes: [
			{ id: 'start', type: 'start', label: labels.start, position: { x: 0, y: 0 } },
			{ id: 'agent-1', type: 'agent', label: labels.agent, profile_id: profileId ?? null, position: { x: 0, y: 0 } },
			{ id: 'end-1', type: 'end', label: labels.end, outcome: 'done', position: { x: 0, y: 0 } }
		],
		edges: [
			{ id: 'start-agent-1', source: 'start', target: 'agent-1', condition: 'else' },
			{ id: 'agent-1-end-1', source: 'agent-1', target: 'end-1', condition: 'agent_finished' }
		],
		budget: {}
	});
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
// narrower than the steps between them. Steps run left to right, GAP apart
// (room for a transition's label); branches sit in lanes below the main path
// and loops arc above it.
const GAP = 140;
const LANE = 120;
const NODE_HEIGHT = 50;
const LOOP_BASE = 28;
const LOOP_STEP = 20;
/** The smallest zoom at which a step's name still reads. */
const READABLE = 0.7;
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

// The outcome a workflow hopes for at each step, most hoped-for first: among
// equally long ways on, the main path follows these.
const HOPED: WorkflowCondition[] = [
	'agent_finished',
	'checks_passed',
	'review_approved',
	'approval_approved',
	'published',
	'merged',
	'else'
];
const failedEnd = (n: WorkflowNode) => (n.type === 'end' && n.outcome === 'failed' ? 1 : 0);
const hoped = (condition: WorkflowCondition) => (HOPED.includes(condition) ? HOPED.indexOf(condition) : HOPED.length);

/**
 * Arranges the graph left to right: one column per rank. The main path (the
 * longest way from the start, on the hoped-for outcomes) is the top lane; the
 * other steps of a rank sit in lanes below it, branches before failed endings,
 * each under the lane it comes from. The result depends on the transitions
 * only, never on the order the steps are stored in. Only positions change.
 */
export function tidy(graph: WorkflowGraph): WorkflowGraph {
	const { rank, back } = layering(graph);
	const nodes = new Map(graph.nodes.map((n) => [n.id, n]));
	const forward = graph.edges.filter(
		(e) => !back.has(e.id) && e.source !== e.target && nodes.has(e.source) && nodes.has(e.target)
	);
	const outgoing = new Map<string, WorkflowEdge[]>();
	const incoming = new Map<string, WorkflowEdge[]>();
	for (const edge of forward) {
		outgoing.set(edge.source, [...(outgoing.get(edge.source) ?? []), edge]);
		incoming.set(edge.target, [...(incoming.get(edge.target) ?? []), edge]);
	}
	// How many steps still follow a step, at most.
	const ahead = new Map<string, number>();
	const stepsAhead = (id: string): number => {
		const known = ahead.get(id);
		if (known !== undefined) return known;
		ahead.set(id, 0);
		const most = Math.max(-1, ...(outgoing.get(id) ?? []).map((e) => stepsAhead(e.target))) + 1;
		ahead.set(id, most);
		return most;
	};
	const main = new Set<string>();
	let at = graph.nodes.find((n) => n.type === 'start')?.id;
	while (at && !main.has(at)) {
		main.add(at);
		at = [...(outgoing.get(at) ?? [])].sort(
			(a, b) =>
				stepsAhead(b.target) - stepsAhead(a.target) ||
				failedEnd(nodes.get(a.target)!) - failedEnd(nodes.get(b.target)!) ||
				hoped(a.condition) - hoped(b.condition) ||
				a.target.localeCompare(b.target)
		)[0]?.target;
	}

	const columns = new Map<number, WorkflowNode[]>();
	for (const node of graph.nodes) {
		const r = rank.get(node.id) ?? 0;
		columns.set(r, [...(columns.get(r) ?? []), node]);
	}
	const lanes = new Map<string, number>();
	// The lane a step comes from, and how hoped-for the transition into it is.
	const origin = (node: WorkflowNode) => {
		const from = (incoming.get(node.id) ?? []).filter((e) => lanes.has(e.source));
		return {
			lane: Math.min(Infinity, ...from.map((e) => lanes.get(e.source) ?? 0)),
			hoped: Math.min(Infinity, ...from.map((e) => hoped(e.condition)))
		};
	};
	const positions = new Map<string, { x: number; y: number }>();
	let x = 0;
	for (const r of [...columns.keys()].sort((a, b) => a - b)) {
		const column = [...(columns.get(r) ?? [])].sort((a, b) => {
			const [from, other] = [origin(a), origin(b)];
			return (
				Number(main.has(b.id)) - Number(main.has(a.id)) ||
				failedEnd(a) - failedEnd(b) ||
				from.lane - other.lane ||
				from.hoped - other.hoped ||
				a.id.localeCompare(b.id)
			);
		});
		const width = Math.max(...column.map((n) => nodeWidth(n.type)));
		// The top lane is the main path's, also where a rank has no step on it.
		const first = column.some((n) => main.has(n.id)) ? 0 : 1;
		column.forEach((node, i) => {
			lanes.set(node.id, first + i);
			positions.set(node.id, { x: x + (width - nodeWidth(node.type)) / 2, y: (first + i) * LANE });
		});
		x += width + GAP;
	}
	return { ...graph, nodes: graph.nodes.map((n) => ({ ...n, position: positions.get(n.id) ?? n.position })) };
}

const centre = (node: WorkflowNode) => ({
	x: node.position.x + nodeWidth(node.type) / 2,
	y: node.position.y + NODE_HEIGHT / 2
});

/**
 * The graph as the chart opens it: as stored when its steps already run left
 * to right, otherwise arranged (workflows saved top to bottom).
 */
export function leftToRight(graph: WorkflowGraph): WorkflowGraph {
	const { back } = layering(graph);
	const nodes = new Map(graph.nodes.map((n) => [n.id, n]));
	let across = 0,
		down = 0;
	for (const edge of graph.edges) {
		const from = nodes.get(edge.source),
			to = nodes.get(edge.target);
		if (!from || !to || back.has(edge.id)) continue;
		across += centre(to).x - centre(from).x;
		down += Math.abs(centre(to).y - centre(from).y);
	}
	return across > down ? graph : tidy(graph);
}

/** Whether both graphs place every step at the same spot. */
export function samePositions(a: WorkflowGraph, b: WorkflowGraph): boolean {
	const at = new Map(b.nodes.map((n) => [n.id, n.position]));
	return (
		a.nodes.length === b.nodes.length &&
		a.nodes.every((n) => at.get(n.id)?.x === n.position.x && at.get(n.id)?.y === n.position.y)
	);
}

export interface Point {
	x: number;
	y: number;
}

/**
 * A transition that cannot run straight from its step's right to the next
 * one's left. It runs in the free space between the cards instead: a loop
 * back to an earlier step over the top of the chart, a drop under the row of
 * its step (to a lane below, or past steps it skips).
 */
export interface Route {
	kind: 'loop' | 'drop';
	exit: 'top' | 'right' | 'bottom';
	enter: 'top' | 'left';
	/** Where along the card's top or bottom edge it leaves and enters (0 to 1). */
	from: number;
	to: number;
	/** Where it turns, in turn: a y after leaving upwards or downwards, then an x, and so on. */
	turns: number[];
}

type Box = { x: number; y: number; width: number; height: number };
const box = (node: WorkflowNode): Box => ({
	x: node.position.x,
	y: node.position.y,
	width: nodeWidth(node.type),
	height: NODE_HEIGHT
});
const TRACKS = 3;

/** Whether the straight line between two points (level or upright) touches the box. */
export function crosses(a: Point, b: Point, card: Box, margin = 4): boolean {
	return (
		Math.min(a.x, b.x) < card.x + card.width + margin &&
		Math.max(a.x, b.x) > card.x - margin &&
		Math.min(a.y, b.y) < card.y + card.height + margin &&
		Math.max(a.y, b.y) > card.y - margin
	);
}

/** The transitions that need a route of their own, by ID; the others run straight to the next step. */
export function routes(graph: WorkflowGraph): Map<string, Route> {
	const { rank, back } = layering(graph);
	const nodes = new Map(graph.nodes.map((n) => [n.id, n]));
	const found = new Map<string, Route>();
	const ends = (edge: WorkflowEdge) => ({ from: nodes.get(edge.source), to: nodes.get(edge.target) });
	// Another card above this one: a line cannot rise from, or come down to, its top.
	const covered = (node: WorkflowNode) =>
		graph.nodes.some(
			(other) =>
				other.id !== node.id &&
				other.position.y < node.position.y &&
				other.position.x < node.position.x + nodeWidth(node.type) &&
				node.position.x < other.position.x + nodeWidth(other.type)
		);

	// Loops arc over the whole chart, each at its own height, the longest outermost.
	const top = Math.min(0, ...graph.nodes.map((n) => n.position.y));
	const span = (e: WorkflowEdge) => (rank.get(e.source) ?? 0) - (rank.get(e.target) ?? 0);
	graph.edges
		.filter((e) => back.has(e.id) && e.source !== e.target && nodes.has(e.source) && nodes.has(e.target))
		.sort((a, b) => span(a) - span(b) || a.id.localeCompare(b.id))
		.forEach((edge, i) => {
			const { from, to } = ends(edge);
			if (!from || !to) return;
			const y = top - LOOP_BASE - i * LOOP_STEP;
			const exit = covered(from) ? 'right' : 'top';
			const enter = covered(to) ? 'left' : 'top';
			found.set(edge.id, {
				kind: 'loop',
				exit,
				enter,
				from: 0.65,
				to: 0.35,
				turns: [
					...(exit === 'right' ? [from.position.x + nodeWidth(from.type) + CLEAR] : []),
					y,
					...(enter === 'left' ? [to.position.x - CLEAR] : [])
				]
			});
		});

	// A transition to a lane below, or one whose straight line would run
	// through a card, drops under its step's row and runs there to the gap
	// before its target. Transitions to the same target share a track.
	const drops = graph.edges.filter((edge) => {
		const { from, to } = ends(edge);
		if (!from || !to || back.has(edge.id) || edge.source === edge.target) return false;
		const right = from.position.x + nodeWidth(from.type);
		if (to.position.y >= from.position.y + NODE_HEIGHT && to.position.x > centre(from).x + 20) return true;
		if (to.position.x <= right) return false;
		const [a, b] = [
			{ x: right, y: centre(from).y },
			{ x: to.position.x, y: centre(to).y }
		];
		const mid = (a.x + b.x) / 2;
		const line = [a, { x: mid, y: a.y }, { x: mid, y: b.y }, b];
		return graph.nodes.some(
			(other) =>
				other.id !== from.id &&
				other.id !== to.id &&
				line.some((point, i) => i > 0 && crosses(line[i - 1], point, box(other)))
		);
	});
	const targets = [...new Set(drops.map((e) => e.target))].sort((a, b) => {
		const [one, other] = [nodes.get(a)!, nodes.get(b)!];
		return one.position.x - other.position.x || one.position.y - other.position.y || a.localeCompare(b);
	});
	const leaving = new Map<string, WorkflowEdge[]>();
	for (const edge of drops) leaving.set(edge.source, [...(leaving.get(edge.source) ?? []), edge]);
	for (const [source, list] of leaving) {
		const from = nodes.get(source)!;
		// Transitions to the same target leave together; the others side by side.
		const exits = [...new Set(list.map((e) => e.target))].sort((a, b) => targets.indexOf(a) - targets.indexOf(b));
		for (const edge of list) {
			const to = nodes.get(edge.target)!;
			const track = targets.indexOf(edge.target) % TRACKS;
			found.set(edge.id, {
				kind: 'drop',
				exit: 'bottom',
				enter: 'left',
				from: (exits.indexOf(edge.target) + 1) / (exits.length + 1),
				to: 0.5,
				turns: [from.position.y + NODE_HEIGHT + 34 + track * 12, to.position.x - CLEAR - track * 8]
			});
		}
	}
	return found;
}
/** How far from a card a line beside it runs. */
const CLEAR = 14;

/**
 * The corners of a routed transition between the points where it leaves and
 * enters its steps (the canvas takes those from the cards as drawn).
 */
export function polyline(start: Point, end: Point, route: Route): Point[] {
	const points = [start];
	let upright = route.exit !== 'right';
	for (const turn of route.turns) {
		const last = points[points.length - 1];
		points.push(upright ? { x: last.x, y: turn } : { x: turn, y: last.y });
		upright = !upright;
	}
	const last = points[points.length - 1];
	points.push(upright ? { x: last.x, y: end.y } : { x: end.x, y: last.y }, end);
	return points;
}

/** Where a routed transition leaves and enters its steps, from their stored positions. */
export function anchors(from: WorkflowNode, to: WorkflowNode, route: Route): { start: Point; end: Point } {
	const start =
		route.exit === 'right'
			? { x: from.position.x + nodeWidth(from.type), y: centre(from).y }
			: {
					x: from.position.x + route.from * nodeWidth(from.type),
					y: from.position.y + (route.exit === 'bottom' ? NODE_HEIGHT : 0)
				};
	const end =
		route.enter === 'left'
			? { x: to.position.x, y: centre(to).y }
			: { x: to.position.x + route.to * nodeWidth(to.type), y: to.position.y };
	return { start, end };
}

/** An SVG path through the points with rounded corners. */
export function roundedPath(points: Point[], radius = 8): string {
	let path = `M ${points[0].x} ${points[0].y}`;
	for (let i = 1; i < points.length; i++) {
		const [before, at, after] = [points[i - 1], points[i], points[i + 1]];
		if (!after) {
			path += ` L ${at.x} ${at.y}`;
			break;
		}
		const length = (a: Point, b: Point) => Math.abs(a.x - b.x) + Math.abs(a.y - b.y);
		const r = Math.min(radius, length(before, at) / 2, length(at, after) / 2);
		const towards = (to: Point) => ({
			x: at.x + Math.sign(to.x - at.x) * r,
			y: at.y + Math.sign(to.y - at.y) * r
		});
		const [into, out] = [towards(before), towards(after)];
		path += ` L ${into.x} ${into.y} Q ${at.x} ${at.y} ${out.x} ${out.y}`;
	}
	return path;
}

/** The box around the graph's steps and the transitions routed above and below them. */
export function extent(graph: WorkflowGraph): { x: number; y: number; width: number; height: number } {
	if (graph.nodes.length === 0) return { x: 0, y: 0, width: 0, height: 0 };
	const routed = [...routes(graph).values()];
	const tracks = (kind: Route['kind']) =>
		routed.filter((r) => r.kind === kind).map((r) => r.turns[r.exit === 'right' ? 1 : 0]);
	const left = Math.min(...graph.nodes.map((n) => n.position.x));
	const top = Math.min(...graph.nodes.map((n) => n.position.y), ...tracks('loop').map((y) => y - 12));
	const right = Math.max(...graph.nodes.map((n) => n.position.x + nodeWidth(n.type)));
	const bottom = Math.max(...graph.nodes.map((n) => n.position.y + NODE_HEIGHT), ...tracks('drop').map((y) => y + 8));
	return { x: left, y: top, width: right - left, height: bottom - top };
}

/**
 * Where a sideways-scrolling chart drawn at the zoom (with the padding
 * around it) scrolls to show the step mid-view; nothing while the view has
 * no width yet (it is hidden).
 */
export function scrollTo(
	graph: WorkflowGraph,
	id: string,
	view: { width: number; zoom: number; padding: number }
): number | undefined {
	const node = graph.nodes.find((n) => n.id === id);
	if (!node || view.width <= 0) return undefined;
	return Math.max(0, view.padding + (centre(node).x - extent(graph).x) * view.zoom - view.width / 2);
}

/**
 * Where the canvas opens: the whole graph when it stays readable at that
 * size, otherwise zoomed to a readable size on its first steps.
 */
export function opening(
	graph: WorkflowGraph,
	size: { width: number; height: number },
	padding = 32
): { x: number; y: number; zoom: number } {
	const box = extent(graph);
	const fit = Math.min(
		1,
		(size.width - 2 * padding) / Math.max(box.width, 1),
		(size.height - 2 * padding) / Math.max(box.height, 1)
	);
	const zoom = Math.max(fit, READABLE);
	const wide = box.width * zoom > size.width - 2 * padding;
	return {
		x: wide ? padding - box.x * zoom : (size.width - box.width * zoom) / 2 - box.x * zoom,
		y: (size.height - box.height * zoom) / 2 - box.y * zoom,
		zoom
	};
}

/**
 * Adds a step of the given type right of the source and connects them on the
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
	// Right of the source, in the first lane that is free there.
	const x = from.position.x + nodeWidth(from.type) + GAP;
	let y = from.position.y;
	const taken = (n: WorkflowNode) =>
		Math.abs(n.position.y - y) < LANE / 2 &&
		n.position.x < x + nodeWidth(type) + GAP / 2 &&
		n.position.x + nodeWidth(n.type) > x - GAP / 2;
	while (graph.nodes.some(taken)) y += LANE;
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
 * The profile a blank workflow's first agent step runs: one that changes
 * code, the built-in implementer first while it still does.
 */
export function starterProfile(profiles: AgentProfile[]): AgentProfile | undefined {
	const builders = profiles.filter((p) => !p.archived_at && p.behaviour === 'changes_code');
	return builders.find((p) => p.builtin_key === 'implementer') ?? builders[0];
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
