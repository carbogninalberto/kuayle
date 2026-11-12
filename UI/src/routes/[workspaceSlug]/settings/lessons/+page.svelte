<script lang="ts">
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { ArrowDown, ArrowUp, Archive, Check, Ellipsis, Pencil, Plus, X } from '@lucide/svelte';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import ConfirmDialog from '$lib/components/shared/ConfirmDialog.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import { agentTasks } from '$lib/api/agent-tasks';
	import { getGitHubStatus } from '$lib/api/github';
	import { listMembers } from '$lib/api/members';
	import { memory, type Lesson, type LessonList } from '$lib/api/memory';
	import LessonDialog from '$lib/features/memory/LessonDialog.svelte';
	import { lessonBoard, lessonOrigin, movedLesson, wasEdited } from '$lib/features/memory/lessons';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';

	// Settings → Lessons: the workspace memory. Proposals wait in one place,
	// apart from the lessons agents read; a lesson is only in a repository's
	// numbered list once an owner or admin approved it. Members read the
	// approved lessons and their own proposals and can write one; the server
	// decides who sees and who decides.
	const slug = $derived(page.params.workspaceSlug ?? '');

	let lessons = $state<Lesson[]>([]),
		canManage = $state(false),
		limits = $state<LessonList['limits'] | null>(null),
		connected = $state<string[]>([]),
		names = $state<Record<string, string>>({}),
		// Where a proposal's task opens, for the tasks the reader can open.
		taskLinks = $state<Record<string, string>>({}),
		loading = $state(true),
		failed = $state(false),
		busy = $state(false);
	let editing = $state<Lesson | 'new' | null>(null),
		confirming = $state<Lesson | null>(null);
	const board = $derived(lessonBoard(lessons, connected));

	async function load() {
		failed = false;
		try {
			const [proposed, approved] = await Promise.all([memory.list(slug, 'proposed'), memory.list(slug, 'approved')]);
			lessons = [...(proposed.lessons ?? []), ...(approved.lessons ?? [])];
			canManage = approved.can_manage;
			limits = approved.limits;
			void linkTasks();
		} catch {
			failed = true;
		} finally {
			loading = false;
		}
	}
	// A proposal links to its task for whoever can open it (its creator);
	// for anyone else it says where it came from without a link.
	async function linkTasks() {
		const ids = [...new Set(board.proposals.map((lesson) => lesson.source_task_id).filter((id): id is string => !!id))];
		for (const id of ids.filter((id) => !(id in taskLinks)).slice(0, 20)) {
			try {
				const task = await agentTasks.get(slug, id);
				if (task.conversation_id)
					taskLinks[id] =
						`/${slug}/assistant?conversation=${encodeURIComponent(task.conversation_id)}&agent=${encodeURIComponent(id)}`;
			} catch {
				taskLinks[id] = '';
			}
		}
	}
	onMount(() => {
		void load();
		getGitHubStatus(slug)
			.then((status) => (connected = (status.repos ?? []).map((repo) => repo.full_name)))
			.catch(() => {});
		listMembers(slug)
			.then((members) => (names = Object.fromEntries(members.map((member) => [member.user_id, member.name]))))
			.catch(() => {});
	});

	async function act(action: () => Promise<unknown>) {
		if (busy) return;
		busy = true;
		try {
			await action();
			editing = null;
			confirming = null;
			await load();
		} catch (error) {
			appToast.apiError(error, m['lessons.failed']());
		} finally {
			busy = false;
		}
	}
	function submit(text: string, repository: string) {
		const lesson = editing;
		if (lesson === 'new') return act(() => memory.add(slug, repository, text));
		if (!lesson) return;
		return act(() =>
			lesson.state === 'proposed' ? memory.approve(slug, lesson.id, text) : memory.edit(slug, lesson.id, text)
		);
	}
	function move(repository: string, ids: string[], id: string, delta: -1 | 1) {
		const next = movedLesson(ids, id, delta);
		if (next) void act(() => memory.reorder(slug, repository, next));
	}
	const origin = (lesson: Lesson) => {
		const kind = lessonOrigin(lesson);
		if (kind === 'agent') return m['lessons.by_agent']();
		if (kind === 'github') return m['lessons.from_github']();
		return m['lessons.by_person']({ name: names[lesson.proposed_by ?? ''] || m['common.unknown']() });
	};
</script>

{#if loading}<LoadingState />
{:else if failed}<ErrorState message={m['lessons.failed_load']()} onretry={load} />
{:else}
	<div class="mx-auto max-w-3xl space-y-6 p-4 sm:p-6">
		<div class="flex items-center justify-between gap-3">
			<h1 class="text-lg font-semibold">{m['lessons.title']()}</h1>
			{#if board.choices.length}<Button size="sm" onclick={() => (editing = 'new')} data-testid="lesson-add"
					><Plus />{m['lessons.add']()}</Button
				>{/if}
		</div>

		{#if board.proposals.length}<section aria-labelledby="lessons-proposals" class="space-y-2">
				<h2 id="lessons-proposals" class="flex items-center gap-2 text-sm font-medium">
					{m['lessons.proposals']()}<Badge variant="secondary" class="h-5 px-1.5 tabular-nums"
						>{board.proposals.length}</Badge
					>
				</h2>
				<ul class="divide-y rounded-lg border border-dashed" data-testid="lesson-proposals">
					{#each board.proposals as lesson (lesson.id)}
						{@const link = lesson.source_task_id ? taskLinks[lesson.source_task_id] : ''}
						<li class="flex min-w-0 items-start gap-2 px-3 py-2.5 sm:gap-3">
							<div class="min-w-0 flex-1">
								<p class="text-sm break-words">{lesson.text}</p>
								<p class="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-xs text-muted-foreground">
									<span class="font-mono break-all">{lesson.repository}</span><span aria-hidden="true">·</span>
									{#if link}<a href={link} class="rounded-sm underline underline-offset-4 outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
											>{origin(lesson)}</a
										>{:else}<span>{origin(lesson)}</span>{/if}
									{#if wasEdited(lesson)}<span aria-hidden="true">·</span>
										<Popover.Root>
											<Popover.Trigger
												class="cursor-pointer rounded-sm underline decoration-dotted underline-offset-4 outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
												>{m['lessons.edited']()}</Popover.Trigger
											>
											<Popover.Content align="start" class="w-80 text-sm break-words">{lesson.proposed_text}</Popover.Content>
										</Popover.Root>{/if}
								</p>
							</div>
							{#if canManage}<div class="-my-0.5 -mr-1 flex shrink-0 items-center">
									<Button
										variant="ghost"
										size="icon-sm"
										disabled={busy}
										aria-label={m['workflows.task.approve']()}
										title={m['workflows.task.approve']()}
										onclick={() => void act(() => memory.approve(slug, lesson.id))}><Check /></Button
									>
									<Button
										variant="ghost"
										size="icon-sm"
										disabled={busy}
										aria-label={m['common.edit']()}
										title={m['common.edit']()}
										onclick={() => (editing = lesson)}><Pencil /></Button
									>
									<Button
										variant="ghost"
										size="icon-sm"
										disabled={busy}
										aria-label={m['agent_messages.drop']()}
										title={m['agent_messages.drop']()}
										onclick={() => (confirming = lesson)}><X /></Button
									>
								</div>{/if}
						</li>
					{/each}
				</ul>
			</section>{/if}

		{#each board.repositories as group (group.repository)}
			{@const ids = group.lessons.map((lesson) => lesson.id)}
			<section aria-label={group.repository} class="space-y-2">
				<h2 class="font-mono text-sm font-medium break-all">{group.repository}</h2>
				<ol class="divide-y rounded-lg border" data-testid="lesson-list">
					{#each group.lessons as lesson, index (lesson.id)}
						<li class="flex min-w-0 items-start gap-3 px-3 py-2.5">
							<span class="mt-0.5 w-4 shrink-0 text-right text-xs text-muted-foreground tabular-nums" aria-hidden="true"
								>{index + 1}</span
							>
							<p class="min-w-0 flex-1 text-sm break-words">{lesson.text}</p>
							{#if canManage}<DropdownMenu.Root>
									<DropdownMenu.Trigger>
										{#snippet child({ props })}<Button
												{...props}
												variant="ghost"
												size="icon-sm"
												class="-my-0.5 shrink-0 text-muted-foreground"
												disabled={busy}
												aria-label={m['tasks.more_actions']()}><Ellipsis /></Button
											>{/snippet}
									</DropdownMenu.Trigger>
									<DropdownMenu.Content align="end" class="w-44">
										<DropdownMenu.Item onclick={() => (editing = lesson)}><Pencil />{m['common.edit']()}</DropdownMenu.Item>
										<DropdownMenu.Item disabled={index === 0} onclick={() => move(group.repository, ids, lesson.id, -1)}
											><ArrowUp />{m['wiki.tree.move_up']()}</DropdownMenu.Item
										>
										<DropdownMenu.Item
											disabled={index === ids.length - 1}
											onclick={() => move(group.repository, ids, lesson.id, 1)}
											><ArrowDown />{m['wiki.tree.move_down']()}</DropdownMenu.Item
										>
										<DropdownMenu.Separator />
										<DropdownMenu.Item onclick={() => (confirming = lesson)}><Archive />{m['lessons.retire']()}</DropdownMenu.Item>
									</DropdownMenu.Content>
								</DropdownMenu.Root>{/if}
						</li>
					{/each}
				</ol>
			</section>
		{/each}

		{#if !board.proposals.length && !board.repositories.length}<p class="py-16 text-center text-sm text-muted-foreground">
				{m['lessons.empty']()}
			</p>{/if}
	</div>
{/if}

{#if editing && limits}<LessonDialog
		lesson={editing === 'new' ? undefined : editing}
		repositories={board.choices}
		maxLength={limits.lesson_characters}
		{busy}
		onsubmit={submit}
		onclose={() => (editing = null)}
	/>{/if}

{#if confirming}
	{@const lesson = confirming}
	<ConfirmDialog
		title={lesson.state === 'proposed' ? m['lessons.drop_confirm']() : m['lessons.retire_confirm']()}
		message={lesson.text}
		confirmLabel={lesson.state === 'proposed' ? m['agent_messages.drop']() : m['lessons.retire']()}
		onconfirm={() =>
			void act(() => (lesson.state === 'proposed' ? memory.drop(slug, lesson.id) : memory.retire(slug, lesson.id)))}
		oncancel={() => (confirming = null)}
	/>
{/if}

<PageTitle parts={[m['lessons.title'](), m['settings.title']()]} />
