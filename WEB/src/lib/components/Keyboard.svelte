<script lang="ts">
	import { reveal } from '$lib/actions/reveal';

	const shortcuts = [
		{ keys: ['C'], label: 'Create issue' },
		{ keys: ['⌘', 'K'], label: 'Command palette' },
		{ keys: ['A'], label: 'Assign to…' },
		{ keys: ['S'], label: 'Set status' },
		{ keys: ['L'], label: 'Add label' },
		{ keys: ['⇧', 'V'], label: 'Switch view' }
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
					Built around shortcuts, not menus
				</h2>
				<p class="mt-5 text-lg leading-relaxed text-muted-foreground">
					Every action in Kuayle is reachable from the keyboard: single keys for the common
					operations, a fuzzy command palette for everything else. Triage a full inbox without
					touching the mouse once.
				</p>
			</div>

			<div use:reveal={{ delay: 150 }} class="grid grid-cols-2 gap-4">
				{#each shortcuts as shortcut, i (shortcut.label)}
					<div
						class="flex items-center justify-between gap-3 rounded-xl border border-border bg-card/60 px-4 py-3.5"
					>
						<span class="text-sm text-muted-foreground">{shortcut.label}</span>
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
