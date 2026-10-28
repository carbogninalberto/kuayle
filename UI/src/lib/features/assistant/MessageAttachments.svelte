<script lang="ts">
	import { Download } from '@lucide/svelte';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { conversations, type ConversationAttachment } from '$lib/api/conversations';
	import { m } from '$lib/paraglide/messages.js';

	// AI Elements Message attachments: thumbnails that open in a dialog. Images
	// load from the authenticated conversation endpoint, never a public URL.
	let {
		slug,
		conversationId,
		attachments
	}: { slug: string; conversationId: string; attachments: ConversationAttachment[] } = $props();
	let open = $state<ConversationAttachment | null>(null);
	const url = (attachment: ConversationAttachment) => conversations.attachmentURL(slug, conversationId, attachment.id);
</script>

<ul class="flex flex-wrap justify-end gap-2" aria-label={m['assistant.attachments_label']()}>
	{#each attachments as attachment (attachment.id)}
		<li>
			<Button
				variant="plain"
				class="block overflow-hidden rounded-lg border bg-muted outline-none focus-visible:ring-2 focus-visible:ring-ring"
				aria-label={m['assistant.attachment_open']({ name: attachment.filename })}
				onclick={() => (open = attachment)}
			>
				<img
					src={url(attachment)}
					alt={attachment.filename}
					width={attachment.width}
					height={attachment.height}
					loading="lazy"
					class="size-24 object-cover sm:size-28"
				/>
			</Button>
		</li>
	{/each}
</ul>
<Dialog.Root open={!!open} onOpenChange={(value) => !value && (open = null)}>
	<Dialog.Content class="max-h-[92vh] w-auto max-w-[min(92vw,1200px)] sm:max-w-[min(92vw,1200px)]">
		{#if open}
			<Dialog.Header>
				<Dialog.Title class="truncate pr-8">{open.filename}</Dialog.Title>
				<Dialog.Description>{open.width} × {open.height}</Dialog.Description>
			</Dialog.Header>
			<img
				src={url(open)}
				alt={open.filename}
				class="h-[min(70vh,800px)] w-[min(84vw,1100px)] max-w-full justify-self-center rounded bg-muted object-contain"
			/>
			<Dialog.Footer>
				<Button variant="outline" size="sm" href="{url(open)}?download=1" download={open.filename}
					><Download size={14} />{m['assistant.attachment_download']()}</Button
				>
			</Dialog.Footer>
		{/if}
	</Dialog.Content>
</Dialog.Root>
