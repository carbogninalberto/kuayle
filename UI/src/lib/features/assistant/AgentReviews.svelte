<script lang="ts">
	import { ExternalLink } from '@lucide/svelte';
	import * as Item from '$lib/components/ui/item/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import Markdown from './Markdown.svelte';
	import { reviewTone, type ReviewTone } from './agent-review';
	import { toneBadge } from './task-tone';
	import type { AgentReview, AgentSession } from '$lib/api/agent-tasks';
	import { m } from '$lib/paraglide/messages.js';
	// Requested agent reviews of the task's published pull request. Findings are
	// the reviewer's own text: rendered as sanitized Markdown, never trusted.
	let { reviews }: { reviews: (AgentSession & { review: AgentReview })[] } = $props();
	const badge: Record<ReviewTone, string> = {
		success: toneBadge.success,
		error: toneBadge.error,
		warning: toneBadge.warning,
		running: toneBadge.running,
		neutral: toneBadge.neutral
	};
	function status(session: AgentSession & { review: AgentReview }) {
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
</script>

{#if reviews.length}<section aria-label={m['tasks.reviews']()} class="min-w-0 space-y-2">
		{#each reviews as session (session.id)}
			{@const review = session.review}
			{@const note = comment[review.comment_state]}
			<Item.Root
				variant="outline"
				size="sm"
				class="items-start"
				aria-label={m['tasks.review_by']({ agent: agentInfo(session.provider).name })}
			>
				<Item.Media><AgentLogo agent={session.provider} /></Item.Media>
				<Item.Content class="min-w-0">
					<div class="flex flex-wrap items-center justify-between gap-2">
						<Item.Title>{m['tasks.review_by']({ agent: agentInfo(session.provider).name })}</Item.Title>
						<Badge variant="outline" class="shrink-0 {badge[reviewTone(session)]}" role="status"
							>{status(session)}</Badge
						>
					</div>
					<Item.Description class="line-clamp-none"
						>{m['tasks.review_commit']({
							commit: review.commit_sha.slice(0, 7),
							number: review.pull_request_number
						})}</Item.Description
					>
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
				</Item.Content>
			</Item.Root>
		{/each}
	</section>{/if}
