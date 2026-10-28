<script lang="ts">
	import { X, RefreshCw, TriangleAlert } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Progress } from '$lib/components/ui/progress/index.js';
	import { m } from '$lib/paraglide/messages.js';
	import type { ComposerAttachments } from './composer-attachments.svelte';
	import { formatBytes } from './attachment-rules';

	// AI Elements Prompt Input attachments: removable previews above the input,
	// with upload progress and a retry for failed uploads.
	let { attachments }: { attachments: ComposerAttachments } = $props();
</script>

{#if attachments.items.length}
	<ul class="flex w-full flex-wrap gap-2" aria-label={m['assistant.attachments_label']()}>
		{#each attachments.items as item (item.key)}
			<li
				class="relative flex w-40 items-center gap-2 rounded-md border bg-background p-1.5 pr-8 text-xs"
				aria-busy={item.status === 'uploading'}
			>
				<img src={item.preview} alt="" class="size-10 shrink-0 rounded object-cover" />
				<div class="min-w-0 flex-1">
					<p class="truncate font-medium" title={item.file.name}>{item.file.name}</p>
					{#if item.status === 'uploading'}
						<Progress
							value={Math.round(item.progress * 100)}
							max={100}
							class="mt-1 h-1"
							aria-label={m['assistant.attachment_uploading']({ name: item.file.name })}
						/>
					{:else if item.status === 'failed'}
						<p class="flex items-center gap-1 text-destructive" role="alert">
							<TriangleAlert size={12} class="shrink-0" /><span class="truncate" title={item.error}>{item.error}</span>
						</p>
					{:else}
						<p class="text-muted-foreground">{formatBytes(item.file.size)}</p>
					{/if}
				</div>
				<div class="absolute top-1 right-1 flex flex-col gap-0.5">
					<Button
						variant="ghost"
						size="icon-xs"
						aria-label={m['assistant.attachment_remove']({ name: item.file.name })}
						onclick={() => attachments.remove(item.key)}><X size={12} /></Button
					>
					{#if item.status === 'failed'}<Button
							variant="ghost"
							size="icon-xs"
							aria-label={m['assistant.attachment_retry']({ name: item.file.name })}
							onclick={() => attachments.retry(item.key)}><RefreshCw size={12} /></Button
						>{/if}
				</div>
			</li>
		{/each}
	</ul>
{/if}
