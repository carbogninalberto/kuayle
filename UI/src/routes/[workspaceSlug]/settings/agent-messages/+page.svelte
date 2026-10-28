<script lang="ts">
	import { page } from '$app/state';
	import { untrack } from 'svelte';
	import { MessagesSquare, RefreshCw, CirclePlay, X } from '@lucide/svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { agentMessages, type AgentMessage, type AgentMessageHold } from '$lib/api/agent-messages';
	import AgentMessageRow from '$lib/features/agent-messages/AgentMessageRow.svelte';
	import {
		messageTone,
		summarize,
		type MessageAction,
		type MessageFilters
	} from '$lib/features/agent-messages/agent-messages';
	import { moderate, stateLabel } from '$lib/features/agent-messages/labels';
	import { toneBadge } from '$lib/features/assistant/task-tone';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';

	// The navigator: every message agents of related tasks sent each other,
	// with filters, held conversations and the moderators' controls.
	const slug = $derived(page.params.workspaceSlug ?? '');
	const params = untrack(() => page.url.searchParams);
	let issue = $state(params.get('issue') ?? ''),
		stateFilter = $state<string>(params.get('state') || 'all'),
		channel = $state<string>(params.get('channel') || 'all');
	// A task card links here for its own issue.
	let issueId = $state(params.get('issue_id') ?? ''),
		taskId = $state(params.get('task_id') ?? '');
	let messages = $state<AgentMessage[]>([]),
		holds = $state<AgentMessageHold[]>([]),
		nextBefore = $state<string | undefined>(),
		loading = $state(true),
		failed = $state(false),
		busy = $state<string | null>(null);
	const counts = $derived(summarize(messages));
	const filters = $derived<MessageFilters>({
		issue,
		issue_id: issueId,
		task_id: taskId,
		state: stateFilter === 'all' ? '' : (stateFilter as MessageFilters['state']),
		channel: channel === 'all' ? '' : (channel as MessageFilters['channel'])
	});
	const filtered = $derived(!!(issue.trim() || issueId || taskId || stateFilter !== 'all' || channel !== 'all'));
	const stateOptions = [
		{ value: 'all', label: m['agent_messages.all_states']() },
		...(['queued', 'held', 'delivered', 'dropped', 'rejected'] as const).map((value) => ({
			value,
			label: stateLabel(value)
		}))
	];
	const channelOptions = [
		{ value: 'all', label: m['agent_messages.all_channels']() },
		{ value: 'internal', label: m['agent_messages.channel_internal_short']() },
		{ value: 'comment', label: m['agent_messages.channel_comment_short']() }
	];

	async function load(more = false) {
		if (!more) loading = true;
		failed = false;
		try {
			const [page, held] = await Promise.all([
				agentMessages.list(slug, { ...filters, before: more ? nextBefore : undefined }),
				more ? Promise.resolve({ holds }) : agentMessages.holds(slug)
			]);
			const loaded = page?.messages ?? [];
			messages = more ? [...messages, ...loaded] : loaded;
			nextBefore = page?.next_before;
			holds = held?.holds ?? [];
		} catch (error) {
			failed = true;
			appToast.apiError(error, m['agent_messages.failed_load']());
		} finally {
			loading = false;
		}
	}
	// Typing an identifier reloads once it looks complete or is cleared.
	$effect(() => {
		const current = filters;
		if (current.issue && !/^[A-Za-z][A-Za-z0-9]*-\d+$/.test(current.issue.trim())) return;
		untrack(() => void load());
	});

	async function act(message: AgentMessage, action: MessageAction) {
		busy = message.id;
		try {
			await moderate(slug, message, action);
			appToast.success(m['agent_messages.updated']());
			await load();
		} catch (error) {
			appToast.apiError(error, m['agent_messages.action_failed']());
		} finally {
			busy = null;
		}
	}

	async function releaseHold(hold: AgentMessageHold) {
		busy = hold.id;
		try {
			await agentMessages.releaseConversation(slug, hold.id);
			await load();
		} catch (error) {
			appToast.apiError(error, m['agent_messages.action_failed']());
		} finally {
			busy = null;
		}
	}

	function clearFilters() {
		issue = '';
		issueId = '';
		taskId = '';
		stateFilter = 'all';
		channel = 'all';
	}
</script>

<div class="mx-auto max-w-3xl space-y-4 p-4 sm:p-6">
	<div class="flex items-start gap-3">
		<MessagesSquare class="mt-1 shrink-0 text-muted-foreground" size={18} aria-hidden="true" />
		<div class="min-w-0 flex-1">
			<h1 class="text-lg font-semibold">{m['agent_messages.title']()}</h1>
			<p class="mt-1 text-sm text-muted-foreground">{m['agent_messages.description']()}</p>
		</div>
		<Button variant="ghost" size="icon-sm" aria-label={m['agent_messages.refresh']()} onclick={() => void load()}
			><RefreshCw /></Button
		>
	</div>

	{#if holds.length}<section aria-label={m['agent_messages.held_conversations']()} class="space-y-1.5">
			<h2 class="text-xs font-medium text-muted-foreground">{m['agent_messages.held_conversations']()}</h2>
			<ul class="flex flex-wrap gap-2">
				{#each holds as hold (hold.id)}<li
						class="flex items-center gap-2 rounded-md border border-[var(--status-warning)]/40 bg-[var(--status-warning)]/5 py-1 pr-1 pl-2 text-sm"
					>
						<span class="font-medium">{m['agent_messages.conversation']({ a: hold.issue_a, b: hold.issue_b })}</span>
						<span class="text-xs text-muted-foreground"
							>{m['agent_messages.held_count']({ count: hold.held_messages })}</span
						>
						{#if hold.can_moderate}<Button
								variant="ghost"
								size="sm"
								class="h-7"
								disabled={busy === hold.id}
								onclick={() => void releaseHold(hold)}><CirclePlay />{m['agent_messages.release']()}</Button
							>{/if}
					</li>{/each}
			</ul>
		</section>{/if}

	<div class="flex flex-wrap items-center gap-2" role="search" aria-label={m['agent_messages.filters']()}>
		<Input
			class="h-8 w-full sm:w-36"
			bind:value={issue}
			placeholder={m['agent_messages.filter_issue_placeholder']()}
			aria-label={m['agent_messages.filter_issue']()}
		/>
		<SelectField
			bind:value={stateFilter}
			options={stateOptions}
			aria-label={m['agent_messages.filter_state']()}
			size="sm"
		/>
		<SelectField
			bind:value={channel}
			options={channelOptions}
			aria-label={m['agent_messages.filter_channel']()}
			size="sm"
		/>
		{#if filtered}<Button variant="ghost" size="sm" onclick={clearFilters}
				><X />{m['agent_messages.clear_filters']()}</Button
			>{/if}
		<div class="flex flex-wrap gap-1 sm:ml-auto">
			{#each Object.entries(counts) as [key, count] (key)}<Badge
					variant="outline"
					class={toneBadge[messageTone(key as AgentMessage['state'])]}
					>{count} · {stateLabel(key as AgentMessage['state'])}</Badge
				>{/each}
		</div>
	</div>

	{#if loading}<LoadingState />
	{:else if failed}<ErrorState message={m['agent_messages.failed_load']()} onretry={() => void load()} />
	{:else if !messages.length}<p class="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">
			{filtered ? m['agent_messages.empty_filtered']() : m['agent_messages.empty']()}
		</p>
	{:else}
		<ul class="space-y-2" aria-label={m['agent_messages.title']()}>
			{#each messages as message (message.id)}<AgentMessageRow
					{message}
					{slug}
					busy={busy === message.id}
					onaction={act}
				/>{/each}
		</ul>
		{#if nextBefore}<Button variant="outline" size="sm" class="w-full" onclick={() => void load(true)}
				>{m['agent_messages.load_more']()}</Button
			>{/if}
	{/if}
</div>
