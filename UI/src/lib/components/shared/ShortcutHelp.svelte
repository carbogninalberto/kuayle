<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Kbd } from '$lib/components/ui/kbd';
	import { Separator } from '$lib/components/ui/separator';
	import type { ShortcutDef } from '$lib/utils/keyboard';

	let {
		open = $bindable(false),
		shortcuts
	}: {
		open: boolean;
		shortcuts: ShortcutDef[];
	} = $props();

	// Group shortcuts by category
	let grouped = $derived(
		shortcuts.reduce(
			(acc, s) => {
				const cat = s.category;
				if (!acc[cat]) acc[cat] = [];
				acc[cat].push(s);
				return acc;
			},
			{} as Record<string, ShortcutDef[]>
		)
	);

	let categories = $derived(Object.keys(grouped));

	function getKeyDisplay(shortcut: ShortcutDef): string[][] {
		if ('keys' in shortcut && shortcut.keys) {
			// Sequence: show as separate keys
			return [shortcut.keys.map((k) => k.toUpperCase())];
		}
		if ('key' in shortcut && shortcut.key) {
			const keys: string[] = [];
			if (shortcut.meta) keys.push(isMac() ? '\u2318' : 'Ctrl');
			if (shortcut.shift) keys.push('\u21E7');
			if (shortcut.ctrl && !shortcut.meta) keys.push('Ctrl');
			keys.push(displayKey(shortcut.key));
			return [keys];
		}
		return [[]];
	}

	function displayKey(key: string): string {
		switch (key) {
			case '/': return '/';
			case '?': return '?';
			default: return key.toUpperCase();
		}
	}

	function isMac(): boolean {
		if (typeof navigator === 'undefined') return false;
		return navigator.platform?.toLowerCase().includes('mac') ?? false;
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-[480px] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 overflow-hidden rounded-xl">
		<div class="px-5 pt-5 pb-2">
			<h2 class="text-base font-semibold text-[var(--color-text-primary)]">Keyboard shortcuts</h2>
			<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">Navigate faster with these shortcuts.</p>
		</div>

		<div class="max-h-[400px] overflow-y-auto px-5 pb-5">
			{#each categories as category, i}
				{#if i > 0}
					<Separator class="my-3" />
				{/if}
				<div class="mt-3">
					<h3 class="mb-2 text-[10px] font-medium uppercase tracking-wider text-[var(--color-text-tertiary)]">{category}</h3>
					<div class="space-y-1.5">
						{#each grouped[category] as shortcut}
							<div class="flex items-center justify-between py-1">
								<span class="text-sm text-[var(--color-text-secondary)]">{shortcut.label}</span>
								<div class="flex items-center gap-1">
									{#each getKeyDisplay(shortcut) as keyGroup}
										{#each keyGroup as key, ki}
											{#if ki > 0 && 'keys' in shortcut}
												<span class="text-[10px] text-[var(--color-text-tertiary)]">then</span>
											{/if}
											<Kbd>{key}</Kbd>
										{/each}
									{/each}
								</div>
							</div>
						{/each}
					</div>
				</div>
			{/each}
		</div>
	</Dialog.Content>
</Dialog.Root>
