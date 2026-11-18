<script lang="ts">
	import { badgeVariants } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { getLocale, m } from '$lib/features/i18n/messages';
	import { cn } from '$lib/utils.js';
	import { formatDate } from '$lib/utils/format';

	let {
		version,
		versions,
		dirty
	}: {
		version: number;
		versions: { version: number; created_at?: string }[];
		dirty: boolean;
	} = $props();

	let versionOpen = $state(false);
	let historyOpen = $state(false);
	let versionButton = $state<HTMLElement | null>(null);
	const stateLabel = $derived(
		dirty
			? m['workflows.editor.draft_state']()
			: version > 0
				? m['workflows.editor.saved_state']()
				: m['workflows.builtin']()
	);
	const versionLabel = $derived(
		version > 0 ? m['workflows.editor.saved']({ version }) : m['workflows.editor.no_versions']()
	);
</script>

<Dialog.Root
	bind:open={historyOpen}
	onOpenChange={(open) => {
		if (open) versionOpen = false;
	}}
>
	<Popover.Root bind:open={versionOpen}>
		<Popover.Trigger>
			{#snippet child({ props })}
				<Button
					{...props}
					bind:ref={versionButton}
					variant="plain"
					class={cn(
						badgeVariants({ variant: 'outline' }),
						'h-7 shrink-0 border-current/25 text-xs hover:border-current focus-visible:outline-none',
						dirty
							? 'text-[var(--status-warning)] dark:bg-[var(--status-warning)]/10'
							: 'text-[var(--status-success)] dark:bg-[var(--status-success)]/10'
					)}
				>
					<span aria-live="polite">{version > 0 ? `v${version} · ${stateLabel}` : stateLabel}</span>
				</Button>
			{/snippet}
		</Popover.Trigger>
		<Popover.Content
			align="end"
			role="dialog"
			aria-label={versionLabel}
			class="gap-2 p-3"
			onCloseAutoFocus={(event) => {
				if (historyOpen) event.preventDefault();
			}}
		>
			<p class="font-medium">{versionLabel}</p>
			<p class="text-xs text-muted-foreground">
				{dirty ? m['workflows.editor.pending_edits']() : m['workflows.editor.no_pending_edits']()}
			</p>
			<Dialog.Trigger>
				{#snippet child({ props })}
					<Button {...props} variant="link" class="h-auto self-start p-0 text-xs"
						>{m['workflows.editor.version_history']()}</Button
					>
				{/snippet}
			</Dialog.Trigger>
		</Popover.Content>
	</Popover.Root>
	<Dialog.Content
		aria-describedby={undefined}
		onCloseAutoFocus={(event) => {
			event.preventDefault();
			versionButton?.focus();
		}}
	>
		<Dialog.Title>{m['workflows.editor.version_history']()}</Dialog.Title>
		{#if versions.length > 0}
			<ul class="max-h-[60vh] divide-y overflow-y-auto" role="list">
				{#each versions as entry (entry.version)}
					<li class="flex items-center justify-between gap-3 px-1 py-2">
						<span>{m['workflows.version']({ version: entry.version })}</span>
						{#if entry.created_at}<time datetime={entry.created_at} class="text-xs text-muted-foreground"
								>{formatDate(entry.created_at, getLocale())}</time
							>{/if}
					</li>
				{/each}
			</ul>
		{:else}
			<p class="text-muted-foreground">{m['workflows.editor.no_versions']()}</p>
		{/if}
	</Dialog.Content>
</Dialog.Root>
