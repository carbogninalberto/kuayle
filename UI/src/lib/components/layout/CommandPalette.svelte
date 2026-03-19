<script lang="ts">
	import { goto } from '$app/navigation';
	import type { Team } from '$lib/types/team';

	let { slug, teams, onclose }: { slug: string; teams: Team[]; onclose: () => void } = $props();
	let search = $state('');
	let selectedIndex = $state(0);

	interface CommandItem {
		label: string;
		description?: string;
		action: () => void;
	}

	const commands: CommandItem[] = $derived.by(() => {
		const items: CommandItem[] = [
			{ label: 'Go to Inbox', action: () => navigate(`/${slug}/inbox`) },
			{ label: 'Go to My Issues', action: () => navigate(`/${slug}/my-issues`) },
			{ label: 'Go to Dashboard', action: () => navigate(`/${slug}/dashboard`) },
			{ label: 'Go to Projects', action: () => navigate(`/${slug}/projects`) },
			{ label: 'Go to Settings', action: () => navigate(`/${slug}/settings`) },
			...teams.map((t) => ({
				label: `Go to ${t.name}`,
				description: t.key,
				action: () => navigate(`/${slug}/teams/${t.id}`)
			}))
		];

		if (!search) return items;
		return items.filter((i) => i.label.toLowerCase().includes(search.toLowerCase()));
	});

	function navigate(path: string) {
		goto(path);
		onclose();
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			onclose();
		} else if (e.key === 'ArrowDown') {
			e.preventDefault();
			selectedIndex = Math.min(selectedIndex + 1, commands.length - 1);
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			selectedIndex = Math.max(selectedIndex - 1, 0);
		} else if (e.key === 'Enter' && commands[selectedIndex]) {
			commands[selectedIndex].action();
		}
	}
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	class="fixed inset-0 z-50 flex items-start justify-center pt-[20vh]"
	onkeydown={handleKeydown}
>
	<!-- Backdrop -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="fixed inset-0 bg-black/50" onclick={onclose}></div>

	<!-- Dialog -->
	<div
		class="relative z-10 w-full max-w-lg overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] shadow-2xl"
	>
		<input
			type="text"
			bind:value={search}
			placeholder="Type a command or search..."
			autofocus
			class="w-full border-b border-[var(--app-border)] bg-transparent px-4 py-3 text-sm text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
		/>
		<div class="max-h-72 overflow-y-auto py-2">
			{#each commands as cmd, i}
				<button
					class="flex w-full items-center gap-3 px-4 py-2 text-left text-sm {i === selectedIndex
						? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-secondary)]'}"
					onmouseenter={() => (selectedIndex = i)}
					onclick={() => cmd.action()}
				>
					<span>{cmd.label}</span>
					{#if cmd.description}
						<span class="text-xs text-[var(--color-text-tertiary)]">{cmd.description}</span>
					{/if}
				</button>
			{/each}
			{#if commands.length === 0}
				<p class="px-4 py-2 text-sm text-[var(--color-text-tertiary)]">No results found</p>
			{/if}
		</div>
	</div>
</div>
