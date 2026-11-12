<script lang="ts">
	import Markdown from './Markdown.svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { onDestroy, untrack } from 'svelte';
	import TestResults from './TestResults.svelte';
	import AgentPlan from './AgentPlan.svelte';
	import AgentReasoning from './AgentReasoning.svelte';
	import FileTreeNode from './FileTreeNode.svelte';
	import { fileTree, latestPlan, reasoningSummaries } from './agent-trace';
	import {
		Circle,
		CircleCheck,
		CircleAlert,
		LoaderCircle,
		Terminal,
		ExternalLink,
		GitPullRequest,
		SquareTerminal,
		Bot,
		FileDiff,
		X,
		RotateCcw,
		AppWindow,
		Play,
		Square,
		Server,
		ChevronRight,
		ChevronDown,
		BadgeCheck,
		FlaskConical,
		CirclePause,
		Pause,
		CircleArrowUp
	} from '@lucide/svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import TaskActions from './TaskActions.svelte';
	import TaskMessages from '$lib/features/agent-messages/TaskMessages.svelte';
	import TaskPath from './TaskPath.svelte';
	import AgentTranscript from './AgentTranscript.svelte';
	import TaskMenu from './TaskMenu.svelte';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { signInCommand } from './agent-session';
	import ClaudeTokenForm from '$lib/features/agents/ClaudeTokenForm.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	const agentLabel = (provider: string) => agentInfo(provider).name;
	import { approvedBy } from './review-approval';
	import type { TaskAction, TaskMenuEntry } from './task-actions';
	import { canResume, workflowPause } from './workflow-pause';
	import { mayHaveWorkflowUpdate, workflowUpdate } from './workflow-switch';
	import { useTerminalDock } from '$lib/features/dev-machines/terminal-dock-context.svelte';
	import { agentSessionCommand, resumableStates } from './agent-session';
	import { taskTone, toneDot, toneText } from './task-tone';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import {
		agentTasks,
		type AgentTask,
		type WorkflowSwitch,
		type AgentSession,
		type PublicationView,
		type TaskSignal,
		type TaskTransition,
		type ValidationStep
	} from '$lib/api/agent-tasks';
	import type { AgentRun, AppPort, DevMachineLogChunk } from '$lib/types/dev-machine';
	import { listMachinePorts } from '$lib/api/dev-machines';
	import { portName, taskAppPorts } from '$lib/features/dev-machines/app-ports';
	import { openAppPort } from '$lib/features/dev-machines/open-app-port';
	import { getErrorMessage } from '$lib/features/toast/toast';
	import type { ConversationState } from './conversation-state.svelte';
	import { taskLabel } from './task-labels';
	import { iterationReasons, workflowIterationReasons, type IterationReason } from './shell-rules';
	import { m } from '$lib/features/i18n/messages';
	let {
		task,
		chat,
		onretry,
		ondenied,
		pinned = false
	}: {
		task: AgentTask;
		chat: ConversationState;
		onretry: (task: AgentTask) => void;
		ondenied: (error: unknown) => void;
		// The newest task starts expanded; earlier ones are one-line rows.
		pinned?: boolean;
	} = $props();
	// Earlier tasks start as one-line rows; the newest one is shown in full.
	let expanded = $state(untrack(() => pinned));
	// The parsed session is built only while the output is shown.
	let outputOpen = $state(false);
	let open = $state(false),
		busy = $state(false),
		loading = $state(false),
		error = $state('');
	let history = $state<TaskTransition[]>([]),
		runs = $state<AgentRun[]>([]),
		logs = $state<DevMachineLogChunk[]>([]);
	let historyAfter = $state<number | undefined>(),
		runsBefore = $state<string | undefined>(),
		logsAfter = $state<number | undefined>();
	let validation = $state<ValidationStep | null>(null);
	// Checks run on demand on the selected attempt, oldest first.
	let manualChecks = $state<ValidationStep[]>([]);
	let checkBusy = $state(false),
		checkError = $state('');
	let publication = $state<PublicationView | null>(null);
	let signals = $state<TaskSignal[]>([]);
	let appPorts = $state<AppPort[]>([]),
		appBusy = $state(false),
		appError = $state(''),
		// A start/stop request the task payload does not reflect yet.
		appRequested = $state<'start' | 'stop' | null>(null);
	let sessions = $state<AgentSession[]>([]);
	const signalAction: Record<TaskSignal['action'], (() => string) | undefined> = {
		recorded: undefined,
		follow_up: m['tasks.signal_follow_up'],
		budget_exhausted: m['tasks.signal_budget_exhausted'],
		not_reviewable: m['tasks.signal_not_reviewable'],
		untrusted: m['tasks.signal_untrusted'],
		follow_up_failed: m['tasks.signal_follow_up_failed'],
		infrastructure: m['tasks.signal_infrastructure']
	};
	let selectedRun = $state(untrack(() => task.run_id));
	let seenAttempt = untrack(() => task.run_id),
		disposed = false,
		dirty = false,
		initialRunsLoaded = false;
	let refreshTimer: ReturnType<typeof setTimeout> | undefined;
	// A workflow its creator paused is neither working nor failed, whatever
	// state the pause found the task in.
	const pause = $derived(workflowPause(task));
	const active = $derived(
		pause !== 'paused' && ['preparing', 'running', 'validating', 'publishing', 'cancelling'].includes(task.state)
	);
	const failed = $derived(pause !== 'paused' && ['failed', 'blocked'].includes(task.state));
	const canCancel = $derived(!['completed', 'cancelled'].includes(task.state));
	const canRetry = $derived(['failed', 'blocked', 'awaiting_review'].includes(task.state));
	// Work awaiting review can be accepted in Kuayle without merging.
	const canAccept = $derived(task.state === 'awaiting_review' && task.kind !== 'review');
	let acceptOpen = $state(false);
	// A newer version of the task's workflow, which its creator can move it
	// to. Asked again whenever the task changes state or version: a task is
	// moved while it is paused or waiting.
	let switchPlan = $state<WorkflowSwitch | null>(null),
		switchOpen = $state(false);
	const update = $derived(workflowUpdate(switchPlan));
	$effect(() => {
		void [task.state, task.workflow_version_id, task.workflow_status, task.workflow_paused_at];
		if (!mayHaveWorkflowUpdate(task)) {
			switchPlan = null;
			return;
		}
		let stale = false;
		agentTasks
			.workflowSwitch(chat.slug, task.id)
			.then((plan) => {
				if (!stale && !disposed) switchPlan = plan;
			})
			.catch(() => {
				// Not being able to ask is not worth an alert: the action is absent.
				if (!stale && !disposed) switchPlan = null;
			});
		return () => {
			stale = true;
		};
	});
	async function moveToNewestVersion() {
		if (busy) return;
		busy = true;
		error = '';
		try {
			const moved = await agentTasks.switchWorkflow(chat.slug, task.id);
			if (disposed) return;
			switchPlan = null;
			chat.acceptTask(moved.task);
		} catch (e) {
			failure(e);
		} finally {
			if (!disposed) {
				busy = false;
				switchOpen = false;
			}
		}
	}
	// The independent check owns its own runtime service; never attribute its
	// output to the native harness or the harness output to the check.
	const checkIds = $derived(new Set([validation?.id, ...manualChecks.map((check) => check.id)].filter(Boolean)));
	const isCheck = (line: DevMachineLogChunk) => !!line.service_id && checkIds.has(line.service_id);
	const latestCheck = $derived(manualChecks.at(-1) ?? null);
	const sessionLogs = $derived(logs.filter((line) => !isCheck(line)));
	const stepOutput = (id: string | undefined) =>
		logs
			.filter((line) => !!id && line.service_id === id)
			.map((line) => line.content)
			.join('');
	const checkOutput = $derived(stepOutput(validation?.id));
	const manualOutput = $derived(stepOutput(latestCheck?.id));
	const selected = $derived(runs.find((run) => run.id === selectedRun));
	// Derived only from the attempt's recorded native output and git state.
	const nativeLines = $derived(
		logs.filter((line) => !isCheck(line) && line.stream === 'stdout').map((line) => line.content)
	);
	const plan = $derived(latestPlan(nativeLines));
	const reasoning = $derived(reasoningSummaries(nativeLines));
	const changedFiles = $derived(fileTree(selected?.changed_files ?? []));
	// Repository links come from the recorded publication, never agent output.
	const fileLink = $derived.by(() => {
		const url = publication?.step?.status === 'succeeded' ? publication.pull_request_url : undefined;
		const match = url?.match(/^https:\/\/github\.com\/([A-Za-z0-9-]+\/[A-Za-z0-9._-]+)\/pull\/\d+$/);
		const branch = publication?.branch;
		if (!match || !branch || publication?.step?.agent_run_id !== selectedRun) return undefined;
		return (path: string) =>
			`https://github.com/${match[1]}/blob/${branch.split('/').map(encodeURIComponent).join('/')}/${path.split('/').map(encodeURIComponent).join('/')}`;
	});
	// Review links come only from the recorded publication, never from agent output.
	const pullRequest = $derived(
		publication?.step?.status === 'succeeded' ? (publication.pull_request_url ?? undefined) : undefined
	);
	const publicationLabel = $derived(
		publication?.step
			? {
					queued: m['tasks.publication_queued'](),
					running: m['tasks.publication_running'](),
					succeeded: m['tasks.publication_succeeded'](),
					failed: m['tasks.publication_failed'](),
					cancelled: m['tasks.publication_cancelled'](),
					skipped: m['tasks.publication_skipped']()
				}[publication.step.status]
			: ''
	);
	const activity = $derived(chat.taskActivity[task.id]);
	onDestroy(() => {
		disposed = true;
		clearTimeout(refreshTimer);
	});
	// Lost access hides the panel. A missing attempt only affects this task:
	// its records may be gone while the task and the panel remain.
	function failure(e: unknown) {
		if (disposed) return;
		const status = (e as { status?: number }).status ?? 0;
		if (status === 401 || status === 403) ondenied(e);
		else error = status === 404 ? m['tasks.attempt_missing']() : m['tasks.load_error']();
	}
	async function refresh() {
		if (loading) {
			dirty = true;
			return;
		}
		loading = true;
		error = '';
		const runID = selectedRun;
		try {
			const [attempts, events, recorded] = await Promise.all([
				agentTasks.runs(chat.slug, task.id),
				agentTasks.history(chat.slug, task.id, history.at(-1)?.sequence ?? 0),
				agentTasks.signals(chat.slug, task.id)
			]);
			if (disposed || chat.unavailable) return;
			signals = recorded;
			const existing = new Map(runs.map((run) => [run.id, run]));
			for (const run of attempts.runs) existing.set(run.id, run);
			runs = [...existing.values()];
			if (!initialRunsLoaded) {
				runsBefore = attempts.next_before;
				initialRunsLoaded = true;
			}
			const known = new Set(history.map((e) => e.sequence));
			history = [...history, ...events.transitions.filter((e) => !known.has(e.sequence))];
			historyAfter = events.next_after;
			if (attempts.runs.some((run) => run.id === runID) || runs.some((run) => run.id === runID)) {
				const [page, check, manual] = await Promise.all([
					agentTasks.logs(chat.slug, task.id, runID, logs.at(-1)?.id ?? 0),
					agentTasks.validation(chat.slug, task.id, runID),
					// Manual checks are optional context; the panel works without them.
					agentTasks.manualChecks(chat.slug, task.id, runID).catch(() => [] as ValidationStep[])
				]);
				if (disposed || chat.unavailable || selectedRun !== runID) return;
				validation = check;
				manualChecks = Array.isArray(manual) ? manual : [];
				const ids = new Set(logs.map((line) => line.id));
				logs = [...logs, ...page.logs.filter((line) => !ids.has(line.id))];
				logsAfter = page.next_after;
			}
		} catch (e) {
			failure(e);
		} finally {
			if (!disposed) {
				loading = false;
				if (dirty) {
					dirty = false;
					void refresh();
				}
			}
		}
	}
	$effect(() => {
		if (task.run_id !== seenAttempt) {
			seenAttempt = task.run_id;
			selectedRun = task.run_id;
			logs = [];
			validation = null;
			manualChecks = [];
			logsAfter = undefined;
		}
	});
	$effect(() => {
		const run = selectedRun;
		untrack(() => {
			logs = [];
			validation = null;
			manualChecks = [];
			logsAfter = undefined;
		});
		if (!run) return;
	});
	$effect(() => {
		const observed = [
			open,
			task.state,
			task.run_id,
			selectedRun,
			activity?.log_id,
			chat.taskValidationCursor[task.id],
			chat.taskSignalCursor[task.id]
		];
		// Throttle from the first event: a continuous stream must not keep
		// postponing output until the native process becomes quiet.
		if (!observed[0]) {
			clearTimeout(refreshTimer);
			refreshTimer = undefined;
			return;
		}
		if (refreshTimer === undefined) {
			refreshTimer = setTimeout(() => {
				refreshTimer = undefined;
				void refresh();
			}, 150);
		}
	});
	// Publication is loaded even while details are collapsed so the review link
	// stays visible; publication events and state changes refresh it.
	$effect(() => {
		const run = task.run_id;
		const observed = [task.state, chat.taskPublicationCursor[task.id]];
		if (!['publishing', 'awaiting_review', 'completed', 'blocked'].includes(observed[0] as string)) {
			untrack(() => (publication = null));
			return;
		}
		untrack(() => void loadPublication(run));
	});
	async function loadPublication(run: string) {
		try {
			const view = await agentTasks.publication(chat.slug, task.id, run);
			if (!disposed && !chat.unavailable && task.run_id === run) publication = view;
		} catch (e) {
			failure(e);
		}
	}
	// Sessions (the task's own and its requested reviews) are read-only and
	// listed in the details; review changes arrive as task.review_updated
	// events on this conversation.
	$effect(() => {
		const observed = [open, task.state, task.run_id, chat.taskReviewCursor[task.id]];
		if (!observed[0]) return;
		untrack(() => void loadSessions());
	});
	async function loadSessions() {
		try {
			const view = await agentTasks.sessions(chat.slug, task.id);
			if (!disposed && !chat.unavailable) sessions = view.sessions ?? [];
		} catch (e) {
			failure(e);
		}
	}
	const sessionStates: Record<AgentSession['state'], () => string> = {
		created: m['tasks.session_created'],
		starting: m['tasks.session_starting'],
		active: m['tasks.session_active'],
		waiting_input: m['tasks.session_waiting_input'],
		suspended: m['tasks.session_suspended'],
		completed: m['tasks.session_completed'],
		failed: m['tasks.session_failed'],
		cancelled: m['tasks.session_cancelled']
	};
	// The task's app: its preview command, started only on its creator's request,
	// plus the ports its agent exposed. They are the reviewer's QA entrypoints.
	const appTaskStates = ['awaiting_review', 'blocked', 'failed'];
	const appState = $derived.by(() => {
		const recorded = task.preview?.state;
		if (appRequested === 'start' && recorded !== 'running' && recorded !== 'failed') return 'starting';
		if (appRequested === 'stop') return 'stopped';
		return recorded ?? 'stopped';
	});
	const canStartApp = $derived(!!task.machine_id && appTaskStates.includes(task.state));
	const ownPorts = $derived(
		taskAppPorts(appPorts, task.id).filter((p) => !(appState === 'stopped' && p.source === 'task'))
	);
	$effect(() => {
		const recorded = task.preview?.state;
		if (appRequested === 'start' && (recorded === 'starting' || recorded === 'running' || recorded === 'failed'))
			appRequested = recorded === 'starting' ? 'start' : null;
		else if (appRequested === 'stop' && (!recorded || recorded === 'stopped')) appRequested = null;
	});
	async function loadAppPorts() {
		const machine = task.machine_id;
		if (!machine) return;
		try {
			const list = await listMachinePorts(chat.slug, machine);
			if (!disposed && task.machine_id === machine) appPorts = list.ports ?? [];
		} catch {
			// Ports are optional context; the menu still offers starting the app.
		}
	}
	$effect(() => {
		const observed = [task.machine_id, task.state, task.preview?.state, task.preview?.port_id];
		if (!observed[0] || (!appTaskStates.includes(task.state) && task.state !== 'completed' && !task.preview)) {
			untrack(() => (appPorts = []));
			return;
		}
		untrack(() => void loadAppPorts());
	});
	// While the app starts or stops, check the task until the manager reports it.
	$effect(() => {
		if (appState !== 'starting' && appRequested === null) return;
		let ticks = 0;
		const timer = setInterval(() => {
			if (++ticks > 40) {
				clearInterval(timer);
				appRequested = null;
				return;
			}
			void refreshApp();
		}, 3000);
		return () => clearInterval(timer);
	});
	async function refreshApp() {
		const previous = chat.liveTasks[task.id] ?? null;
		try {
			const updated = await agentTasks.get(chat.slug, task.id);
			if (!disposed) chat.acceptTask(updated, false, previous);
		} catch {
			// The next check retries.
		}
		await loadAppPorts();
	}
	async function toggleApp(start: boolean) {
		if (appBusy) return;
		appBusy = true;
		appError = '';
		try {
			if (start) await agentTasks.startPreview(chat.slug, task.id);
			else await agentTasks.stopPreview(chat.slug, task.id);
			if (disposed) return;
			appRequested = start ? 'start' : 'stop';
			await refreshApp();
		} catch (e) {
			if (!disposed) appError = getErrorMessage(e, start ? m['tasks.app_start_error']() : m['tasks.app_stop_error']());
		} finally {
			if (!disposed) appBusy = false;
		}
	}
	async function openPort(port: AppPort) {
		if (!task.machine_id) return;
		appError = '';
		try {
			await openAppPort(chat.slug, task.machine_id, port.id);
		} catch (e) {
			if (!disposed) appError = getErrorMessage(e, m['machines.port_not_ready']());
		}
	}
	const appAction = $derived.by((): TaskAction | undefined => {
		if (!task.machine_id || (!canStartApp && !ownPorts.length && appState === 'stopped')) return undefined;
		const menu: TaskMenuEntry[] = [];
		if (appState === 'failed' && task.preview?.error)
			menu.push({ kind: 'note', id: 'error', text: task.preview.error, tone: 'error' });
		for (const port of ownPorts)
			menu.push({
				kind: 'item',
				id: port.id,
				icon: ExternalLink,
				label: m['tasks.app_open']({ name: portName(port) }),
				hint: port.label ? `:${port.port}` : undefined,
				onclick: () => void openPort(port)
			});
		const running = appState === 'starting' || appState === 'running';
		if (canStartApp || running) {
			if (menu.length) menu.push({ kind: 'separator', id: 'separator' });
			menu.push(
				running
					? {
							kind: 'item',
							id: 'stop',
							icon: Square,
							label: m['tasks.app_stop'](),
							disabled: appBusy,
							variant: 'danger',
							onclick: () => void toggleApp(false)
						}
					: {
							kind: 'item',
							id: 'start',
							icon: Play,
							label: m['tasks.app_start'](),
							disabled: appBusy,
							onclick: () => void toggleApp(true)
						}
			);
		}
		const label =
			appState === 'starting'
				? m['tasks.app_starting']()
				: appState === 'running'
					? m['tasks.app_running']()
					: appState === 'failed'
						? m['tasks.app_failed']()
						: m['tasks.app']();
		return {
			id: 'app',
			icon: AppWindow,
			label,
			group: 'tools',
			menu,
			onopen: () => void loadAppPorts(),
			indicator: appState === 'stopped' ? undefined : appState
		};
	});
	// The task's check runs on the latest attempt's checkout without the agent;
	// the result streams into the details like the independent check.
	const canRunCheck = $derived(
		!!task.machine_id &&
			task.kind !== 'review' &&
			['awaiting_review', 'blocked', 'failed', 'completed'].includes(task.state)
	);
	const checkRunning = $derived(latestCheck?.status === 'queued' || latestCheck?.status === 'running');
	async function runCheck() {
		if (checkBusy) return;
		checkBusy = true;
		checkError = '';
		try {
			const step = await agentTasks.runCheck(chat.slug, task.id);
			if (disposed) return;
			selectedRun = step.agent_run_id;
			manualChecks = [...manualChecks.filter((check) => check.id !== step.id), step];
			open = true;
			void refresh();
		} catch (e) {
			if (!disposed) checkError = getErrorMessage(e, m['tasks.run_check_error']());
		} finally {
			if (!disposed) checkBusy = false;
		}
	}
	async function cancel() {
		if (busy) return;
		busy = true;
		error = '';
		const previous = chat.liveTasks[task.id] ?? null;
		try {
			await agentTasks.cancel(chat.slug, task.id);
			const updated = await agentTasks.get(chat.slug, task.id);
			if (!disposed) chat.acceptTask(updated, false, previous);
		} catch (e) {
			failure(e);
		} finally {
			if (!disposed) busy = false;
		}
	}
	// Pausing holds the task's workflow where it is; resuming continues it.
	async function pauseAction(request: () => Promise<AgentTask>) {
		if (busy) return;
		busy = true;
		error = '';
		try {
			const next = await request();
			if (!disposed) chat.acceptTask(next);
		} catch (e) {
			failure(e);
		} finally {
			if (!disposed) busy = false;
		}
	}
	// Accepting completes the attempt the user reviewed; the pull request stays
	// open and unmerged. A newer attempt or a merge in between refuses it.
	async function accept() {
		acceptOpen = false;
		if (busy) return;
		busy = true;
		error = '';
		const previous = chat.liveTasks[task.id] ?? null;
		try {
			const updated = await agentTasks.accept(chat.slug, task.id, task.generation);
			if (!disposed) chat.acceptTask(updated, false, previous);
		} catch (e) {
			if ((e as { status?: number }).status !== 409) failure(e);
			else if (!disposed) {
				error = m['tasks.accept_conflict']();
				try {
					chat.acceptTask(await agentTasks.get(chat.slug, task.id), false, previous);
				} catch (reload) {
					failure(reload);
				}
			}
		} finally {
			if (!disposed) busy = false;
		}
	}
	// A terminal in the task's own worktree, docked at the bottom, for working
	// directly beside (or instead of) the agent.
	const dock = useTerminalDock();
	// A recorded approval shows the task is ready to merge; merging completes it.
	const approver = $derived(task.state === 'awaiting_review' ? approvedBy(signals, task.run_id) : undefined);
	const tone = $derived(approver ? 'success' : pause === 'paused' ? 'warning' : taskTone(task.state));
	// Paused by the agent's usage limit: the alert below explains it.
	const usageLimited = $derived(task.state === 'blocked' && !!task.usage_limit_message);
	const stateLabel = $derived(
		approver
			? m['tasks.approved']()
			: usageLimited || pause === 'paused'
				? m['tasks.state.paused']()
				: pause === 'pausing'
					? m['tasks.pausing']()
					: taskLabel(task.state)
	);
	// The path says why a task waits or stopped, on the step it happened at.
	let pathSaysReason = $state(false);
	// A paused task says so in its state; what it was doing stays as the reason.
	const reason = $derived(
		approver
			? m['tasks.approved_reason']({ name: approver })
			: usageLimited || pathSaysReason || (pause === 'paused' && task.state === 'blocked')
				? ''
				: task.reason
	);
	$effect(() => {
		const observed = [task.state, chat.taskSignalCursor[task.id]];
		if (observed[0] !== 'awaiting_review') return;
		untrack(() => void loadSignals());
	});
	async function loadSignals() {
		try {
			const recorded = await agentTasks.signals(chat.slug, task.id);
			if (!disposed && !chat.unavailable) signals = recorded;
		} catch (e) {
			failure(e);
		}
	}
	// Each iteration says why it started (first run, review changes, a retry
	// after a failure, your follow-up), derived from the recorded transitions
	// and the signals that queued follow-ups.
	const reasons = $derived(iterationReasons(history, signals));
	const iterationReason = $derived<IterationReason | undefined>(reasons[task.generation]);
	// A workflow names the step and the transition that started the iteration.
	const workflowReason = $derived(
		task.workflow_version_id ? workflowIterationReasons(history)[task.generation] : undefined
	);
	const reasonLabels: Record<IterationReason, () => string> = {
		first: m['tasks.iteration_first'],
		review_changes: m['tasks.iteration_review_changes'],
		ci_failed: m['tasks.iteration_ci_failed'],
		follow_up: m['tasks.iteration_follow_up'],
		retry_failed: m['tasks.iteration_retry_failed'],
		retry_blocked: m['tasks.iteration_retry_blocked'],
		restarted: m['tasks.iteration_restarted']
	};
	$effect(() => {
		const observed = [expanded, task.generation, chat.taskSignalCursor[task.id]];
		if (!observed[0] || task.generation < 2) return;
		untrack(() => void loadIterations());
	});
	async function loadIterations() {
		try {
			let after = history.at(-1)?.sequence ?? 0;
			const [recorded] = await Promise.all([
				agentTasks.signals(chat.slug, task.id),
				(async () => {
					// Transitions are paged; an iteration's start can be several pages in.
					for (let page = 0; page < 10; page++) {
						const events = await agentTasks.history(chat.slug, task.id, after);
						if (disposed) return;
						const known = new Set(history.map((e) => e.sequence));
						history = [...history, ...events.transitions.filter((e) => !known.has(e.sequence))];
						historyAfter = events.next_after;
						if (!events.next_after || events.next_after === after) break;
						after = events.next_after;
					}
				})()
			]);
			if (!disposed && !chat.unavailable) signals = recorded;
		} catch (e) {
			failure(e);
		}
	}
	// An attempt's diff is captured when its native run ends.
	const diffReady = $derived(
		!!selectedRun &&
			(changedFiles.length > 0 || !['queued', 'preparing', 'running', 'cancelling', 'cancelled'].includes(task.state))
	);
	const sessionCommand = $derived(
		resumableStates.includes(task.state) ? agentSessionCommand(task.provider, task.id) : undefined
	);
	function openTerminal(initialInput?: string) {
		if (!task.machine_id || !dock) return;
		dock.open({
			initialInput,
			slug: chat.slug,
			machineId: task.machine_id,
			machineName: chat.issue?.identifier ?? task.machine_id,
			checkoutId: task.checkout_id,
			checkoutLabel: chat.issue ? `${chat.issue.identifier} · ${chat.issue.title}` : undefined,
			sessionName: chat.issue?.identifier
		});
	}
	// Claude Code tasks open on the server, which resumes the task's exact
	// session with the creator's saved sign-in (see OpenInClaudeCode).
	const claudeCode = $derived(task.provider === 'claude-code' && resumableStates.includes(task.state));
	function openInClaudeCode() {
		if (!task.machine_id || !dock) return;
		const id = task.id;
		dock.open({
			slug: chat.slug,
			machineId: task.machine_id,
			machineName: chat.issue?.identifier ?? task.machine_id,
			checkoutId: task.checkout_id,
			checkoutLabel: chat.issue ? `${chat.issue.identifier} · ${chat.issue.title}` : undefined,
			sessionName: 'Claude Code',
			launch: () => agentTasks.openInClaudeCode(chat.slug, id)
		});
	}
	const actions = $derived.by(() => {
		const list: TaskAction[] = [];
		// Retrying is the next step once a task stopped; otherwise it is secondary.
		// A paused workflow has one next step: resuming it.
		const resumable = pause === 'paused' || pause === 'pausing';
		const stopped = canRetry && task.state !== 'awaiting_review' && !resumable;
		if (resumable)
			list.push({
				id: 'resume',
				icon: Play,
				label: m['tasks.resume'](),
				disabled: busy || !canResume(task),
				onclick: () => void pauseAction(() => agentTasks.resume(chat.slug, task.id)),
				group: 'main',
				variant: 'primary'
			});
		if (canRetry)
			list.push({
				id: 'retry',
				icon: RotateCcw,
				label: m['tasks.retry'](),
				disabled: busy,
				onclick: () => onretry(task),
				group: stopped ? 'main' : 'more',
				variant: stopped ? 'primary' : undefined
			});
		if (canAccept)
			list.push({
				id: 'accept',
				icon: BadgeCheck,
				label: m['tasks.accept'](),
				title: m['tasks.accept_help'](),
				disabled: busy,
				onclick: () => (acceptOpen = true),
				group: resumable ? 'more' : 'main',
				variant: resumable ? undefined : 'primary'
			});
		if (pullRequest)
			list.push({
				id: 'pr',
				icon: GitPullRequest,
				label: m['tasks.open_pull_request'](),
				href: pullRequest,
				external: true,
				group: 'main',
				variant: stopped || canAccept || resumable ? undefined : 'primary'
			});
		if (diffReady)
			list.push({
				id: 'diff',
				icon: FileDiff,
				label: m['tasks.view_diff'](),
				group: 'main',
				variant: pullRequest || stopped || canAccept || resumable ? undefined : 'primary',
				onclick: () =>
					(chat.diffView = {
						taskId: task.id,
						runId: selectedRun,
						title: `${chat.issue?.identifier ?? ''} · ${m['tasks.attempt']({ number: task.generation })}`
					})
			});
		// Tools, in the menu: the agent's own session first, then QA.
		if (task.machine_id && dock) {
			if (claudeCode)
				list.push({
					id: 'claude-code',
					icon: Bot,
					label: m['tasks.open_in_claude_code'](),
					title: m['tasks.open_in_claude_code_help'](),
					group: 'tools',
					movesFocus: true,
					onclick: openInClaudeCode
				});
			else if (sessionCommand) {
				const command = sessionCommand;
				list.push({
					id: 'session',
					icon: Bot,
					label: m['tasks.agent_session'](),
					title: m['tasks.agent_session_help'](),
					group: 'tools',
					movesFocus: true,
					onclick: () => openTerminal(command)
				});
			}
		}
		if (canRunCheck)
			list.push({
				id: 'check',
				icon: FlaskConical,
				label: checkRunning ? m['tasks.manual_check_running']() : m['tasks.run_check'](),
				title: m['tasks.run_check_help'](),
				group: 'tools',
				disabled: checkBusy || checkRunning,
				onclick: () => void runCheck()
			});
		if (appAction) list.push(appAction);
		if (task.machine_id && dock)
			list.push({
				id: 'terminal',
				icon: SquareTerminal,
				label: m['tasks.agents_terminal'](),
				group: 'tools',
				movesFocus: true,
				onclick: () => openTerminal()
			});
		if (task.machine_id)
			list.push({
				id: 'environment',
				icon: Server,
				label: m['tasks.environment'](),
				href: `/${chat.slug}/machines/${task.machine_id}`,
				group: 'tools'
			});
		if (pause === 'pausable')
			list.push({
				id: 'pause',
				icon: Pause,
				label: m['tasks.pause'](),
				group: 'more',
				disabled: busy,
				menu: (['now', 'after_step'] as const).map((mode) => ({
					kind: 'item' as const,
					id: mode,
					label: mode === 'now' ? m['tasks.pause_now']() : m['tasks.pause_after_step'](),
					onclick: () => void pauseAction(() => agentTasks.pause(chat.slug, task.id, mode))
				}))
			});
		if (update.kind !== 'none')
			list.push({
				id: 'workflow-update',
				icon: CircleArrowUp,
				label: m['tasks.workflow_update']({ version: update.version }),
				group: 'more',
				disabled: busy,
				onclick: () => (switchOpen = true)
			});
		if (canCancel)
			list.push({
				id: 'cancel',
				icon: X,
				label: task.state === 'cancelling' ? m['tasks.retry_cancel']() : m['tasks.cancel'](),
				group: 'more',
				variant: 'danger',
				disabled: busy,
				onclick: () => void cancel()
			});
		return list;
	});
	// Asked for from an inline step or a background task row: open in full
	// and bring it into view.
	let article = $state<HTMLElement>();
	$effect(() => {
		if (chat.focusedTask !== task.id) return;
		untrack(() => {
			expanded = true;
			chat.focusedTask = null;
			queueMicrotask(() => article?.scrollIntoView({ block: 'nearest' }));
		});
	});

	// The attempt stopped for the agent's own sign-in: sign in from the
	// machine's terminal, then continue. One request ID per visit converges a
	// double click on one capture and one new attempt.
	let signInRequest = crypto.randomUUID(),
		continuing = $state(false),
		signInError = $state('');
	const needsSignIn = $derived(!!task.sign_in_required && task.state === 'failed');
	async function continueAfterSignIn() {
		if (continuing) return;
		continuing = true;
		signInError = '';
		try {
			const next = await agentTasks.continueAfterSignIn(chat.slug, task.id, signInRequest);
			if (!disposed) chat.acceptTask(next);
		} catch (e) {
			if (disposed) return;
			const status = (e as { status?: number }).status;
			if (status === 409) {
				signInError = m['tasks.sign_in_not_found']();
				signInRequest = crypto.randomUUID();
			} else failure(e);
		} finally {
			if (!disposed) continuing = false;
		}
	}

	// A task paused at the agent's usage limit resumes on its own after the
	// reset; the creator can resume it now or keep it paused.
	let usageAction = $state<'resume' | 'cancel' | null>(null);
	const when = (value?: string | null) => (value ? new Date(value).toLocaleString() : '');
	async function usageLimitAction(kind: 'resume' | 'cancel') {
		if (usageAction) return;
		usageAction = kind;
		try {
			const next =
				kind === 'resume'
					? await agentTasks.resumeUsageLimit(chat.slug, task.id)
					: await agentTasks.cancelUsageLimitResume(chat.slug, task.id);
			if (!disposed) chat.acceptTask(next);
		} catch (e) {
			failure(e);
		} finally {
			if (!disposed) usageAction = null;
		}
	}

	async function olderRuns() {
		if (!runsBefore || loading) return;
		loading = true;
		try {
			const page = await agentTasks.runs(chat.slug, task.id, runsBefore);
			if (disposed) return;
			const known = new Set(runs.map((run) => run.id));
			runs = [...runs, ...page.runs.filter((run) => !known.has(run.id))];
			runsBefore = page.next_before;
		} catch (e) {
			failure(e);
		} finally {
			if (!disposed) loading = false;
		}
	}
</script>

<!-- A task is a row in the panel's list, not a nested card. -->
<article bind:this={article} class="min-w-0 scroll-mt-2 py-2.5" aria-label={m['tasks.task']()}>
	<div class="flex items-start gap-1.5">
		<Button
			variant="plain"
			class="mt-px flex size-5 shrink-0 cursor-pointer items-center justify-center rounded text-muted-foreground transition-colors outline-none hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 [&_svg]:size-3.5"
			aria-expanded={expanded}
			aria-label={expanded ? m['tasks.collapse_task']() : m['tasks.expand_task']()}
			onclick={() => (expanded = !expanded)}
			>{#if expanded}<ChevronDown />{:else}<ChevronRight />{/if}</Button
		>
		<div class="min-w-0 flex-1 space-y-1">
			<p
				class="min-w-0 text-[13px] leading-5 font-medium break-words {expanded
					? 'line-clamp-2 whitespace-pre-wrap'
					: 'truncate'}"
				title={task.prompt}
			>
				{task.prompt}
			</p>
			<div class="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1 text-xs text-muted-foreground">
				<span class="inline-flex shrink-0 items-center gap-1.5 font-medium {toneText[tone]}" role="status"
					><span aria-hidden="true" class="size-1.5 rounded-full {toneDot[tone]}"></span>{stateLabel}</span
				>
				<span class="inline-flex min-w-0 items-center gap-1"
					><AgentLogo agent={task.provider} size="sm" class="size-4 [&_svg]:size-2.5" /><span class="min-w-0 truncate"
						>{agentLabel(task.provider)}{task.model ? ` · ${task.model}` : ''} · {m['tasks.attempt']({
							number: task.generation
						})}</span
					></span
				>
				<!-- Why the iteration started; the first run needs no explanation. -->
				{#if workflowReason}<Badge
						variant="secondary"
						class="h-4 max-w-full shrink truncate px-1.5 text-[10px] font-normal"
						title={workflowReason}
						data-testid="iteration-reason">{workflowReason}</Badge
					>{:else if iterationReason && iterationReason !== 'first'}<Badge
						variant="secondary"
						class="h-4 shrink-0 px-1.5 text-[10px] font-normal"
						data-testid="iteration-reason">{reasonLabels[iterationReason]()}</Badge
					>{/if}
			</div>
		</div>
		{#if expanded}<TaskMenu {actions} />{/if}
	</div>
	{#if expanded}<div class="mt-2 space-y-2.5 pl-[26px]">
			{#if reason}<p class="flex min-w-0 items-start gap-1.5 text-xs leading-5 text-muted-foreground">
					<span aria-hidden="true" class="mt-[3px] shrink-0 [&_svg]:size-3.5 {toneText[tone]}"
						>{#if active}<LoaderCircle class="animate-spin motion-reduce:animate-none" />{:else if failed}<CircleAlert
							/>{:else if task.state === 'completed' || approver}<CircleCheck
							/>{:else if task.state === 'awaiting_review'}<GitPullRequest />{:else}<Circle />{/if}</span
					><span class="min-w-0 break-words">{reason}</span>
				</p>{/if}
			{#if needsSignIn}
				<Alert.Root class="border-[var(--status-warning)]/40 bg-[var(--status-warning)]/5">
					<CircleAlert class="text-[var(--status-warning)]" />
					<Alert.Title>{m['tasks.sign_in_title']({ agent: agentLabel(task.provider) })}</Alert.Title>
					<Alert.Description class="space-y-3">
						{#if task.provider === 'claude-code'}
							<p>{m['tasks.sign_in_token_body']()}</p>
							<div class="space-y-2">
								<p>{m['settings.agents.claude_token_help']()}</p>
								<code class="block w-fit rounded-md border bg-muted px-2 py-1 font-mono text-foreground"
									>claude setup-token</code
								>
							</div>
							<ClaudeTokenForm
								slug={chat.slug}
								submitLabel={m['tasks.sign_in_token_continue']()}
								onsaved={continueAfterSignIn}
							/>
							<p>{m['tasks.sign_in_terminal_instead']()}</p>
						{:else}<p>{m['tasks.sign_in_body']()}</p>{/if}
						<div class="flex flex-wrap gap-2">
							{#if task.machine_id && dock}<Button
									size="sm"
									variant="outline"
									onclick={() => openTerminal(signInCommand(task.provider))}
									><SquareTerminal />{m['tasks.sign_in_open']()}</Button
								>{/if}
							<Button
								size="sm"
								variant={task.provider === 'claude-code' ? 'outline' : 'default'}
								disabled={continuing}
								onclick={() => void continueAfterSignIn()}
								>{#if continuing}<LoaderCircle class="animate-spin" />{m['tasks.sign_in_continuing']()}{:else}{m[
										'tasks.sign_in_continue'
									]()}{/if}</Button
							>
						</div>
						{#if task.provider === 'claude-code'}<a
								class="inline-block text-xs underline underline-offset-2"
								href="/{chat.slug}/settings/agents#your-sign-ins">{m['tasks.sign_in_manage']()}</a
							>{/if}
						{#if signInError}<p role="alert" class="text-destructive">{signInError}</p>{/if}
					</Alert.Description>
				</Alert.Root>
			{/if}
			{#if usageLimited}
				<Alert.Root
					data-testid="task-usage-limit"
					class="border-[var(--status-warning)]/40 bg-[var(--status-warning)]/5"
				>
					<CirclePause class="text-[var(--status-warning)]" />
					<Alert.Title>{m['tasks.usage_limit_title']({ agent: agentLabel(task.provider) })}</Alert.Title>
					<Alert.Description class="space-y-3">
						<p class="break-words">{task.usage_limit_message}</p>
						{#if task.usage_limit_resets_at}<p>
								{m['tasks.usage_limit_resets']({ time: when(task.usage_limit_resets_at) })}
							</p>{/if}
						<p>
							{task.usage_limit_resume_at
								? m['tasks.usage_limit_resumes']({ time: when(task.usage_limit_resume_at) })
								: m['tasks.usage_limit_manual']()}
						</p>
						<div class="flex flex-wrap gap-2">
							<Button size="sm" disabled={!!usageAction} onclick={() => void usageLimitAction('resume')}
								>{#if usageAction === 'resume'}<LoaderCircle class="animate-spin" />{:else}<Play
									/>{/if}{m['tasks.usage_limit_resume_now']()}</Button
							>
							{#if task.usage_limit_resume_at}<Button
									size="sm"
									variant="outline"
									disabled={!!usageAction}
									onclick={() => void usageLimitAction('cancel')}
									>{#if usageAction === 'cancel'}<LoaderCircle class="animate-spin" />{/if}{m[
										'tasks.usage_limit_dont_resume'
									]()}</Button
								>{/if}
						</div>
					</Alert.Description>
				</Alert.Root>
			{/if}
			<TaskActions {actions} />
			<TaskPath {chat} {task} bind:saysReason={pathSaysReason} />
			<TaskMessages slug={chat.slug} {task} />
			<Trace.Root class="border-t pt-1.5">
				<Trace.Collapsible bind:open>
					<Trace.CollapsibleTrigger
						class="-mx-1 w-[calc(100%+0.5rem)] max-w-none justify-start rounded-md px-1 py-0.5 hover:bg-muted/60 focus-visible:ring-2"
						aria-label={m['tasks.details']()}
						><Trace.Line class="text-xs"
							><Trace.Icon class="[&_svg]:size-3.5"><Terminal /></Trace.Icon><Trace.Title
								>{m['tasks.details']()}</Trace.Title
							></Trace.Line
						></Trace.CollapsibleTrigger
					>
					<Trace.CollapsibleContent class="space-y-3">
						<!-- Rendered only while open: the closed details hold no duplicate state text. -->
						{#if open}{#each history as entry (entry.sequence)}<Trace.Line class="items-start"
									><Trace.Icon><CircleCheck /></Trace.Icon><span class="min-w-0 flex-1"
										><Trace.Title class="block">{taskLabel(entry.to_state)}</Trace.Title><Trace.Description
											class="block whitespace-normal break-words">{entry.reason}</Trace.Description
										></span
									></Trace.Line
								>{/each}{/if}
						{#if historyAfter}<Button variant="ghost" size="sm" disabled={loading} onclick={() => void refresh()}
								>{m['tasks.more_history']()}</Button
							>{/if}
						{#if runs.length > 1}<SelectField
								bind:value={selectedRun}
								aria-label={m['tasks.execution_attempt']()}
								options={runs.map((run) => ({
									value: run.id,
									label: `${run.provider_id} · ${new Date(run.created_at).toLocaleString()}`
								}))}
							/>{/if}
						{#if runsBefore}<Button variant="ghost" size="sm" disabled={loading} onclick={() => void olderRuns()}
								>{m['tasks.more_runs']()}</Button
							>{/if}
						{#each sessions as session (session.id)}<Trace.Line class="items-start"
								><Trace.Icon><AgentLogo agent={session.provider} size="sm" /></Trace.Icon><span class="min-w-0 flex-1"
									><Trace.Title class="block whitespace-normal break-words"
										>{session.purpose === 'review'
											? m['tasks.session_review']({ agent: agentLabel(session.provider) })
											: m['tasks.session_implement']({ agent: agentLabel(session.provider) })}</Trace.Title
									><Trace.Description class="block whitespace-normal break-words"
										>{sessionStates[session.state]()}{#if session.native_session_id}{' · '}<span
												class="font-mono"
												title={session.native_session_id}>{session.native_session_id.slice(0, 8)}</span
											>{/if}</Trace.Description
									></span
								></Trace.Line
							>{/each}
						{#if plan}<AgentPlan steps={plan} />{/if}
						{#if reasoning.length}<AgentReasoning summaries={reasoning} />{/if}
						<TestResults step={validation} output={checkOutput} slug={chat.slug} taskId={task.id} />
						{#if latestCheck}<TestResults
								step={latestCheck}
								output={manualOutput}
								slug={chat.slug}
								taskId={task.id}
								manual
							/>{/if}
						{#if changedFiles.length}<section aria-label={m['tasks.changed_files']()} class="min-w-0 text-sm">
								<p class="mb-1 text-xs font-medium text-muted-foreground">{m['tasks.changed_files']()}</p>
								{#each changedFiles as node (node.path)}<FileTreeNode {node} href={fileLink} />{/each}
							</section>{/if}
						{#if signals.length}<section aria-label={m['tasks.signals']()} class="min-w-0 space-y-1">
								{#each signals as signal (signal.id)}
									{@const note = signalAction[signal.action]}
									<Trace.Line
										class="items-start"
										status={signal.outcome === 'failure' ||
										signal.outcome === 'timed_out' ||
										signal.outcome === 'changes_requested'
											? 'error'
											: undefined}
										><Trace.Icon
											>{#if signal.kind === 'ci'}<CircleCheck />{:else}<GitPullRequest />{/if}</Trace.Icon
										><span class="min-w-0 flex-1"
											><Trace.Title class="block whitespace-normal break-words"
												>{signal.kind === 'ci'
													? m['tasks.signal_ci']({ name: signal.name, outcome: signal.outcome })
													: m['tasks.signal_review']({ name: signal.name, outcome: signal.outcome })}</Trace.Title
											>{#if note}<Trace.Description class="block whitespace-normal break-words"
													>{note()}</Trace.Description
												>{/if}</span
										></Trace.Line
									>
								{/each}
							</section>{/if}
						{#if publication?.step && publication.step.agent_run_id === selectedRun}<Trace.Line
								status={publication.step.status === 'failed'
									? 'error'
									: ['queued', 'running'].includes(publication.step.status)
										? 'running'
										: undefined}
								><Trace.Icon><GitPullRequest /></Trace.Icon><Trace.Title class="whitespace-normal break-words"
									>{publicationLabel}</Trace.Title
								></Trace.Line
							>{/if}
						{#if selected?.summary}<Markdown source={selected.summary} class="text-sm" />{/if}
						{#if activity?.run_id === selectedRun}{#each activity.events.slice(-8) as event, i (i)}<Trace.Line
									><Trace.Title>{event.type}</Trace.Title></Trace.Line
								>{/each}{/if}
						<!-- Raw output is long: collapsed by default, still copyable when open. -->
						<Trace.Collapsible bind:open={outputOpen}>
							<Trace.CollapsibleTrigger
								><Trace.Line
									><Trace.Icon><Terminal /></Trace.Icon><Trace.Title>{m['tasks.output']()}</Trace.Title></Trace.Line
								></Trace.CollapsibleTrigger
							>
							<Trace.CollapsibleContent>
								<!-- The run as a readable session; its Raw toggle keeps the original output. -->
								{#if outputOpen}<AgentTranscript
										chunks={sessionLogs}
										active={active && selectedRun === task.run_id}
									/>{/if}
							</Trace.CollapsibleContent>
						</Trace.Collapsible>
						{#if logsAfter}<Button variant="ghost" size="sm" disabled={loading} onclick={() => void refresh()}
								>{m['tasks.more_logs']()}</Button
							>{/if}
						{#if loading}<p role="status" class="text-xs">{m['assistant.loading']()}</p>{/if}
					</Trace.CollapsibleContent>
				</Trace.Collapsible>
			</Trace.Root>
			{#if appError}<p role="alert" class="text-sm break-words text-destructive">{appError}</p>{/if}
			{#if checkError}<p role="alert" class="text-sm break-words text-destructive">{checkError}</p>{/if}
			{#if error}<p role="alert" class="text-sm text-destructive">{error}</p>
				<Button variant="ghost" size="sm" onclick={() => void refresh()}>{m['tasks.refresh']()}</Button>{/if}
		</div>{/if}
	<AlertDialog.Root bind:open={switchOpen}>
		<AlertDialog.Content>
			<AlertDialog.Header>
				<AlertDialog.Title
					>{update.kind === 'none' ? '' : m['tasks.workflow_update_title']({ version: update.version })}</AlertDialog.Title
				>
				<AlertDialog.Description>
					{#if update.kind === 'ready'}{m['tasks.workflow_update_desc']({ step: update.step })}
					{:else if update.kind === 'pause_first'}{m['tasks.workflow_update_pause_first']()}
					{:else if update.kind === 'step_removed'}{m['tasks.workflow_update_step_removed']({
							version: update.version,
							current: update.current
						})}
					{:else if update.kind === 'step_changed'}{m['tasks.workflow_update_step_changed']({
							version: update.version,
							current: update.current
						})}{/if}
				</AlertDialog.Description>
			</AlertDialog.Header>
			<AlertDialog.Footer>
				<AlertDialog.Cancel>{update.kind === 'ready' ? m['common.cancel']() : m['common.close']()}</AlertDialog.Cancel>
				{#if update.kind === 'ready'}<AlertDialog.Action disabled={busy} onclick={() => void moveToNewestVersion()}
						>{m['tasks.workflow_update_confirm']()}</AlertDialog.Action
					>{/if}
			</AlertDialog.Footer>
		</AlertDialog.Content>
	</AlertDialog.Root>
	<AlertDialog.Root bind:open={acceptOpen}>
		<AlertDialog.Content>
			<AlertDialog.Header>
				<AlertDialog.Title
					>{m['tasks.accept_title']({
						identifier: chat.issue?.identifier ?? m['tasks.this_task']()
					})}</AlertDialog.Title
				>
				<AlertDialog.Description>{m['tasks.accept_desc']()}</AlertDialog.Description>
			</AlertDialog.Header>
			<AlertDialog.Footer>
				<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
				<AlertDialog.Action disabled={busy} onclick={() => void accept()}
					>{m['tasks.accept_confirm']()}</AlertDialog.Action
				>
			</AlertDialog.Footer>
		</AlertDialog.Content>
	</AlertDialog.Root>
</article>
