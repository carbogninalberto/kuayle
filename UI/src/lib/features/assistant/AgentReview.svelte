<script lang="ts">
	import { ChevronRight, ExternalLink } from '@lucide/svelte';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import Markdown from './Markdown.svelte';
	import { reviewTone, type ReviewSession } from './agent-review';
	import { toneBadge } from './task-tone';
	import type { AgentReview } from '$lib/api/agent-tasks';
	import { m } from '$lib/features/i18n/messages';
	let { session, embedded = false }: { session: ReviewSession; embedded?: boolean } = $props();
	let open = $state(false);
	const review = $derived(session.review);
	const title = $derived(m['tasks.review_by']({ agent: agentInfo(session.provider).name }));
	function status(session: ReviewSession) {
		if (session.state === 'completed') {
			switch (session.review.verdict) {
				case 'approve':
					return m['tasks.review_verdict_approve']();
				case 'request_changes':
					return m['tasks.review_verdict_request_changes']();
				case 'comment':
					return m['tasks.review_verdict_comment']();
			}
			return m['tasks.review_verdict_unknown']();
		}
		if (session.state === 'failed' || session.state === 'cancelled') return m['tasks.review_failed']();
		return m['tasks.review_running']();
	}
	const comment: Record<AgentReview['comment_state'], (() => string) | undefined> = {
		not_requested: undefined,
		pending: m['tasks.review_comment_pending'],
		posted: m['tasks.review_comment_posted'],
		failed: m['tasks.review_comment_failed']
	};
	const note = $derived(comment[review.comment_state]);
</script>

{#snippet verdict()}
	<Badge variant="outline" class="shrink-0 {toneBadge[reviewTone(session)]}" role="status">{status(session)}</Badge>
{/snippet}

{#snippet details()}
	<p class="text-xs text-muted-foreground">{m['tasks.review_commit']({
		commit: review.commit_sha.slice(0, 7),
		number: review.pull_request_number
	})}</p>
	{#if review.focus}<p class="text-xs whitespace-pre-wrap break-words text-muted-foreground">
		{m['tasks.review_focus']({ focus: review.focus })}
	</p>{/if}
	{#if review.findings}<Markdown source={review.findings} class="text-sm" />{/if}
	{#if note}<p class="text-xs text-muted-foreground">
		{note()}{#if review.comment_state === 'posted'}
			<a
				class="ml-1 inline-flex items-center gap-1 underline underline-offset-2"
				href={review.pull_request_url}
				target="_blank"
				rel="noopener noreferrer"><ExternalLink size={12} />{m['tasks.open_pull_request']()}</a
			>{/if}
	</p>{/if}
{/snippet}

{#if embedded}
	<div class="min-w-0 space-y-2">
		{@render verdict()}
		{@render details()}
	</div>
{:else}
	<section aria-label={title} class="min-w-0">
		<Collapsible.Root bind:open>
			<Collapsible.Trigger
				class="group flex w-full min-w-0 cursor-pointer items-center gap-2.5 rounded-md py-1.5 text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
			>
				<AgentLogo agent={session.provider} size="sm" />
				<span class="min-w-0 flex-1 truncate text-sm font-medium">{title}</span>
				{@render verdict()}
				<ChevronRight size={14} aria-hidden="true" class="shrink-0 text-muted-foreground/60 transition-transform group-aria-expanded:rotate-90 motion-reduce:transition-none" />
			</Collapsible.Trigger>
			<Collapsible.Content>
				{#if open}<div class="ml-2.5 flex min-w-0 flex-col gap-2 border-l py-3 pl-4 sm:pl-5">
					{@render details()}
				</div>{/if}
			</Collapsible.Content>
		</Collapsible.Root>
	</section>
{/if}
