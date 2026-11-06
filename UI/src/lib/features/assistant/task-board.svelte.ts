import { agentTasks, type AgentTask, type TaskOptions } from '$lib/api/agent-tasks';
import type { ConversationState } from './conversation-state.svelte';

// The coding tasks of an issue conversation: loaded once for the page and
// shared by the agent panel, the inline task steps and the composer target.
// Tasks themselves live in chat.liveTasks so live events keep updating them.
export class TaskBoard {
	options = $state<TaskOptions | null>(null);
	loading = $state(true);
	failed = $state(false);
	denied = $state(false);
	optionsUnavailable = $state(false);
	before = $state<string | undefined>();
	loadingOlder = $state(false);
	// The Start coding / Retry dialog; retry names the task being retried.
	composer = $state<{ open: boolean; retry?: AgentTask }>({ open: false });
	private disposed = false;
	constructor(readonly chat: ConversationState) {}

	// Newest first; reviews are shown on the task they review (see acceptTask).
	get tasks() {
		return Object.values(this.chat.liveTasks).sort(
			(a, b) => Date.parse(b.created_at) - Date.parse(a.created_at) || b.id.localeCompare(a.id)
		);
	}

	start() {
		void this.load();
		void agentTasks
			.options(this.chat.slug, this.chat.id)
			.then((value) => {
				if (!this.disposed) this.options = value;
			})
			.catch(() => {
				if (!this.disposed) this.optionsUnavailable = true;
			});
	}
	fail(error: unknown) {
		if (this.disposed) return;
		const code = (error as { status?: number }).status;
		if ([401, 403, 404].includes(code ?? 0)) {
			this.denied = true;
			this.chat.liveTasks = {};
			this.chat.taskActivity = {};
			this.composer = { open: false };
		} else this.failed = true;
	}
	async load() {
		this.loading = true;
		this.failed = false;
		try {
			const result = await agentTasks.list(this.chat.slug, this.chat.id);
			if (this.disposed || this.chat.unavailable) return;
			for (const task of result.tasks) this.chat.acceptTask(task);
			this.before = result.next_before;
		} catch (e) {
			this.fail(e);
		} finally {
			if (!this.disposed) this.loading = false;
		}
	}
	async older() {
		if (!this.before || this.loadingOlder) return;
		this.loadingOlder = true;
		try {
			const page = await agentTasks.list(this.chat.slug, this.chat.id, this.before);
			if (this.disposed || this.denied || this.chat.unavailable) return;
			for (const task of page.tasks) this.chat.acceptTask(task);
			this.before = page.next_before;
		} catch (e) {
			this.fail(e);
		} finally {
			if (!this.disposed) this.loadingOlder = false;
		}
	}
	startTask() {
		this.composer = { open: true };
	}
	retry(task: AgentTask) {
		this.composer = { open: true, retry: task };
	}
	closeComposer() {
		this.composer = { open: false };
	}
	dispose() {
		this.disposed = true;
	}
}
