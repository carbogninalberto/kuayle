import { api } from './client';
import type { AgentProvider, AgentRun, DevMachineLogChunk, ModelServer, RunUsage, TerminalSessionLaunchResponse } from '$lib/types/dev-machine';
import type { ConversationAttachment } from './conversations';
import type { WorkflowBudget, WorkflowNodeType } from './workflows';
export type TaskState =
	| 'queued'
	| 'preparing'
	| 'waiting_for_disk'
	| 'running'
	| 'waiting_input'
	| 'validating'
	| 'publishing'
	| 'awaiting_review'
	| 'blocked'
	| 'completed'
	| 'failed'
	| 'cancelling'
	| 'cancelled';
export interface AgentTask {
	id: string;
	workspace_id: string;
	conversation_id: string | null;
	creator_id: string;
	issue_id: string;
	team_id: string;
	request_id: string;
	prompt: string;
	provider: string;
	model: string;
	thinking: string;
	state: TaskState;
	reason: string;
	generation: number;
	failure_count: number;
	machine_id?: string | null;
	checkout_id: string;
	run_id: string;
	// The attempt stopped because the agent needs the creator's own sign-in.
	sign_in_required?: boolean;
	// Paused (blocked) because the agent hit its provider's usage limit: the
	// provider's message, when the limit resets, and when the task resumes on
	// its own (absent: automatic resume is off).
	usage_limit_message?: string | null;
	usage_limit_resets_at?: string | null;
	usage_limit_resume_at?: string | null;
	usage_limit_resumes?: number;
	// The same pause because the provider account has no credit left: the
	// message is the provider's, nothing resumes on its own, and resuming
	// after a top-up continues the same step.
	provider_credit_exhausted?: boolean;
	// A review task reviews another task's published pull request; it is shown
	// on that task's card, never as a task of its own.
	kind?: 'implement' | 'review';
	review_of_task_id?: string;
	// A task following a configurable workflow pins its version and reports
	// its current node and status; Standard tasks have none.
	workflow_version_id?: string | null;
	workflow_node?: string | null;
	workflow_status?: string | null;
	workflow_iterations?: number;
	// Each extension a person granted adds the workflow's budget once more.
	workflow_budget_extensions?: number;
	// Set while its creator's pause holds the workflow; a request is a pause
	// that takes effect when the current step has an outcome.
	workflow_paused_at?: string | null;
	// The agent is being stopped to deliver a message and continues by
	// itself: the task reads "cancelling" but is not being cancelled.
	interrupting?: boolean;
	workflow_pause_request?: WorkflowPauseMode | null;
	// Set on a task that runs a pipeline workflow (KUA-252): it is written
	// to in its room, and its tick moves it through the pipeline's phases.
	pipeline?: boolean;
	// The task's own app process (its preview command), when one was started.
	preview?: TaskPreview | null;
	created_at: string;
	updated_at: string;
}
export interface TaskPreview {
	state: 'starting' | 'running' | 'stopped' | 'failed';
	port_id: string | null;
	port: number | null;
	error: string | null;
}
export interface ValidationStep {
	id: string;
	agent_run_id: string;
	step_type?: string;
	status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled' | 'skipped';
	command_argv: string[];
	exit_code?: number | null;
	summary?: string | null;
	compose_file?: string;
	started_at?: string | null;
	completed_at?: string | null;
}
export interface PublicationView {
	step: ValidationStep | null;
	branch?: string | null;
	pull_request_url?: string | null;
}
export interface ValidationArtifact {
	id: string;
	step_id: string;
	path: string;
	content_type: string;
	size: number;
	created_at: string;
}
export interface AgentQueueItem {
	id: string;
	conversation_id: string;
	state: AgentTask['state'];
	reason: string;
	issue_identifier: string;
	issue_title: string;
	// The coding agent the task runs, e.g. "codex" or "claude-code".
	provider: string;
	updated_at: string;
	machine_id?: string | null;
	machine_name?: string | null;
	checkout_id?: string | null;
}
export interface TaskSignal {
	id: string;
	task_id: string;
	run_id: string;
	kind: 'ci' | 'review';
	external_id: string;
	outcome: string;
	name: string;
	summary: string;
	url: string;
	action:
		| 'recorded'
		| 'follow_up'
		| 'budget_exhausted'
		| 'not_reviewable'
		| 'untrusted'
		| 'follow_up_failed'
		| 'infrastructure';
	follow_up_generation?: number;
	created_at: string;
	updated_at: string;
}
export type SessionState =
	| 'created'
	| 'starting'
	| 'active'
	| 'waiting_input'
	| 'suspended'
	| 'completed'
	| 'failed'
	| 'cancelled';
// A requested agent review of a task's published pull request. Findings are
// the reviewer's untrusted final output.
export interface AgentReview {
	task_id: string;
	reviewed_task_id: string;
	requested_by: string;
	provider: string;
	commit_sha: string;
	repository: string;
	pull_request_number: number;
	pull_request_url: string;
	base_branch: string;
	focus: string;
	attempt: number;
	post_to_pull_request: boolean;
	verdict?: 'approve' | 'request_changes' | 'comment' | 'unknown';
	findings?: string;
	comment_state: 'not_requested' | 'pending' | 'posted' | 'failed';
	completed_at?: string;
	created_at: string;
	updated_at: string;
}
// The harness session a task's attempts ran in, with the review it ran.
export interface AgentSession {
	id: string;
	task_id: string;
	machine_id?: string;
	provider: string;
	purpose: 'implement' | 'review';
	state: SessionState;
	native_session_id?: string;
	started_at?: string;
	ended_at?: string;
	created_at: string;
	updated_at: string;
	task_state: TaskState;
	review?: AgentReview;
}
export interface TaskActivity {
	task_id: string;
	run_id: string;
	log_id: number;
	stream: string;
	truncated: boolean;
	events: Array<{ type: string; payload: unknown }>;
}
export interface TaskTransition {
	sequence: number;
	task_id: string;
	from_state: TaskState | '';
	to_state: TaskState;
	actor_id?: string;
	reason: string;
	created_at: string;
}
export interface TaskInput {
	request_id: string;
	prompt: string;
	provider: string;
	model: string;
	thinking: string;
	model_server_id?: string;
	/** A workflow ID, "standard", or omitted for the issue's default workflow. */
	workflow_id?: string;
}
export interface TaskOptions {
	providers: AgentProvider[];
	model_servers: ModelServer[];
	default_provider?: string;
}
// One message of a task's live channel. Messages to the agent come from its
// creator ("user") or the orchestrator for them; the agent's own text
// (from_agent) is untrusted and only ever shown as plain text.
export interface AgentLiveMessage {
	attachments?: ConversationAttachment[];
	id: string;
	task_id: string;
	direction: 'to_agent' | 'from_agent';
	seq: number | null;
	sender: 'user' | 'orchestrator' | 'agent';
	author_id: string | null;
	kind: 'message' | 'question';
	body: string;
	state: 'queued' | 'delivered' | 'received' | 'rejected';
	delivered_via: 'inbox' | 'prompt' | null;
	run_id: string | null;
	reject_reason: string | null;
	created_at: string;
	delivered_at: string | null;
	/** An agent's message: the sequence number of the message it answers. */
	reply_to?: number | null;
	/** How many attempts received a message to the agent; 0 owes no reply. */
	deliveries?: number;
}
// The task notebook (KUA-134): one working document per task that every step
// receives and updates. Agents write the plan's steps, decisions, what is
// done, what remains, how to verify and their findings; Kuayle records
// people's instructions, review and CI findings and events.
export interface NotebookPlanStep {
	title: string;
	files?: string[];
	commands?: string[];
	acceptance?: string;
	done?: boolean;
}
export interface NotebookFinding {
	id: number;
	source: 'review' | 'ci' | 'check' | 'comment' | 'agent';
	text: string;
	resolved?: boolean;
	at: string;
}
export interface NotebookInstruction {
	source: 'request' | 'retry' | 'follow_up' | 'live_message';
	author_id?: string;
	/** Sent by the assistant for the person. */
	assistant?: boolean;
	text: string;
	at: string;
}
export interface Notebook {
	plan: { text: string; steps: NotebookPlanStep[] | null };
	decisions: { decision: string; why?: string }[] | null;
	done: string[] | null;
	remaining: string[] | null;
	verify: string[] | null;
	findings: NotebookFinding[] | null;
	instructions: NotebookInstruction[] | null;
	events: { text: string; at: string }[] | null;
}
// Who changed which sections of the notebook, at which step and attempt.
export interface NotebookChange {
	revision: number;
	author: 'agent' | 'kuayle' | 'person';
	user_id?: string;
	run_id?: string;
	generation?: number;
	step_sequence?: number;
	step_label: string;
	sections: string[];
	summary: string;
	created_at: string;
}
/** One file a commit of the task tracks; changed: the task changed it. */
export interface TaskFileEntry {
	path: string;
	changed: boolean;
}
/** The files one commit of the task tracks, sorted by path; truncated: the listing stops at a bound. */
export interface TaskFiles {
	commit: string;
	files: TaskFileEntry[];
	truncated: boolean;
}
/**
 * Lines start to end (1-based) of one listed file at commit. truncated: the
 * file goes on after end, or a line was cut. A binary or too large file
 * carries no content.
 */
export interface TaskFileContent {
	path: string;
	commit: string;
	content: string;
	start: number;
	end: number;
	total_lines: number;
	truncated: boolean;
	binary: boolean;
	too_large: boolean;
}
export interface TaskNotebook {
	task_id: string;
	revision: number;
	updated_at?: string;
	notebook: Notebook;
	/** Newest first, at most 50. */
	history: NotebookChange[];
}
// One part of the prompt an attempt received: where it came from and how it
// reached the agent. The "prompt" parts, in order, are the prompt as sent.
export interface PromptSource {
	source: string;
	channel: 'prompt' | 'instructions_file';
	text: string;
	truncated?: boolean;
}
export interface RunPrompt {
	run_id: string;
	/** False for attempts that ran before prompts were recorded. */
	recorded: boolean;
	worktree_read_only: boolean;
	sources: PromptSource[] | null;
}
// How a message is sent: queued for the working agent (or the next
// iteration), by interrupting the working agent so it reads it now, or by
// starting an iteration for it when none runs.
export type LiveSendMode = 'interrupt' | 'start';
// The merged pull (or merge) request of an issue that a retry, a follow-up
// or "send and start now" asks about first (409 MERGED_WORK).
export interface MergedPullRequest {
	number: number;
	url: string;
	repository: string;
}
// A live message as the conversation that owns the task shows it.
export interface AgentLiveChatMessage extends AgentLiveMessage {
	identifier: string;
}
// The room of a task on a pipeline workflow (KUA-252): one log per task,
// ordered by seq, read by the task's creator alone. The creator ("human"),
// the pipeline's roles ("member", by role) and Kuayle post to it. A role's
// post the room refused is kept with its reason and delivered to nobody.
// Text from roles is untrusted and only ever shown as plain text.
export type TaskRoomAuthor = 'human' | 'member' | 'kuayle';
export type TaskRoomKind = 'say' | 'ask' | 'phase' | 'verdict' | 'request' | 'waiver' | 'grant' | 'fact' | 'summary';
// The structured part of a post; each kind reads its own fields.
export interface TaskRoomData {
	/** phase: the phase moved to. */
	phase?: string;
	/** verdict: what the role decided, on a commit, for its own role. */
	verdict?: 'approve' | 'request_changes' | 'comment';
	commit?: string;
	role?: string;
	/** waiver, grant: a check of the pipeline. */
	check?: string;
	/** request: review, merge, publish or split. */
	action?: string;
	/** fact: its name and value. */
	fact?: string;
	value?: unknown;
}
export interface TaskMessage {
	id: string;
	task_id: string;
	seq: number;
	author_kind: TaskRoomAuthor;
	author_role: string | null;
	author_user_id: string | null;
	author_member_id: string | null;
	/** A role's post: the run (its turn) that wrote it. */
	run_id: string | null;
	/** Empty: everyone. */
	to_roles: string[] | null;
	kind: TaskRoomKind;
	body: string;
	data: TaskRoomData;
	reply_to: number | null;
	reject_reason: string | null;
	created_at: string;
}
// What the creator may post; request_id makes a repeated post return the first.
export interface TaskRoomPost {
	kind: 'say' | 'ask' | 'request' | 'waiver' | 'grant';
	body?: string;
	to?: string[];
	data?: TaskRoomData;
	reply_to?: number;
	request_id?: string;
}
// Pausing a workflow: at once, which stops the agent that is working, or
// once the current step has finished.
// Moving a task to the newest version of its workflow: where it continues,
// or why it cannot be moved.
export type WorkflowSwitchRefusal =
	| 'no_workflow'
	| 'no_newer_version'
	| 'finished'
	| 'not_at_rest'
	| 'step_removed'
	| 'step_changed'
	| 'workflow_archived'
	| 'workflow_unavailable'
	| 'workflow_unusable'
	| 'step_missing'
	| 'step_not_runnable'
	| 'target_version_changed'
	| 'workflow_kind'
	| 'pipeline_pinned'
	| 'not_creator'
	| 'session_required';
export interface WorkflowSwitchTarget {
	workflow_id: string;
	node: string;
	expected_version_id?: string;
}
export interface WorkflowSwitch {
	from_version: number;
	to_version: number;
	node?: string;
	node_label?: string;
	refused?: WorkflowSwitchRefusal;
	continuation?: {
		from_workflow_id: string;
		from_workflow_name: string;
		from_node: string;
		to_workflow_id: string;
		to_workflow_name: string;
		to_version_id: string;
		node_type: WorkflowNodeType;
		carries: string[];
		budget: WorkflowBudget;
		max_visits: number;
		timeout_minutes: number;
		resets: string[];
		time_spent_seconds: number;
	};
}

export type WorkflowPauseMode = 'now' | 'after_step';
const base = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}`;
const task = (slug: string, id: string) => `${base(slug)}/agent-tasks/${encodeURIComponent(id)}`;
const chat = (slug: string, id: string) => `${base(slug)}/conversations/${encodeURIComponent(id)}`;
export const agentTasks = {
	options: (slug: string, id: string) => api.get<TaskOptions>(`${chat(slug, id)}/task-options`),
	list: (slug: string, id: string, before?: string) =>
		api.get<{ tasks: AgentTask[]; next_before?: string }>(
			`${chat(slug, id)}/tasks${before ? `?before=${encodeURIComponent(before)}` : ''}`
		),
	create: (slug: string, id: string, input: TaskInput) => api.post<AgentTask>(`${chat(slug, id)}/tasks`, input),
	get: (slug: string, id: string) => api.get<AgentTask>(task(slug, id)),
	cancel: (slug: string, id: string) => api.post<void>(`${task(slug, id)}/cancel`, {}),
	// Completes a task awaiting review in Kuayle without merging its pull request;
	// generation is the attempt the user reviewed.
	accept: (slug: string, id: string, generation: number) =>
		api.post<AgentTask>(`${task(slug, id)}/accept`, { generation }),
	// Queue starting or stopping the task's app (its preview command); 202.
	startPreview: (slug: string, id: string) => api.post<void>(`${task(slug, id)}/preview`),
	stopPreview: (slug: string, id: string) => api.delete<void>(`${task(slug, id)}/preview`),
	// The task's live channel: both directions, oldest first; sending queues the
	// creator's message for the running agent or the next attempt.
	liveMessages: (slug: string, id: string) =>
		api.get<{ messages: AgentLiveMessage[] }>(`${task(slug, id)}/live-messages`),
	// Starting an iteration (mode "start") on an issue whose work was merged is
	// refused with 409 MERGED_WORK until it is sent again with confirmMerged.
	sendLiveMessage: (slug: string, id: string, body: string, mode?: LiveSendMode, confirmMerged = false, attachments: string[] = []) =>
		api.post<AgentLiveMessage>(
			`${task(slug, id)}/live-messages`,
			{ body, ...(mode ? { mode } : {}), ...(confirmMerged ? { confirm_merged: true } : {}), ...(attachments.length ? { attachment_ids: attachments } : {}) }
		),
	// The room of a task on a pipeline: at most 200 posts after a seq, oldest
	// first; a task on no pipeline answers 409 ROOM_UNAVAILABLE.
	room: (slug: string, id: string, after = 0) =>
		api.get<{ messages: TaskMessage[] }>(`${task(slug, id)}/room?after=${after}`),
	postRoom: (slug: string, id: string, post: TaskRoomPost) => api.post<TaskMessage>(`${task(slug, id)}/room`, post),
	// Opens a terminal on the task's machine running Claude Code on the task's
	// own session, in its worktree; only for its creator, while it is stopped.
	openInClaudeCode: (slug: string, id: string) =>
		api.post<TerminalSessionLaunchResponse>(`${task(slug, id)}/terminal`),
	// Keeps the sign-in finished in the machine's terminal and starts a new attempt.
	continueAfterSignIn: (slug: string, id: string, requestId: string) =>
		api.post<AgentTask>(`${task(slug, id)}/sign-in`, { request_id: requestId }),
	// Pauses the task's workflow; resume continues it from the same step (or
	// withdraws a pause that has not taken effect yet). Creator only.
	pause: (slug: string, id: string, mode: WorkflowPauseMode) =>
		api.post<AgentTask>(`${task(slug, id)}/pause`, { mode }),
	resume: (slug: string, id: string) => api.post<AgentTask>(`${task(slug, id)}/resume`, {}),
	// What moving the task to the newest version of its workflow would do
	// (nothing changes), and the move itself. Creator only; a task that
	// cannot be moved answers 409 with the reason.
	workflowSwitch: (slug: string, id: string, target?: Pick<WorkflowSwitchTarget, 'workflow_id' | 'node'>, signal?: AbortSignal) =>
		api.get<WorkflowSwitch>(`${task(slug, id)}/workflow/switch${target ? `?${new URLSearchParams(target)}` : ''}`, { signal }),
	switchWorkflow: (slug: string, id: string, target?: WorkflowSwitchTarget) =>
		api.post<{ switch: WorkflowSwitch; task: AgentTask }>(`${task(slug, id)}/workflow/switch`, target ?? {}),
	resumeUsageLimit: (slug: string, id: string) => api.post<AgentTask>(`${task(slug, id)}/resume`, {}),
	cancelUsageLimitResume: (slug: string, id: string) => api.delete<AgentTask>(`${task(slug, id)}/resume`),
	// A retry on an issue whose work was merged answers 409 MERGED_WORK naming
	// the pull request; the same request with confirm_merged retries anyway.
	retry: (
		slug: string,
		id: string,
		input: { request_id: string; generation: number; prompt: string; confirm_merged?: boolean }
	) => api.post<AgentTask>(`${task(slug, id)}/retry`, input),
	history: (slug: string, id: string, after = 0) =>
		api.get<{ transitions: TaskTransition[]; next_after?: number }>(`${task(slug, id)}/history?after=${after}`),
	runs: (slug: string, id: string, before?: string) =>
		api.get<{ runs: AgentRun[]; next_before?: string; usage?: RunUsage }>(
			`${task(slug, id)}/runs${before ? `?before=${encodeURIComponent(before)}` : ''}`
		),
	artifacts: (slug: string, id: string, run: string) =>
		api.get<ValidationArtifact[]>(`${task(slug, id)}/runs/${encodeURIComponent(run)}/artifacts`),
	artifact: (slug: string, id: string, run: string, artifact: string) =>
		api.blob(`${task(slug, id)}/runs/${encodeURIComponent(run)}/artifacts/${encodeURIComponent(artifact)}`),
	// The task's notebook and its newest changes; creator only.
	notebook: (slug: string, id: string) => api.get<TaskNotebook>(`${task(slug, id)}/notebook`),
	// The prompt an attempt received, split by source; creator only.
	runPrompt: (slug: string, id: string, run: string) =>
		api.get<RunPrompt>(`${task(slug, id)}/runs/${encodeURIComponent(run)}/prompt`),
	// The task's full debug trace as one JSON file; creator or workspace
	// owner/admin who can see the issue.
	debugTrace: (slug: string, id: string) => api.blob(`${task(slug, id)}/debug`),
	diff: (slug: string, id: string, run: string) =>
		api.get<{ diff: string; truncated: boolean }>(`${task(slug, id)}/runs/${encodeURIComponent(run)}/diff`),
	// The files a commit of the task tracks (the head by default) and one page
	// of a file's lines; creator only. Untracked and ignored files are never served.
	files: (slug: string, id: string, ref?: string) =>
		api.get<TaskFiles>(`${task(slug, id)}/files${ref ? `?${new URLSearchParams({ ref })}` : ''}`),
	fileContent: (slug: string, id: string, path: string, page: { ref?: string; start?: number; end?: number } = {}) => {
		const query = new URLSearchParams({ path });
		if (page.ref) query.set('ref', page.ref);
		if (page.start) query.set('start', String(page.start));
		if (page.end) query.set('end', String(page.end));
		return api.get<TaskFileContent>(`${task(slug, id)}/files/content?${query}`);
	},
	// Checks run on demand on an attempt, oldest first.
	manualChecks: (slug: string, id: string, run: string) =>
		api.get<ValidationStep[]>(`${task(slug, id)}/runs/${encodeURIComponent(run)}/checks`),
	queue: (slug: string) => api.get<AgentQueueItem[]>(`${base(slug)}/agent-tasks`),
	signals: (slug: string, id: string) => api.get<TaskSignal[]>(`${task(slug, id)}/signals`),
	sessions: (slug: string, id: string) => api.get<{ sessions: AgentSession[] }>(`${task(slug, id)}/sessions`),
	publication: (slug: string, id: string, run: string) =>
		api.get<PublicationView>(`${task(slug, id)}/runs/${encodeURIComponent(run)}/publication`),
	logs: (slug: string, id: string, run: string, after = 0) =>
		api.get<{ logs: DevMachineLogChunk[]; next_after?: number }>(
			`${task(slug, id)}/runs/${encodeURIComponent(run)}/logs?after=${after}`
		)
};
