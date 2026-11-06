<script lang="ts">
	import MockFrame from './MockFrame.svelte';

	type Card = { key: string; title: string; labels: string[]; people: string[]; priority?: 'urgent' | 'high' };
	const columns: { name: string; dot: string; cards: Card[] }[] = [
		{
			name: 'Todo',
			dot: 'bg-zinc-400',
			cards: [
				{ key: 'ENG-214', title: 'Rate limit the public API per token', labels: ['api'], people: ['MV', 'SI'], priority: 'high' },
				{ key: 'ENG-219', title: 'Webhook retries with backoff', labels: ['webhooks'], people: ['DO'] }
			]
		},
		{
			name: 'In Progress',
			dot: 'bg-amber-300',
			cards: [
				{ key: 'ENG-207', title: 'Cycle carry-over keeps sub-issues', labels: ['cycles'], people: ['SI', 'LP', 'MV'], priority: 'urgent' },
				{ key: 'ENG-211', title: 'Private team for the security review', labels: ['access'], people: ['DO'] }
			]
		},
		{
			name: 'In Review',
			dot: 'bg-brand-300',
			cards: [{ key: 'ENG-198', title: 'Gantt shows project target dates', labels: ['projects'], people: ['LP', 'MV'] }]
		},
		{
			name: 'Done',
			dot: 'bg-emerald-400',
			cards: [
				{ key: 'ENG-190', title: 'PR merge moves issue to Done', labels: ['github'], people: ['SI'] },
				{ key: 'ENG-186', title: 'Version history for descriptions', labels: ['issues'], people: ['MV'] }
			]
		}
	];

	const results = [
		{ label: 'ENG-207 Cycle carry-over keeps sub-issues', hint: 'Issue' },
		{ label: 'ENG-163 Carry over unfinished issues', hint: 'Issue' },
		{ label: 'Cycle 14', hint: 'Go to' },
		{ label: 'Insights', hint: 'Go to' }
	];
</script>

<div class="relative">
	<MockFrame
		label="Illustration of a Kuayle team board with issues in Todo, In Progress, In Review and Done, several with multiple assignees, and the command palette searching issues."
		path="kuayle.example.com / engineering / board"
	>
		<div class="flex text-left">
			<!-- Sidebar -->
			<div class="hidden w-40 shrink-0 border-r border-white/10 p-3 text-[0.7rem] text-muted-foreground md:block">
				<p class="font-medium text-foreground">Engineering</p>
				<ul class="mt-3 space-y-1.5">
					<li class="rounded bg-white/5 px-2 py-1 text-foreground">Board</li>
					<li class="px-2">Triage <span class="ml-1 font-mono text-brand-300">3</span></li>
					<li class="px-2">Cycle 14</li>
					<li class="px-2">Projects</li>
					<li class="px-2">Views</li>
					<li class="px-2">Insights</li>
				</ul>
				<p class="mt-4 font-medium text-foreground">Security <span class="ml-1 rounded border border-white/15 px-1 text-[0.6rem]">private</span></p>
			</div>
			<!-- Columns -->
			<div class="grid min-w-0 flex-1 grid-cols-2 gap-2 p-3 sm:grid-cols-4">
				{#each columns as column, i}
					<div class="min-w-0 {i > 1 ? 'hidden sm:block' : ''}">
						<p class="flex items-center gap-1.5 px-1 text-[0.7rem] font-medium text-muted-foreground">
							<span class="size-1.5 rounded-full {column.dot}"></span>{column.name}
							<span class="font-mono">{column.cards.length}</span>
						</p>
						<div class="mt-2 space-y-2">
							{#each column.cards as card}
								<div class="rounded-md border border-white/10 bg-white/[0.03] p-2">
									<p class="flex items-center justify-between font-mono text-[0.6rem] text-muted-foreground">
										{card.key}
										{#if card.priority}<span class={card.priority === 'urgent' ? 'text-red-300' : 'text-amber-200'}>{card.priority === 'urgent' ? '!!!' : '!!'}</span>{/if}
									</p>
									<p class="mt-1 line-clamp-2 text-[0.7rem] leading-snug text-foreground">{card.title}</p>
									<div class="mt-2 flex items-center justify-between">
										<span class="rounded border border-white/10 px-1 text-[0.6rem] text-muted-foreground">{card.labels[0]}</span>
										<span class="flex -space-x-1.5">
											{#each card.people as person}
												<span class="grid size-5 place-items-center rounded-full bg-brand-500 text-[0.5rem] leading-none font-semibold text-white ring-1 ring-[#101015]">{person}</span>
											{/each}
										</span>
									</div>
								</div>
							{/each}
						</div>
					</div>
				{/each}
			</div>
		</div>
	</MockFrame>

	<!-- Command palette -->
	<div
		class="absolute right-3 -bottom-8 w-60 rounded-lg border border-white/15 bg-[#16161d]/95 p-2 text-left shadow-2xl shadow-black/50 backdrop-blur sm:right-8 sm:w-72"
		aria-hidden="true"
	>
		<div class="flex items-center justify-between border-b border-white/10 px-2 pb-2 text-xs text-muted-foreground">
			<span><span class="text-foreground">carry</span><span class="text-brand-300">|</span></span>
			<kbd class="rounded border border-white/15 px-1 font-mono text-[0.6rem]">⌘K</kbd>
		</div>
		<ul class="mt-1 text-xs">
			{#each results as result, i}
				<li class="flex items-center justify-between gap-2 rounded px-2 py-1.5 {i === 0 ? 'bg-brand-400/15 text-foreground' : 'text-muted-foreground'}">
					<span class="truncate">{result.label}</span>
					<span class="shrink-0 text-[0.6rem] text-muted-foreground">{result.hint}</span>
				</li>
			{/each}
		</ul>
	</div>
</div>
