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
		FlaskConical
	} from '@lucide/svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import TaskActions from './TaskActions.svelte';
	import AgentReviews from './AgentReviews.svelte';
	import TaskMessages from '$lib/features/agent-messages/TaskMessages.svelte';
	import TaskLiveThread from './TaskLiveThread.svelte';
	import ReviewRequestMenu from './ReviewRequestMenu.svelte';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { reviewHarnesses, reviewSessions } from './agent-review';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { signInCommand } from './agent-session';
	import ClaudeTokenForm from '$lib/features/agents/ClaudeTokenForm.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	const agentLabel = (provider: string) => agentInfo(provider).name;
	import { approvedBy } from './review-approval';
	import type { TaskAction, TaskMenuEntry } from './task-actions';
	import { useTerminalDock } from '$lib/features/dev-machines/terminal-dock-context.svelte';
	import { agentSessionCommand, resumableStates } from './agent-session';
	import { taskTone, toneBadge, toneStatus } from './task-tone';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import {
		agentTasks,
		type AgentTask,
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
	import { m } from '$lib/paraglide/messages.js';
	let {
		task,
		chat,
		onretry,
		ondenied,
		pinned = false,
		allowedAgents = []
	}: {
		task: AgentTask;
		chat: ConversationState;
		onretry: (task: AgentTask) => void;
		ondenied: (error: unknown) => void;
		// Coding agents the workspace allows, offered for a requested review.
		allowedAgents?: string[];
		// The newest task feeds the compact header pinned above the chat.
		pinned?: boolean;
	} = $props();
	// Earlier tasks start as one-line rows; the newest one is shown in full.
	let expanded = $state(untrack(() => pinned));
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
		follow_up_failed: m['tasks.signal_follow_up_failed']
	};
	let selectedRun = $state(untrack(() => task.run_id));
	let seenAttempt = untrack(() => task.run_id),
		disposed = false,
		dirty = false,
		initialRunsLoaded = false;
	let refreshTimer: ReturnType<typeof setTimeout> | undefined;
	const active = $derived(['preparing', 'running', 'validating', 'publishing', 'cancelling'].includes(task.state));
	const failed = $derived(['failed', 'blocked'].includes(task.state));
	const canCancel = $derived(!['completed', 'cancelled'].includes(task.state));
	const canRetry = $derived(['failed', 'blocked', 'awaiting_review'].includes(task.state));
	// Work awaiting review can be accepted in Kuayle without merging.
	const canAccept = $derived(task.state === 'awaiting_review' && task.kind !== 'review');
	let acceptOpen = $state(false);
	// The independent check owns its own runtime service; never attribute its
	// output to the native harness or the harness output to the check.
	const checkIds = $derived(new Set([validation?.id, ...manualChecks.map((check) => check.id)].filter(Boolean)));
	const isCheck = (line: DevMachineLogChunk) => !!line.service_id && checkIds.has(line.service_id);
	const latestCheck = $derived(manualChecks.at(-1) ?? null);
	const output = $derived(
		logs
			.filter((line) => !isCheck(line))
			.map((line) => `${line.stream === 'stderr' ? '[stderr] ' : ''}${line.content}`)
			.join('')
	);
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
	// Sessions (the task's own and its requested reviews) are read-only; review
	// changes arrive as task.review_updated events on this conversation.
	$effect(() => {
		const observed = [expanded, task.state, task.run_id, chat.taskReviewCursor[task.id]];
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
	const reviews = $derived(reviewSessions(sessions));
	const harnesses = $derived(reviewHarnesses(allowedAgents, task.provider));
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
	// Reviews are requested from the chat only: the menu writes the request
	// into the composer, and the assistant asks for approval.
	function requestReview(harness: string) {
		chat.composerDraft = m['tasks.review_request_prompt']({
			identifier: chat.issue?.identifier ?? '',
			agent: agentLabel(harness)
		});
	}
	// The task's app: its preview command, started on review or on request,
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
	const tone = $derived(approver ? 'success' : taskTone(task.state));
	const stateLabel = $derived(approver ? m['tasks.approved']() : taskLabel(task.state));
	const reason = $derived(approver ? m['tasks.approved_reason']({ name: approver }) : task.reason);
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
		const stopped = canRetry && task.state !== 'awaiting_review';
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
				group: 'main',
				variant: 'primary'
			});
		if (pullRequest)
			list.push({
				id: 'pr',
				icon: GitPullRequest,
				label: m['tasks.open_pull_request'](),
				href: pullRequest,
				external: true,
				group: 'main',
				variant: stopped || canAccept ? undefined : 'primary'
			});
		if (diffReady)
			list.push({
				id: 'diff',
				icon: FileDiff,
				label: m['tasks.view_diff'](),
				group: 'main',
				variant: pullRequest || stopped || canAccept ? undefined : 'primary',
				onclick: () =>
					(chat.diffView = {
						taskId: task.id,
						runId: selectedRun,
						title: `${chat.issue?.identifier ?? ''} · ${m['tasks.attempt']({ number: task.generation })}`
					})
			});
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
		if (task.machine_id && dock) {
			list.push({
				id: 'terminal',
				icon: SquareTerminal,
				label: m['tasks.agents_terminal'](),
				group: 'tools',
				onclick: () => openTerminal()
			});
			if (claudeCode) {
				list.push({
					id: 'claude-code',
					icon: Bot,
					label: m['tasks.open_in_claude_code'](),
					title: m['tasks.open_in_claude_code_help'](),
					group: 'main',
					onclick: openInClaudeCode
				});
			} else if (sessionCommand) {
				const command = sessionCommand;
				list.push({
					id: 'session',
					icon: Bot,
					label: m['tasks.agent_session'](),
					title: m['tasks.agent_session_help'](),
					group: 'tools',
					onclick: () => openTerminal(command)
				});
			}
		}
		if (task.machine_id)
			list.push({
				id: 'environment',
				icon: Server,
				label: m['tasks.environment'](),
				href: `/${chat.slug}/machines/${task.machine_id}`,
				group: 'tools'
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
	$effect(() => {
		if (!pinned) return;
		const bar = {
			taskId: task.id,
			identifier: chat.issue?.identifier,
			title: task.prompt.split('\n', 1)[0],
			status: stateLabel,
			badgeClass: toneBadge[tone],
			actions
		};
		chat.taskBar = bar;
		return () => {
			if (chat.taskBar?.taskId === bar.taskId) chat.taskBar = null;
		};
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
<article class="min-w-0 py-2" aria-label={m['tasks.task']()}>
	<div class="flex items-start gap-2">
		<Button
			variant="ghost"
			size="icon-sm"
			class="mt-0.5 shrink-0 cursor-pointer"
			aria-expanded={expanded}
			aria-label={expanded ? m['tasks.collapse_task']() : m['tasks.expand_task']()}
			onclick={() => (expanded = !expanded)}
			>{#if expanded}<ChevronDown />{:else}<ChevronRight />{/if}</Button
		>
		<div class="min-w-0 flex-1 space-y-0.5 pt-1">
			<div class="flex items-start justify-between gap-3">
				<p class="min-w-0 text-sm font-medium break-words {expanded ? 'line-clamp-2 whitespace-pre-wrap' : 'truncate'}">
					{task.prompt}
				</p>
				<Badge variant="outline" class="shrink-0 {toneBadge[tone]}" role="status">{stateLabel}</Badge>
			</div>
			<p class="text-xs text-muted-foreground">
				{task.provider}{task.model ? ` · ${task.model}` : ''} · {m['tasks.attempt']({ number: task.generation })}
			</p>
		</div>
	</div>
	{#if expanded}<div class="mt-2 space-y-3 pl-9">
			<Trace.Root>
				<Trace.Line class="items-start" status={toneStatus(tone)}
					><Trace.Icon
						>{#if active}<LoaderCircle class="animate-spin" />{:else if failed || task.state === 'blocked'}<CircleAlert
							/>{:else if task.state === 'completed'}<CircleCheck />{:else if approver}<CircleCheck
							/>{:else if task.state === 'awaiting_review'}<GitPullRequest />{:else}<Circle />{/if}</Trace.Icon
					><Trace.Title class="overflow-visible text-clip whitespace-normal break-words">{reason}</Trace.Title
					></Trace.Line
				>
				<Trace.Collapsible bind:open>
					<Trace.CollapsibleTrigger aria-label={m['tasks.details']()}
						><Trace.Line
							><Trace.Icon><Terminal /></Trace.Icon><Trace.Title>{m['tasks.details']()}</Trace.Title></Trace.Line
						></Trace.CollapsibleTrigger
					>
					<Trace.CollapsibleContent class="space-y-3">
						{#each history as entry (entry.sequence)}<Trace.Line class="items-start"
								><Trace.Icon><CircleCheck /></Trace.Icon><span class="min-w-0 flex-1"
									><Trace.Title class="block">{taskLabel(entry.to_state)}</Trace.Title><Trace.Description
										class="block whitespace-normal break-words">{entry.reason}</Trace.Description
									></span
								></Trace.Line
							>{/each}
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
						<Trace.Collapsible>
							<Trace.CollapsibleTrigger
								><Trace.Line
									><Trace.Icon><Terminal /></Trace.Icon><Trace.Title>{m['tasks.output']()}</Trace.Title></Trace.Line
								></Trace.CollapsibleTrigger
							>
							<Trace.CollapsibleContent>
								<CodeBlock.Root
									><CodeBlock.Header
										><CodeBlock.Title>{m['tasks.output']()}</CodeBlock.Title><CodeBlock.Actions
											><CodeBlock.CopyButton value={output} aria-label={m['assistant.copy']()} /></CodeBlock.Actions
										></CodeBlock.Header
									><CodeBlock.Content
										code={output || m['tasks.no_output']()}
										wrap
										class="max-h-72 text-xs"
									/></CodeBlock.Root
								>
							</Trace.CollapsibleContent>
						</Trace.Collapsible>
						{#if logsAfter}<Button variant="ghost" size="sm" disabled={loading} onclick={() => void refresh()}
								>{m['tasks.more_logs']()}</Button
							>{/if}
						{#if loading}<p role="status" class="text-xs">{m['assistant.loading']()}</p>{/if}
					</Trace.CollapsibleContent>
				</Trace.Collapsible>
			</Trace.Root>
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
			<AgentReviews {reviews} />
			<TaskMessages slug={chat.slug} {task} />
			{#if task.kind !== 'review'}<TaskLiveThread {chat} {task} />{/if}
			{#if appError}<p role="alert" class="text-sm break-words text-destructive">{appError}</p>{/if}
			{#if checkError}<p role="alert" class="text-sm break-words text-destructive">{checkError}</p>{/if}
			{#if error}<p role="alert" class="text-sm text-destructive">{error}</p>
				<Button variant="ghost" size="sm" onclick={() => void refresh()}>{m['tasks.refresh']()}</Button>{/if}
			<TaskActions {actions}>
				{#snippet extra()}
					{#if task.state === 'awaiting_review' && pullRequest && harnesses.length && chat.issue}<ReviewRequestMenu
							{harnesses}
							implementer={task.provider}
							onpick={requestReview}
						/>{/if}
				{/snippet}
			</TaskActions>
		</div>{/if}
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
