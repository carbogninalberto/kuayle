<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Label } from '$lib/components/ui/label';

	const rowId = $props.id();
	let { id = rowId, label, help, error, control, class: className = '' }: {
		class?: string;
		id?: string;
		label: string;
		help?: string;
		error?: string;
		control: Snippet<[{
			id: string;
			'aria-describedby'?: string;
			'aria-invalid'?: true;
		}]>;
	} = $props();

	const describedBy = $derived([help && id + '-help', error && id + '-error'].filter(Boolean).join(' ') || undefined);
</script>

<div class="@container {className}">
	<div class="grid gap-2 @md:grid-cols-2 @md:items-start @md:gap-6">
		<div class="min-w-0 space-y-1 wrap-anywhere">
			<Label for={id} class="leading-5">{label}</Label>
			{#if help}<p id="{id}-help" class="text-sm text-[var(--color-text-secondary)]">{help}</p>{/if}
		</div>
		<div class="min-w-0 space-y-1">
			{@render control({ id, 'aria-describedby': describedBy, 'aria-invalid': error ? true : undefined })}
			{#if error}<p id="{id}-error" class="text-sm wrap-anywhere text-destructive">{error}</p>{/if}
		</div>
	</div>
</div>
