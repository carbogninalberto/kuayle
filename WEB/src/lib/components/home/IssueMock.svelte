<script lang="ts">
	import MockFrame from './MockFrame.svelte';
</script>

<MockFrame label="Illustration of a Kuayle issue with three assignees, two sub-issues, a blocking relation and a description edit in the version history." path="ENG-207">
	<div class="grid gap-0 text-left text-xs sm:grid-cols-[1fr_11rem]">
		<div class="space-y-3 p-4">
			<p class="font-mono text-[0.65rem] text-muted-foreground">ENG-207 · In Progress</p>
			<p class="text-sm font-semibold text-foreground">Cycle carry-over keeps sub-issues</p>
			<p class="leading-relaxed text-muted-foreground">When a cycle completes, unfinished issues move to the next cycle with their sub-issues and relations.</p>
			<div class="rounded-md border border-white/10">
				<p class="border-b border-white/10 px-2 py-1 text-[0.65rem] text-muted-foreground">Sub-issues 1 / 2</p>
				<p class="flex items-center gap-2 px-2 py-1.5"><span class="size-2 rounded-full bg-emerald-400"></span><span class="font-mono text-muted-foreground">ENG-208</span> Copy relations on carry-over</p>
				<p class="flex items-center gap-2 px-2 py-1.5"><span class="size-2 rounded-full bg-amber-300"></span><span class="font-mono text-muted-foreground">ENG-209</span> Burndown counts carried issues</p>
			</div>
			<div class="rounded-md border border-dashed border-white/15 p-2">
				<p class="text-[0.65rem] text-muted-foreground">Version history</p>
				<p class="mt-1"><span class="text-muted-foreground">Description edited by</span> Mira · 2h</p>
				<p class="mt-1 font-mono text-[0.65rem]"><span class="bg-red-400/15 text-red-200 line-through">move issues</span> <span class="bg-emerald-400/15 text-emerald-200">move unfinished issues</span></p>
			</div>
		</div>
		<div class="space-y-3 border-t border-white/10 p-4 sm:border-t-0 sm:border-l">
			{#each [['Status', 'In Progress', 'S'], ['Priority', 'Urgent', 'P'], ['Labels', 'cycles', 'L']] as [name, value, key]}
				<div>
					<p class="flex justify-between text-[0.65rem] text-muted-foreground">{name}<kbd class="font-mono">{key}</kbd></p>
					<p class="mt-0.5 text-foreground">{value}</p>
				</div>
			{/each}
			<div>
				<p class="flex justify-between text-[0.65rem] text-muted-foreground">Assignees<kbd class="font-mono">A</kbd></p>
				<ul class="mt-1 space-y-1">
					{#each ['Sam Ito', 'Lena Park', 'Mira Vos'] as person}
						<li class="flex items-center gap-1.5 text-foreground"><span class="grid size-5 place-items-center rounded-full bg-brand-500 text-[0.5rem] leading-none text-white">{person.split(' ').map((p) => p[0]).join('')}</span>{person}</li>
					{/each}
				</ul>
			</div>
			<div>
				<p class="text-[0.65rem] text-muted-foreground">Relations</p>
				<p class="mt-0.5 text-foreground">Blocking <span class="font-mono text-muted-foreground">ENG-214</span></p>
			</div>
		</div>
	</div>
</MockFrame>
