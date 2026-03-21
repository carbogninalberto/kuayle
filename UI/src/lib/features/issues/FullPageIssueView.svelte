<script lang="ts">
	import { onMount } from 'svelte';
	import type { Issue, Comment, IssueHistory, IssueStatus, IssuePriority } from '$lib/types/issue';
	import { PRIORITY_LABELS } from '$lib/types/issue';
	import { teamStatusesState } from './team-statuses.state.svelte';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Label } from '$lib/types/label';
	import type { Project } from '$lib/types/project';
	import { listComments, createComment, resolveComment, reopenComment, getIssueHistory, getIssue } from '$lib/api/issues';
	import { listMembers } from '$lib/api/members';
	import { listLabels } from '$lib/api/labels';
	import { listProjects } from '$lib/api/projects';
	import { issuesState } from './issues.state.svelte';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import DatePickerPopover from '$lib/components/shared/DatePickerPopover.svelte';
	import RichEditor from '$lib/components/shared/RichEditor.svelte';
	import { formatRelativeTime } from '$lib/utils/format';
	import { toast } from 'svelte-sonner';
	import * as Popover from '$lib/components/ui/popover';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import {
		ChevronUp, ChevronDown, ChevronRight, Plus, CalendarDays, X,
		Copy, Link as LinkIcon, GitBranch, SquareMousePointer,
		CircleDot, ArrowUpCircle, UserCircle, FolderKanban, Pencil, Layers,
		Tag, Gauge, RefreshCw, ArrowUp, Paperclip, MoreHorizontal, Check,
		Trash2, CornerDownRight
	} from 'lucide-svelte';
	import { listCycles } from '$lib/api/cycles';
	import type { Cycle } from '$lib/types/cycle';
	import IssueRelations from './IssueRelations.svelte';
	import SubIssuesList from './SubIssuesList.svelte';
	import { goto } from '$app/navigation';
	import { sanitizeHtml } from '$lib/security/sanitize';

	let {
		issue,
		slug,
		onnavigate,
		onupdated
	}: {
		issue: Issue;
		slug: string;
		onnavigate?: (direction: 'prev' | 'next') => void;
		onupdated?: (issue: Issue) => void;
	} = $props();

	let comments = $state<Comment[]>([]);
	let history = $state<IssueHistory[]>([]);
	let members = $state<WorkspaceMember[]>([]);
	let labels = $state<Label[]>([]);
	let projects = $state<Project[]>([]);
	let newComment = $state('');
	let commentVersion = $state(0);
	let replyContents = $state<Record<string, string>>({});
	let replyVersions = $state<Record<string, number>>({});
	let commentMenuId = $state<string | null>(null);
	let editingTitle = $state(false);
	let titleValue = $state('');
	let statusOpen = $state(false);
	let priorityOpen = $state(false);
	let assigneeOpen = $state(false);
	let labelsOpen = $state(false);
	let cycles = $state<Cycle[]>([]);
	let cycleOpen = $state(false);
	let estimateOpen = $state(false);
	let projectOpen = $state(false);
	let loaded = $state(false);
	let showAllActivity = $state(false);

	// Collapsible sidebar sections
	let detailsExpanded = $state(true);
	let labelsExpanded = $state(true);
	let projectExpanded = $state(true);
	let cycleExpanded = $state(true);

	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];
	const imageUploadUrl = $derived(`/api/workspaces/${slug}/upload`);

	let issueProject = $derived(projects.find(p => p.id === issue.project_id));
	let issueCycle = $derived(cycles.find(c => c.id === issue.cycle_id));

	onMount(async () => {
		// Load team statuses (needed on direct navigation / refresh)
		await teamStatusesState.load(slug, issue.team_id);

		const [c, h, m, l, p] = await Promise.all([
			listComments(slug, issue.identifier),
			getIssueHistory(slug, issue.identifier),
			listMembers(slug),
			listLabels(slug),
			listProjects(slug)
		]);
		comments = c ?? [];
		history = h ?? [];
		members = m ?? [];
		labels = l ?? [];
		projects = p ?? [];
		loaded = true;
		listCycles(slug, issue.team_id).then(c => cycles = c).catch(() => {});
	});

	$effect(() => {
		titleValue = issue.title;
	});

	async function saveTitle() {
		editingTitle = false;
		if (titleValue.trim() && titleValue !== issue.title) {
			try {
				const updated = await issuesState.update(slug, issue.identifier, { title: titleValue.trim() });
				onupdated?.(updated);
			} catch {
				titleValue = issue.title;
				toast.error('Failed to update title');
			}
		} else {
			titleValue = issue.title;
		}
	}

	async function saveDescription(html: string) {
		try {
			const updated = await issuesState.update(slug, issue.identifier, { description: html });
			onupdated?.(updated);
		} catch {
			toast.error('Failed to update description');
		}
	}

	async function updateField(field: string, value: any) {
		try {
			await issuesState.update(slug, issue.identifier, { [field]: value });
			await refreshIssue();
		} catch {
			toast.error(`Failed to update ${field}`);
		}
	}

	function formatHistoryValue(field: string, value: string | null): string {
		if (!value) return 'none';
		switch (field) {
			case 'status':
				return value;
			case 'priority':
				return PRIORITY_LABELS[Number(value) as IssuePriority] ?? value;
			case 'assignee_id': {
				const member = members.find(m => m.user_id === value);
				return member ? (member.name || member.email) : 'Unassigned';
			}
			case 'project': {
				const p = projects.find(p => p.id === value);
				return p ? p.name : '-';
			}
			case 'cycle': {
				const c = cycles.find(c => c.id === value);
				return c ? c.name : '-';
			}
			case 'estimate':
				return value ? `${value} pts` : '-';
			case 'due_date':
				return value || '-';
			case 'labels':
				return value || '-';
			default:
				return value;
		}
	}

	function historyFieldLabel(field: string): string {
		switch (field) {
			case 'assignee_id': return 'assignee';
			case 'due_date': return 'due date';
			default: return field;
		}
	}

	function historyIcon(field: string): typeof CircleDot {
		switch (field) {
			case 'status': return CircleDot;
			case 'priority': return ArrowUpCircle;
			case 'assignee_id': return UserCircle;
			case 'title': case 'description': return Pencil;
			case 'due_date': return CalendarDays;
			case 'labels': return Tag;
			case 'estimate': return Gauge;
			case 'project': return FolderKanban;
			case 'cycle': return RefreshCw;
			default: return CircleDot;
		}
	}

	function historyColor(field: string): string {
		switch (field) {
			case 'status': return 'text-blue-400';
			case 'priority': return 'text-orange-400';
			case 'assignee_id': return 'text-purple-400';
			case 'due_date': return 'text-red-400';
			case 'labels': return 'text-teal-400';
			case 'estimate': return 'text-green-400';
			case 'project': return 'text-indigo-400';
			case 'cycle': return 'text-cyan-400';
			case 'title': case 'description': return 'text-[var(--color-text-tertiary)]';
			default: return 'text-[var(--color-text-tertiary)]';
		}
	}

	async function handleAddComment() {
		if (!newComment.trim() || newComment === '<p></p>') return;
		try {
			await createComment(slug, issue.identifier, newComment);
			newComment = '';
			commentVersion++;
			refreshActivity();
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to add comment');
		}
	}

	async function handleReply(parentId: string) {
		const content = replyContents[parentId] ?? '';
		if (!content.trim() || content === '<p></p>') return;
		try {
			await createComment(slug, issue.identifier, content, parentId);
			replyContents[parentId] = '';
			replyVersions[parentId] = (replyVersions[parentId] ?? 0) + 1;
			replyVersions = { ...replyVersions };
			refreshActivity();
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to reply');
		}
	}

	async function handleResolve(commentId: string) {
		try {
			await resolveComment(slug, issue.identifier, commentId);
			commentMenuId = null;
			refreshActivity();
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to resolve');
		}
	}

	async function handleReopen(commentId: string) {
		try {
			await reopenComment(slug, issue.identifier, commentId);
			commentMenuId = null;
			refreshActivity();
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to reopen');
		}
	}

	async function refreshIssue() {
		try {
			const fresh = await getIssue(slug, issue.identifier);
			const idx = issuesState.issues.findIndex(i => i.identifier === issue.identifier);
			if (idx >= 0) issuesState.issues[idx] = fresh;
			if (issuesState.selectedIssue?.identifier === issue.identifier) {
				issuesState.selectedIssue = fresh;
			}
			onupdated?.(fresh);
		} catch { /* ignore */ }
		// Refresh activity in background
		refreshActivity();
	}

	async function refreshActivity() {
		try {
			const [c, h] = await Promise.all([
				listComments(slug, issue.identifier),
				getIssueHistory(slug, issue.identifier)
			]);
			comments = c ?? [];
			history = h ?? [];
		} catch { /* ignore */ }
	}

	function copyToClipboard(text: string, label: string) {
		navigator.clipboard.writeText(text);
		toast.success(`${label} copied`);
	}

	function getUsername(): string {
		const user = authState.user;
		if (!user) return 'user';
		// Use name or email prefix, lowercase, no spaces
		const name = (user.name || user.email.split('@')[0])
			.toLowerCase()
			.replace(/[^a-z0-9]/g, '');
		return name || 'user';
	}

	function getBranchName(): string {
		const id = issue.identifier.toLowerCase();
		const title = issue.title
			.toLowerCase()
			.replace(/[^a-z0-9\s-]/g, '')
			.replace(/\s+/g, '-')
			.slice(0, 50)
			.replace(/-$/, '');
		return `${getUsername()}/${id}-${title}`;
	}

	async function copyBranchAndMoveToProgress() {
		const branch = getBranchName();
		navigator.clipboard.writeText(branch);

		// Move to "in progress" (started category)
		const startedStatus = teamStatusesState.statuses.find(s => s.category === 'started');
		if (startedStatus && issue.status_id !== startedStatus.id) {
			try {
				await issuesState.update(slug, issue.identifier, { status_id: startedStatus.id });
				const fresh = await getIssue(slug, issue.identifier);
				onupdated?.(fresh);
				toast.success('Branch copied & moved to In Progress');
			} catch {
				toast.success('Branch copied');
			}
		} else {
			toast.success('Branch name copied');
		}
	}

	function getAIPrompt(): string {
		let prompt = `Work on issue ${issue.identifier}:\n\n`;
		prompt += `<issue identifier="${issue.identifier}">\n`;
		prompt += `<title>${issue.title}</title>\n`;
		const teamKey = issue.identifier.split('-')[0];
		prompt += `<team name="${teamKey}"/>\n`;
		if (issue.labels && issue.labels.length > 0) {
			for (const l of issue.labels) {
				prompt += `<label>${l.name}</label>\n`;
			}
		}
		if (issueProject) {
			prompt += `<project name="${issueProject.name}">${issueProject.description ?? ''}</project>\n`;
		}
		if (issue.description) {
			prompt += `<description>${issue.description.replace(/<[^>]*>/g, '')}</description>\n`;
		}
		prompt += `</issue>`;
		return prompt;
	}

	function formatDueDate(dateStr: string): { label: string; colorClass: string } {
		const due = new Date(dateStr);
		const now = new Date();
		const diffDays = Math.ceil((due.getTime() - now.getTime()) / 86400000);
		let label: string;
		if (diffDays === 0) label = 'Today';
		else if (diffDays === 1) label = 'Tomorrow';
		else if (diffDays === -1) label = 'Yesterday';
		else label = due.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });

		const colorClass = diffDays < 0
			? 'text-red-400'
			: diffDays <= 1
				? 'text-orange-400'
				: 'text-[var(--color-text-primary)]';

		return { label, colorClass };
	}

	let issueCount = $derived(issuesState.issues.length);
	let currentIndex = $derived(issuesState.issues.findIndex(i => i.identifier === issue.identifier));
</script>

<div class="flex h-full flex-col animate-in fade-in duration-150">
	<!-- Top bar — matches sidebar h-[49px] -->
	<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-4">
		<div class="flex items-center gap-1.5 text-xs">
			<a
				href="/{slug}/teams/{issue.team_id}"
				class="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] transition-colors"
			>
				{issue.identifier.split('-')[0]}
			</a>
			<span class="text-[var(--color-text-tertiary)]">&rsaquo;</span>
			<span class="font-medium text-[var(--color-text-primary)]">{issue.identifier}</span>
		</div>
		<div class="flex items-center gap-0.5">
			<!-- Actions -->
			<button
				onclick={() => copyToClipboard(issue.identifier, 'ID')}
				class="rounded p-1.5 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)] transition-colors"
				title="Copy issue ID"
			>
				<Copy size={14} />
			</button>
			<button
				onclick={() => copyToClipboard(window.location.href, 'Link')}
				class="rounded p-1.5 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)] transition-colors"
				title="Copy link"
			>
				<LinkIcon size={14} />
			</button>
			<button
				onclick={copyBranchAndMoveToProgress}
				class="rounded p-1.5 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)] transition-colors"
				title="Copy git branch name & move to In Progress"
			>
				<GitBranch size={14} />
			</button>
			<button
				onclick={() => copyToClipboard(getAIPrompt(), 'AI prompt')}
				class="rounded p-1.5 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)] transition-colors"
				title="Copy AI prompt"
			>
				<SquareMousePointer size={14} />
			</button>

			{#if onnavigate && issueCount > 0}
				<div class="ml-1 flex items-center gap-0.5 border-l border-[var(--app-border)] pl-2">
					<span class="text-[11px] text-[var(--color-text-tertiary)] mr-1">{currentIndex + 1}/{issueCount}</span>
					<button
						onclick={() => onnavigate?.('prev')}
						class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)] transition-colors"
						title="Previous issue (K)"
					>
						<ChevronUp size={16} />
					</button>
					<button
						onclick={() => onnavigate?.('next')}
						class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)] transition-colors"
						title="Next issue (J)"
					>
						<ChevronDown size={16} />
					</button>
				</div>
			{/if}
		</div>
	</div>

	<!-- Main content -->
	<div class="flex flex-1 overflow-hidden">
		<!-- Left column — main content -->
		<div class="flex-1 overflow-y-auto">
			<div class="mx-auto max-w-[840px] px-10 py-6">
				<!-- Title -->
				<!-- svelte-ignore a11y_autofocus -->
				{#if editingTitle}
					<input
						type="text"
						bind:value={titleValue}
						onblur={saveTitle}
						onkeydown={(e) => { if (e.key === 'Enter') saveTitle(); if (e.key === 'Escape') { titleValue = issue.title; editingTitle = false; } }}
						autofocus
						class="w-full bg-transparent text-lg font-semibold text-[var(--color-text-primary)] outline-none"
					/>
				{:else}
					<button
						onclick={() => (editingTitle = true)}
						class="w-full text-left text-lg font-semibold text-[var(--color-text-primary)] hover:text-[var(--color-text-primary)] transition-colors"
					>
						{issue.title}
					</button>
				{/if}

				<!-- Description -->
				<div class="mt-3">
					<RichEditor
						content={issue.description ?? ''}
						placeholder="Add description..."
						bubbleMenu={true}
						borderless={true}
						uploadUrl={imageUploadUrl}
						onupdate={saveDescription}
					/>
				</div>

				<!-- Sub-issues -->
				<div class="mt-5">
					{#if (issue.sub_issue_count ?? 0) > 0}
						<SubIssuesList
							{slug}
							identifier={issue.identifier}
							subIssueCount={issue.sub_issue_count ?? 0}
							subIssueDone={issue.sub_issue_done ?? 0}
							onclickissue={(sub) => goto(`/${slug}/issue/${sub.identifier}`)}
						/>
					{:else}
						<button
							onclick={() => toast.info('Create sub-issue coming soon')}
							class="flex items-center gap-1.5 text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)] transition-colors"
						>
							<Plus size={12} />
							Add sub-issue
						</button>
					{/if}
				</div>

				<!-- Relations -->
				<div class="mt-4">
					<IssueRelations {slug} identifier={issue.identifier} />
				</div>

				<!-- Activity -->
				<div class="mt-6 border-t border-[var(--app-border)] pt-4">
					<h3 class="text-xs font-medium text-[var(--color-text-tertiary)] uppercase tracking-wide mb-3">Activity</h3>

					{#if loaded}
						{@const GROUP_THRESHOLD_MS = 5000}
						{@const historyGroups = [...history].sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime()).reduce<Array<{ items: IssueHistory[]; time: string }>>((acc, h) => {
							const prev = acc[acc.length - 1];
							if (prev && Math.abs(new Date(h.created_at).getTime() - new Date(prev.time).getTime()) < GROUP_THRESHOLD_MS) {
								prev.items.push(h);
							} else {
								acc.push({ items: [h], time: h.created_at });
							}
							return acc;
						}, [])}

						{@const RECENT_COUNT = 10}
						{@const visibleHistory = showAllActivity ? historyGroups : historyGroups.slice(-RECENT_COUNT)}
						{@const hiddenCount = historyGroups.length - visibleHistory.length}

						<div class="relative">
							{#if visibleHistory.length > 0}
								<div class="absolute left-[9px] top-3 bottom-0 w-px bg-[var(--app-border)]"></div>
							{/if}

							{#if hiddenCount > 0}
								<button
									onclick={() => showAllActivity = true}
									class="relative z-10 mb-2 rounded-full border border-[var(--app-border)] bg-[var(--color-bg)] px-2.5 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)] transition-colors"
								>
									Show {hiddenCount} earlier {hiddenCount === 1 ? 'event' : 'events'}
								</button>
							{/if}

							<div>
								{#each visibleHistory as entry}
									{@const items = entry.items}
									{@const firstField = items[0].field}
									{@const IconComponent = items.length > 1 ? Layers : historyIcon(firstField)}
									{@const iconColor = items.length > 1 ? 'text-[var(--color-text-tertiary)]' : historyColor(firstField)}
									{@const textFields = [...new Set(items.filter(c => c.field === 'title' || c.field === 'description').map(c => c.field))]}
									{@const valueItems = items.filter((c, i, arr) => c.field !== 'title' && c.field !== 'description' && arr.findIndex(x => x.field === c.field) === i)}
									<div class="relative flex items-center gap-3 pb-2.5">
										<div class="relative z-10 flex h-5 w-5 shrink-0 items-center justify-center ring-2 ring-[var(--color-bg)] rounded-full bg-[var(--color-bg)] {iconColor}">
											<IconComponent size={12} />
										</div>
										<div class="flex items-center gap-1.5 text-xs text-[var(--color-text-tertiary)] min-w-0 overflow-hidden">
											{#if textFields.length > 0}
												<span>updated <strong class="text-[var(--color-text-secondary)]">{textFields.map(f => historyFieldLabel(f)).join(', ')}</strong></span>
												{#if valueItems.length > 0}<span class="text-[var(--app-border)]">|</span>{/if}
											{/if}
											{#each valueItems as change, idx}
												{#if idx > 0}<span class="text-[var(--app-border)]">|</span>{/if}
												<strong class="text-[var(--color-text-secondary)]">{historyFieldLabel(change.field)}</strong>
												<span>&rarr;</span>
												{#if change.field === 'labels' && change.new_value}
													{#each change.new_value.split(', ') as labelName}
														{@const label = labels.find(l => l.name === labelName)}
														<code class="shrink-0 inline-flex items-center gap-1 rounded bg-[var(--color-bg-tertiary)] px-1 py-0.5 text-[11px] text-[var(--color-text-secondary)]">
															<span class="inline-block h-2 w-2 rounded-full shrink-0" style="background-color: {label?.color ?? 'var(--color-text-tertiary)'}"></span>
															{labelName}
														</code>
													{/each}
												{:else}
													<code class="shrink-0 rounded bg-[var(--color-bg-tertiary)] px-1 py-0.5 text-[11px] text-[var(--color-text-secondary)]">{formatHistoryValue(change.field, change.new_value)}</code>
												{/if}
											{/each}
											<span>&middot;</span>
											<span class="shrink-0">{formatRelativeTime(entry.time)}</span>
										</div>
									</div>
								{/each}
							</div>
						</div>

						{#if showAllActivity && historyGroups.length > 10}
							<button
								onclick={() => showAllActivity = false}
								class="relative z-10 mt-2 rounded-full border border-[var(--app-border)] bg-[var(--color-bg)] px-2.5 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)] transition-colors"
							>
								Show less
							</button>
						{/if}

						{#if historyGroups.length === 0}
							<p class="text-xs text-[var(--color-text-tertiary)]">No activity yet</p>
						{/if}
					{/if}
				</div>

				<!-- Comments -->
				<div class="mt-4 space-y-3">
					{#each comments as comment (comment.id)}
						<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
							<!-- Comment header + body -->
							<div class="group/comment p-4">
								<div class="flex items-center gap-2">
									<div class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[var(--app-accent)] text-[8px] font-medium text-white">
										{(comment.user?.name ?? 'U').charAt(0).toUpperCase()}
									</div>
									<span class="text-[13px] font-medium text-[var(--color-text-primary)]">{comment.user?.name ?? 'User'}</span>
									<span class="text-[11px] text-[var(--color-text-tertiary)]">{formatRelativeTime(comment.created_at)}</span>
									{#if comment.resolved_at}
										<span class="text-[11px] font-medium text-green-400">Resolved</span>
									{/if}
									<div class="ml-auto opacity-0 group-hover/comment:opacity-100 transition-opacity">
										{#if comment.resolved_at}
											<button onclick={() => handleReopen(comment.id)} class="flex items-center gap-1 rounded-full border border-[var(--app-border)] px-2 py-0.5 text-[11px] text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] transition-colors" title="Reopen thread">
												Reopen thread
											</button>
										{:else}
											<button onclick={() => handleResolve(comment.id)} class="rounded p-1 text-[var(--color-text-tertiary)] hover:text-green-400 hover:bg-[var(--color-bg-hover)]" title="Resolve thread">
												<Check size={14} />
											</button>
										{/if}
									</div>
								</div>
								<div class="prose prose-invert prose-sm max-w-none mt-2.5 text-[13px] text-[var(--color-text-primary)] [&>p:first-child]:mt-0 [&>p:last-child]:mb-0">
									{@html sanitizeHtml(comment.body ?? '')}
								</div>
							</div>

							<!-- Replies -->
							{#if comment.replies && comment.replies.length > 0}
								{#each comment.replies as reply (reply.id)}
									<div class="group/reply border-t border-[var(--app-border)] px-4 py-3 pl-4">
										<div class="flex items-center gap-2">
											<div class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[var(--app-accent)] text-[8px] font-medium text-white">
												{(reply.user?.name ?? 'U').charAt(0).toUpperCase()}
											</div>
											<span class="text-[13px] font-medium text-[var(--color-text-primary)]">{reply.user?.name ?? 'User'}</span>
											<span class="text-[11px] text-[var(--color-text-tertiary)]">{formatRelativeTime(reply.created_at)}</span>
										</div>
										<div class="prose prose-invert prose-sm max-w-none mt-2.5 text-[13px] text-[var(--color-text-primary)] [&>p:first-child]:mt-0 [&>p:last-child]:mb-0">
											{@html sanitizeHtml(reply.body ?? '')}
										</div>
									</div>
								{/each}
							{/if}

							<!-- Reply input (hidden when resolved) -->
							{#if !comment.resolved_at}
								<div class="border-t border-[var(--app-border)] px-4 py-3 flex gap-3">
									<div class="my-auto flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[var(--app-accent)] text-[8px] font-medium text-white">
										{(authState.user?.name ?? 'U').charAt(0).toUpperCase()}
									</div>
									<div class="min-w-0 flex items-center w-full">
										{#key replyVersions[comment.id] ?? 0}
											<RichEditor
												content=""
												placeholder="Leave a reply..."
												minimal={true}
												borderless={true}
												uploadUrl={imageUploadUrl}
												onupdate={(html) => { replyContents[comment.id] = html; replyContents = replyContents; }}
												onsubmit={() => handleReply(comment.id)}
											/>
										{/key}
										<div class="flex items-center justify-end gap-1.5 mt-1">
											<button class="rounded p-1.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]" title="Attach file">
												<Paperclip size={14} />
											</button>
											<button
												onclick={() => handleReply(comment.id)}
												disabled={!(replyContents[comment.id]?.trim()) || replyContents[comment.id] === '<p></p>'}
												class="rounded-full bg-[var(--app-accent)] p-1.5 text-white hover:bg-[var(--app-accent-hover)] disabled:opacity-30 transition-colors"
												title="Send (Ctrl+Enter)"
											>
												<ArrowUp size={12} />
											</button>
										</div>
									</div>
								</div>
							{/if}
						</div>
					{/each}

					<!-- New comment input -->
					<div class="flex items-center rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] focus-within:border-[var(--color-text-tertiary)] transition-colors p-3">
						{#key commentVersion}
						<RichEditor
							content=""
							placeholder="Leave a comment..."
							minimal={true}
							borderless={true}
							uploadUrl={imageUploadUrl}
							onupdate={(html) => newComment = html}
							onsubmit={handleAddComment}
						/>
					{/key}
						<div class="flex items-center justify-end gap-1.5 px-3 py-0">
							<button class="rounded p-1.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]" title="Attach file">
								<Paperclip size={14} />
							</button>
							<button
								onclick={handleAddComment}
								disabled={!newComment.trim() || newComment === '<p></p>'}
								class="rounded-full bg-[var(--app-accent)] p-1.5 text-white hover:bg-[var(--app-accent-hover)] disabled:opacity-30 transition-colors"
								title="Send (Ctrl+Enter)"
							>
								<ArrowUp size={14} />
							</button>
						</div>
					</div>
				</div>
			</div>
		</div>

		<!-- Right column — card-based sidebar -->
		<div class="w-[300px] shrink-0 overflow-y-auto p-3 space-y-2">
			<!-- Details card -->
			<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
				<button
					onclick={() => detailsExpanded = !detailsExpanded}
					class="flex w-full items-center gap-1.5 px-3 py-2.5 text-[11px] font-medium uppercase tracking-wider text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)] transition-colors"
				>
					<ChevronRight size={12} class="transition-transform {detailsExpanded ? 'rotate-90' : ''}" />
					Details
				</button>
				{#if detailsExpanded}
					<div class="px-1.5 pb-2 space-y-0.5">
						<!-- Status row -->
						<div class="flex items-center gap-3 rounded-md px-2 py-1.5 hover:bg-[var(--color-bg-hover)] transition-colors">
							<span class="w-20 shrink-0 text-xs text-[var(--color-text-tertiary)]">Status</span>
							<Popover.Root bind:open={statusOpen}>
								<Popover.Trigger>
									<button class="flex items-center gap-1.5 text-sm text-[var(--color-text-primary)]">
										<IssueStatusIcon status={issue.status} category={issue.status_info?.category} color={issue.status_info?.color} size={14} />
										{issue.status_info?.name ?? issue.status}
									</button>
								</Popover.Trigger>
								<Popover.Content class="w-44 p-1" align="start">
									{#each teamStatusesState.statusOrder as ts}
										<button
											onclick={() => { updateField('status_id', ts.id); statusOpen = false; }}
											class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] transition-colors {issue.status_id === ts.id ? 'bg-[var(--color-bg-hover)]' : ''}"
										>
											<IssueStatusIcon category={ts.category} color={ts.color} size={14} />
											{ts.name}
										</button>
									{/each}
								</Popover.Content>
							</Popover.Root>
						</div>

						<!-- Priority row -->
						<div class="flex items-center gap-3 rounded-md px-2 py-1.5 hover:bg-[var(--color-bg-hover)] transition-colors">
							<span class="w-20 shrink-0 text-xs text-[var(--color-text-tertiary)]">Priority</span>
							<Popover.Root bind:open={priorityOpen}>
								<Popover.Trigger>
									<button class="flex items-center gap-1.5 text-sm text-[var(--color-text-primary)]">
										<IssuePriorityIcon priority={issue.priority} size={14} />
										{PRIORITY_LABELS[issue.priority]}
									</button>
								</Popover.Trigger>
								<Popover.Content class="w-40 p-1" align="start">
									{#each priorityValues as value}
										<button
											onclick={() => { updateField('priority', value); priorityOpen = false; }}
											class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] transition-colors {issue.priority === value ? 'bg-[var(--color-bg-hover)]' : ''}"
										>
											<IssuePriorityIcon priority={value} size={14} />
											{PRIORITY_LABELS[value]}
										</button>
									{/each}
								</Popover.Content>
							</Popover.Root>
						</div>

						<!-- Assignee row -->
						<div class="flex items-start gap-3 rounded-md px-2 py-1.5 hover:bg-[var(--color-bg-hover)] transition-colors">
							<span class="w-20 shrink-0 text-xs text-[var(--color-text-tertiary)] pt-0.5">Assignee</span>
							<div class="flex flex-wrap items-center gap-1 flex-1">
								{#if issue.assignees && issue.assignees.length > 0}
									{#each issue.assignees as a}
										<span class="flex items-center gap-1.5 rounded-full bg-[var(--color-bg-tertiary)] px-2 py-0.5 text-sm text-[var(--color-text-primary)]">
											<div class="flex h-4 w-4 items-center justify-center rounded-full bg-[var(--app-accent)] text-[8px] text-white shrink-0">
												{(a.name ?? 'U').charAt(0).toUpperCase()}
											</div>
											{a.name}
										</span>
									{/each}
								{:else if issue.assignee}
									<span class="flex items-center gap-1.5 text-sm text-[var(--color-text-primary)]">
										<div class="flex h-4 w-4 items-center justify-center rounded-full bg-[var(--app-accent)] text-[8px] text-white">
											{(issue.assignee.name ?? 'U').charAt(0).toUpperCase()}
										</div>
										{issue.assignee.name}
									</span>
								{:else}
									<span class="text-sm text-[var(--color-text-tertiary)]">Add assignee</span>
								{/if}
								<Popover.Root bind:open={assigneeOpen}>
									<Popover.Trigger>
										<button class="flex h-5 w-5 items-center justify-center rounded-full hover:bg-[var(--color-bg-tertiary)] text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)] transition-colors">
											<Plus size={14} />
										</button>
									</Popover.Trigger>
									<Popover.Content class="w-48 p-1" align="start">
										{#each members as member}
											{@const isAssigned = (issue.assignees ?? []).some(a => a.id === member.user_id)}
											<button
												onclick={async () => {
													const currentIds = (issue.assignees ?? []).map(a => a.id);
													const newIds = isAssigned
														? currentIds.filter(id => id !== member.user_id)
														: [...currentIds, member.user_id];
													try {
														await issuesState.update(slug, issue.identifier, { assignee_ids: newIds });
														await refreshIssue();
													} catch { toast.error('Failed to update assignees'); }
												}}
												class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
											>
												<Checkbox checked={isAssigned} />
												<div class="flex h-4 w-4 items-center justify-center rounded-full bg-[var(--app-accent)] text-[8px] text-white">
													{(member.name || member.email).charAt(0).toUpperCase()}
												</div>
												{member.name || member.email}
											</button>
										{/each}
										{#if members.length === 0}
											<p class="px-2 py-3 text-center text-xs text-[var(--color-text-tertiary)]">No members</p>
										{/if}
									</Popover.Content>
								</Popover.Root>
							</div>
						</div>

						<!-- Due date row -->
						<div class="flex items-center gap-3 rounded-md px-2 py-1.5 hover:bg-[var(--color-bg-hover)] transition-colors">
							<span class="w-20 shrink-0 text-xs text-[var(--color-text-tertiary)]">Due date</span>
							<DatePickerPopover
								value={issue.due_date}
								onchange={(d) => updateField('due_date', d ?? '')}
								placeholder="Set date"
								colorClass={issue.due_date ? formatDueDate(issue.due_date).colorClass : ''}
							/>
						</div>

						<!-- Estimate row -->
						<div class="flex items-center gap-3 rounded-md px-2 py-1.5 hover:bg-[var(--color-bg-hover)] transition-colors">
							<span class="w-20 shrink-0 text-xs text-[var(--color-text-tertiary)]">Estimate</span>
							<Popover.Root bind:open={estimateOpen}>
								<Popover.Trigger>
									<button class="text-sm {issue.estimate !== null && issue.estimate !== undefined ? 'text-[var(--color-text-primary)]' : 'text-[var(--color-text-tertiary)]'}">
										{issue.estimate !== null && issue.estimate !== undefined ? `${issue.estimate} pts` : 'Set estimate'}
									</button>
								</Popover.Trigger>
								<Popover.Content class="w-28 p-1" align="start">
									<button
										onclick={() => { updateField('estimate', null); estimateOpen = false; }}
										class="flex w-full items-center rounded px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
									>
										Clear
									</button>
									{#each [0, 1, 2, 3, 5, 8, 13, 21] as est}
										<button
											onclick={() => { updateField('estimate', est); estimateOpen = false; }}
											class="flex w-full items-center rounded px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issue.estimate === est ? 'bg-[var(--color-bg-hover)]' : ''}"
										>
											{est}
										</button>
									{/each}
								</Popover.Content>
							</Popover.Root>
						</div>
					</div>
				{/if}
			</div>

			<!-- Labels card -->
			<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
				<button
					onclick={() => labelsExpanded = !labelsExpanded}
					class="flex w-full items-center gap-1.5 px-3 py-2.5 text-[11px] font-medium uppercase tracking-wider text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)] transition-colors"
				>
					<ChevronRight size={12} class="transition-transform {labelsExpanded ? 'rotate-90' : ''}" />
					Labels
				</button>
				{#if labelsExpanded}
					<div class="px-3 pb-3">
						<div class="flex flex-wrap items-center gap-1">
							{#if issue.labels && issue.labels.length > 0}
								{#each issue.labels as lbl}
									<button onclick={() => labelsOpen = true} class="flex items-center gap-1.5 rounded-full bg-[var(--color-bg-tertiary)] px-2.5 py-1 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] transition-colors cursor-pointer">
										<span class="h-2.5 w-2.5 rounded-full shrink-0" style="background-color: {lbl.color}"></span>
										{lbl.name}
									</button>
								{/each}
							{/if}
							<Popover.Root bind:open={labelsOpen}>
								<Popover.Trigger>
									{#if issue.labels && issue.labels.length > 0}
										<button class="flex h-6 w-6 items-center justify-center rounded-full hover:bg-[var(--color-bg-hover)] text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)] transition-colors">
											<Plus size={14} />
										</button>
									{:else}
										<button class="flex items-center gap-1.5 rounded-md px-2 py-1 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)] transition-colors">
											<Plus size={12} />
											Add label
										</button>
									{/if}
								</Popover.Trigger>
								<Popover.Content class="w-48 p-1" align="start">
									{#each labels as label}
										<button
											onclick={async () => {
												const currentIds = (issue.labels ?? []).map(l => l.id);
												const newIds = currentIds.includes(label.id)
													? currentIds.filter(id => id !== label.id)
													: [...currentIds, label.id];
												try {
													await issuesState.update(slug, issue.identifier, { label_ids: newIds });
													await refreshIssue();
												} catch { toast.error('Failed to update labels'); }
											}}
											class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
										>
											<Checkbox checked={(issue.labels ?? []).some(l => l.id === label.id)} />
											<div class="h-2.5 w-2.5 rounded-full shrink-0" style="background-color: {label.color}"></div>
											<span class="truncate">{label.name}</span>
										</button>
									{/each}
									{#if labels.length === 0}
										<p class="px-2 py-3 text-center text-xs text-[var(--color-text-tertiary)]">No labels</p>
									{/if}
								</Popover.Content>
							</Popover.Root>
						</div>
					</div>
				{/if}
			</div>

			<!-- Project card -->
			<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
				<button
					onclick={() => projectExpanded = !projectExpanded}
					class="flex w-full items-center gap-1.5 px-3 py-2.5 text-[11px] font-medium uppercase tracking-wider text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)] transition-colors"
				>
					<ChevronRight size={12} class="transition-transform {projectExpanded ? 'rotate-90' : ''}" />
					Project
				</button>
				{#if projectExpanded}
					<div class="px-3 pb-3">
						<Popover.Root bind:open={projectOpen}>
							<Popover.Trigger>
								{#if issueProject}
									<button class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] transition-colors w-full text-left">
										<FolderKanban size={14} class="text-[var(--color-text-tertiary)] shrink-0" />
										<span class="truncate">{issueProject.name}</span>
									</button>
								{:else}
									<button class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] transition-colors">
										<FolderKanban size={14} />
										Add project
									</button>
								{/if}
							</Popover.Trigger>
							<Popover.Content class="w-48 p-1" align="start">
								<button
									onclick={() => { updateField('project_id', ''); if (issue.cycle_id) updateField('cycle_id', ''); projectOpen = false; }}
									class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
								>
									No project
								</button>
								{#each projects as project}
									<button
										onclick={() => { updateField('project_id', project.id); projectOpen = false; }}
										class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issue.project_id === project.id ? 'bg-[var(--color-bg-hover)]' : ''}"
									>
										<FolderKanban size={14} class="text-[var(--color-text-tertiary)]" />
										{project.name}
									</button>
								{/each}
								{#if projects.length === 0}
									<p class="px-2 py-2 text-center text-xs text-[var(--color-text-tertiary)]">No projects</p>
								{/if}
							</Popover.Content>
						</Popover.Root>
						{#if issueProject?.description}
							<p class="mt-1 px-2 text-xs text-[var(--color-text-tertiary)] leading-relaxed">{issueProject.description}</p>
						{/if}

						<!-- Cycle as sub-item of project (only when project is selected) -->
						{#if issueProject}
						<div class="ml-3 flex">
							<svg class="shrink-0 mr-1" width="14" height="100%" viewBox="0 0 14 28" preserveAspectRatio="xMinYMin" fill="none">
								<path d="M1 0 L1 18 C1 23, 5 23, 9 23 L14 23" stroke="var(--color-text-tertiary)" stroke-width="1.5" opacity="0.4" fill="none"/>
							</svg>
							<div class="flex-1 min-w-0 mt-2.5">
								<Popover.Root bind:open={cycleOpen}>
									<Popover.Trigger>
										<button class="flex items-center gap-2 rounded-md px-2 py-1 text-xs hover:bg-[var(--color-bg-hover)] transition-colors {issueCycle ? 'text-[var(--color-text-primary)]' : 'text-[var(--color-text-tertiary)]'}">
											<RefreshCw size={12} class="shrink-0 text-[var(--color-text-tertiary)]" />
											{issueCycle ? issueCycle.name : 'No cycle'}
										</button>
									</Popover.Trigger>
									<Popover.Content class="w-48 p-1" align="start">
										<button
											onclick={() => { updateField('cycle_id', ''); cycleOpen = false; }}
											class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
										>
											No cycle
										</button>
										{#each cycles as cycle}
											<button
												onclick={() => { updateField('cycle_id', cycle.id); cycleOpen = false; }}
												class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issue.cycle_id === cycle.id ? 'bg-[var(--color-bg-hover)]' : ''}"
											>
												{cycle.name}
											</button>
										{/each}
										{#if cycles.length === 0}
											<p class="px-2 py-2 text-center text-xs text-[var(--color-text-tertiary)]">No cycles</p>
										{/if}
									</Popover.Content>
								</Popover.Root>
							</div>
						</div>
						{/if}
					</div>
				{/if}
			</div>
		</div>
	</div>
</div>
