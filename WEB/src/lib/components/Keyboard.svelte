<script lang="ts">
	import { reveal } from '$lib/actions/reveal';

	const shortcuts = [
		{ keys: ['C'], label: 'Create issue' },
		{ keys: ['⌘', 'K'], label: 'Command palette' },
		{ keys: ['A'], label: 'Set assignees' },
		{ keys: ['S'], label: 'Set status' },
		{ keys: ['L'], label: 'Add label' },
		{ keys: ['P'], label: 'Set priority' }
	];
</script>

<section id="keyboard" class="relative overflow-hidden py-24 sm:py-32">
	<div
		class="absolute top-1/2 left-1/2 -z-10 h-[420px] w-[720px] -translate-x-1/2 -translate-y-1/2 rounded-full bg-brand-600/15 blur-[140px]"
	></div>

	<div class="mx-auto max-w-6xl px-6">
		<div class="grid items-center gap-14 lg:grid-cols-2">
			<div use:reveal>
				<p class="text-sm font-semibold tracking-widest text-brand-300 uppercase">Keyboard-first</p>
				<h2 class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">
					Shortcuts for frequent issue actions
				</h2>
				<p class="mt-5 text-lg leading-relaxed text-muted-foreground">
					Use C to create an issue and Cmd/Ctrl+K to search issues or open workspace destinations.
					Inside an issue, S, P, A and L open the status, priority, assignee and label controls.
					The triage queue uses J/K for navigation and number keys to accept or decline work.
				</p>
			</div>

		<div use:reveal={{ delay: 150 }} class="grid grid-cols-1 gap-4 sm:grid-cols-2">
			{#each shortcuts as shortcut, i (shortcut.label)}
				<div
					class="group flex items-center justify-between gap-3 rounded-xl border border-border bg-card/60 px-4 py-3.5 transition-all duration-300 hover:-translate-y-0.5 hover:border-brand-400/40 hover:bg-card"
				>
					<span class="text-sm text-muted-foreground transition-colors group-hover:text-foreground">{shortcut.label}</span>
					<span class="flex shrink-0 gap-1">
						{#each shortcut.keys as key (key)}
							<kbd class="key" style="animation: key-press 4s ease-in-out {i * 0.65}s infinite"
								>{key}</kbd
							>
						{/each}
					</span>
				</div>
			{/each}
		</div>
		</div>
	</div>
</section>
