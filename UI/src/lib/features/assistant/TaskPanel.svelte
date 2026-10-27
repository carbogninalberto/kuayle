<script lang="ts">
	import { onMount } from 'svelte';
	import { Code, RefreshCw } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { agentTasks, type AgentTask, type TaskOptions } from '$lib/api/agent-tasks';
	import type { ConversationState } from './conversation-state.svelte';
	import TaskComposer from './TaskComposer.svelte';
	import TaskCard from './TaskCard.svelte';
	import TaskActions from './TaskActions.svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { fly } from 'svelte/transition';
	import { prefersReducedMotion } from 'svelte/motion';
	import { m } from '$lib/paraglide/messages.js';
	import { useTerminalDock } from '$lib/features/dev-machines/terminal-dock-context.svelte';
	let { chat }: { chat: ConversationState } = $props();
	const dock = useTerminalDock();
	let options = $state<TaskOptions | null>(null),
		loading = $state(true),
		failed = $state(false),
		denied = $state(false),
		optionsUnavailable = $state(false),
		open = $state(false),
		retry = $state<AgentTask | undefined>();
	let before = $state<string | undefined>(),
		loadingOlder = $state(false);
	let disposed = false;
	// Once the task list scrolls away, the newest task stays reachable as a
	// compact header pinned to the top of the chat.
	let panel = $state<HTMLElement>();
	let scrolledAway = $state(false);
	$effect(() => {
		if (!panel) return;
		const observer = new IntersectionObserver(([entry]) => (scrolledAway = !entry.isIntersecting));
		observer.observe(panel);
		return () => observer.disconnect();
	});
	function showPanel() {
		panel?.scrollIntoView({ behavior: prefersReducedMotion.current ? 'auto' : 'smooth', block: 'start' });
	}
	const tasks = $derived(
		Object.values(chat.liveTasks).sort(
			(a, b) => Date.parse(b.created_at) - Date.parse(a.created_at) || b.id.localeCompare(a.id)
		)
	);
	// Seeing agent work turns on the dock's Agents tab for this workspace.
	$effect(() => {
		if (tasks.length) dock?.trackAgents();
	});
	function fail(error: unknown) {
		if (disposed) return;
		const code = (error as { status?: number }).status;
		if ([401, 403, 404].includes(code ?? 0)) {
			denied = true;
			chat.liveTasks = {};
			chat.taskActivity = {};
			open = false;
			retry = undefined;
		} else failed = true;
	}
	async function load() {
		loading = true;
		failed = false;
		try {
			const result = await agentTasks.list(chat.slug, chat.id);
			if (disposed || chat.unavailable) return;
			for (const task of result.tasks) chat.acceptTask(task);
			before = result.next_before;
		} catch (e) {
			fail(e);
		} finally {
			if (!disposed) loading = false;
		}
	}
	onMount(() => {
		void load();
		void agentTasks
			.options(chat.slug, chat.id)
			.then((value) => {
				if (!disposed) options = value;
			})
			.catch(() => {
				if (!disposed) optionsUnavailable = true;
			});
		return () => {
			disposed = true;
		};
	});
	async function older() {
		if (!before || loadingOlder) return;
		loadingOlder = true;
		try {
			const page = await agentTasks.list(chat.slug, chat.id, before);
			if (disposed || denied || chat.unavailable) return;
			for (const task of page.tasks) chat.acceptTask(task);
			before = page.next_before;
		} catch (e) {
			fail(e);
		} finally {
			if (!disposed) loadingOlder = false;
		}
	}
</script>

<section bind:this={panel} aria-label={m['tasks.title']()} class="min-w-0 scroll-mt-3 space-y-1">
	<div class="flex items-center justify-between gap-3">
		<h2 class="text-sm font-medium">{m['tasks.title']()}</h2>
		{#if !denied}<Button
				size="sm"
				variant="outline"
				disabled={!options?.providers.length || loading}
				onclick={() => {
					retry = undefined;
					open = true;
				}}><Code size={14} />{m['tasks.start']()}</Button
			>{/if}
	</div>
	{#if denied}<p class="text-sm text-muted-foreground" role="status">{m['tasks.unavailable']()}</p>
	{:else}
		{#if loading}<p class="text-xs text-muted-foreground">{m['assistant.loading']()}</p>{/if}
		{#if failed}<p role="alert" class="text-sm text-destructive">{m['tasks.load_error']()}</p>
			<Button variant="ghost" size="sm" onclick={() => void load()}
				><RefreshCw size={14} />{m['tasks.refresh']()}</Button
			>{/if}
		{#if optionsUnavailable}<p class="text-xs text-muted-foreground">{m['tasks.configuration_unavailable']()}</p>{/if}
		<div class="divide-y divide-border">
			{#each tasks as task, i (task.id)}<TaskCard
					{task}
					{chat}
					pinned={i === 0}
					onretry={(value) => {
						retry = value;
						open = true;
					}}
					ondenied={fail}
				/>{/each}
		</div>
		{#if before}<Button variant="ghost" size="sm" disabled={loadingOlder} onclick={() => void older()}
				>{m['tasks.older']()}</Button
			>{/if}
	{/if}
</section>
{#if scrolledAway && chat.taskBar && !denied}
	<!-- Zero-height sticky anchor: the header floats over the messages without
	     shifting them; the negative margin cancels the content gap. -->
	<div class="sticky top-0 z-20 -mt-8 h-0">
		<div
			role="region"
			aria-label={m['tasks.task']()}
			transition:fly={{ y: -8, duration: prefersReducedMotion.current ? 0 : 180 }}
			class="absolute inset-x-0 top-2 flex min-w-0 items-center gap-2 rounded-xl border border-border/60 bg-background/60 py-1 pr-1.5 pl-1.5 shadow-lg shadow-black/5 ring-1 ring-white/10 backdrop-blur-xl backdrop-saturate-150 dark:shadow-black/30"
		>
			<Button
				variant="ghost"
				size="sm"
				class="h-7 min-w-0 flex-1 cursor-pointer justify-start px-1.5 text-xs font-normal"
				title={m['tasks.show_task']()}
				onclick={showPanel}
				><span class="truncate"
					>{#if chat.taskBar.identifier}<span class="font-medium">{chat.taskBar.identifier}</span><span
							aria-hidden="true"
							class="mx-1 text-muted-foreground">·</span
						>{/if}{chat.taskBar.title}</span
				></Button
			>
			<TaskActions actions={chat.taskBar.actions} compact />
			<Badge variant="outline" class="shrink-0 {chat.taskBar.badgeClass}">{chat.taskBar.status}</Badge>
		</div>
	</div>
{/if}
{#if open && !denied && !chat.unavailable}<TaskComposer
		slug={chat.slug}
		conversation={chat.id}
		{options}
		task={retry}
		initialPrompt={chat.issue?.title ?? ''}
		onaccepted={(task) => chat.acceptTask(task)}
		onclose={() => {
			open = false;
			retry = undefined;
		}}
	/>{/if}
