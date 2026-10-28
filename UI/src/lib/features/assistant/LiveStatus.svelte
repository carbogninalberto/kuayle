<script lang="ts">
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { m } from '$lib/paraglide/messages.js';

	// The conversation's realtime state as a glowing dot; the popover explains
	// it. Colours come from the theme's status tokens.
	let {
		connected,
		busy = false,
		offline = false
	}: { connected: boolean; busy?: boolean; offline?: boolean } = $props();

	type State = 'responding' | 'live' | 'reconnecting' | 'offline';
	const current = $derived<State>(offline ? 'offline' : !connected ? 'reconnecting' : busy ? 'responding' : 'live');
	const color: Record<State, string> = {
		responding: 'var(--status-running)',
		live: 'var(--status-success)',
		reconnecting: 'var(--status-warning)',
		offline: 'var(--status-error)'
	};
	const text: Record<State, { title: () => string; body: () => string }> = {
		responding: { title: m['assistant.live_responding'], body: m['assistant.live_responding_body'] },
		live: { title: m['assistant.connected'], body: m['assistant.live_body'] },
		reconnecting: { title: m['assistant.reconnecting'], body: m['assistant.reconnecting_body'] },
		offline: { title: m['assistant.offline'], body: m['assistant.offline_body'] }
	};
</script>

<Popover.Root>
	<Popover.Trigger>
		{#snippet child({ props })}
			<Button
				{...props}
				variant="ghost"
				size="icon-sm"
				aria-label={text[current].title()}
				title={text[current].title()}
			>
				<span
					role="status"
					aria-label={text[current].title()}
					class="live-dot {current}"
					style="--dot: {color[current]}"
				></span>
			</Button>
		{/snippet}
	</Popover.Trigger>
	<Popover.Content align="end" class="w-64 space-y-1 text-sm">
		<p class="flex items-center gap-2 font-medium">
			<span class="live-dot {current}" style="--dot: {color[current]}" aria-hidden="true"></span>{text[current].title()}
		</p>
		<p class="text-xs text-muted-foreground">{text[current].body()}</p>
	</Popover.Content>
</Popover.Root>

<style>
	.live-dot {
		display: inline-block;
		width: 0.5rem;
		height: 0.5rem;
		flex-shrink: 0;
		border-radius: 9999px;
		background: var(--dot);
	}
	.live-dot.live,
	.live-dot.responding,
	.live-dot.reconnecting {
		animation: live-glow 2s ease-in-out infinite;
	}
	.live-dot.responding {
		animation-duration: 1s;
	}
	@keyframes live-glow {
		0%,
		100% {
			box-shadow: 0 0 0 0 color-mix(in oklab, var(--dot) 55%, transparent);
		}
		50% {
			box-shadow: 0 0 0 5px color-mix(in oklab, var(--dot) 0%, transparent);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.live-dot {
			animation: none !important;
		}
	}
</style>
