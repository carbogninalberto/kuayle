<script lang="ts">
	import MockFrame from './MockFrame.svelte';

	const items = [
		{ key: 'ENG-231', title: 'Export button missing on saved views', from: 'Dana', when: '10m' },
		{ key: 'ENG-230', title: 'Mention notifications arrive twice', from: 'Sam', when: '1h' },
		{ key: 'ENG-229', title: 'Support Markdown tables in comments', from: 'Lena', when: '3h' }
	];
</script>

<MockFrame label="Illustration of the Kuayle triage inbox with three incoming issues, accept and decline actions and a mention in a threaded comment." path="engineering / triage">
	<div class="space-y-2 p-4 text-left text-xs">
		{#each items as item, i}
			<div class="flex items-center justify-between gap-3 rounded-md border border-white/10 px-3 py-2 {i === 0 ? 'bg-brand-400/10' : 'bg-white/[0.02]'}">
				<div class="min-w-0">
					<p class="font-mono text-[0.6rem] text-muted-foreground">{item.key} · from {item.from} · {item.when}</p>
					<p class="truncate text-foreground">{item.title}</p>
				</div>
				{#if i === 0}
					<span class="flex shrink-0 gap-1">
						<span class="rounded border border-emerald-400/30 px-1.5 py-0.5 text-[0.6rem] text-emerald-200">Accept</span>
						<span class="rounded border border-white/15 px-1.5 py-0.5 text-[0.6rem] text-muted-foreground">Decline</span>
					</span>
				{/if}
			</div>
		{/each}
		<div class="mt-3 rounded-md border border-dashed border-white/15 p-3">
			<p class="text-[0.65rem] text-muted-foreground">Comment thread · ENG-231</p>
			<p class="mt-1 text-foreground"><span class="text-brand-200">@Mira</span> can this ship in Cycle 14?</p>
			<p class="mt-1 pl-3 text-muted-foreground">↳ Yes, adding it now. <span class="text-emerald-300">Resolved</span></p>
		</div>
	</div>
</MockFrame>
