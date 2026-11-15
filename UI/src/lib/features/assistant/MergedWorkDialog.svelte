<script lang="ts">
	import { ExternalLink } from '@lucide/svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import type { MergedPullRequest } from '$lib/api/agent-tasks';
	import { m } from '$lib/features/i18n/messages';

	// KUA-193: asked before a retry, or an iteration started for a message,
	// runs the agent again on an issue whose work was already merged. "Retry
	// anyway" closes it and lets the caller send the same request again,
	// confirmed; Cancel changes nothing.
	let {
		pullRequest = $bindable(null),
		onconfirm
	}: {
		/** The merged pull request the server named (link already checked); null closes the dialog. */
		pullRequest?: MergedPullRequest | null;
		onconfirm: () => void;
	} = $props();
	// The last one named stays on screen while the dialog animates closed.
	let shown = $state<MergedPullRequest | null>(null);
	$effect(() => {
		if (pullRequest) shown = pullRequest;
	});
</script>

<AlertDialog.Root
	open={!!pullRequest}
	onOpenChange={(open) => {
		if (!open) pullRequest = null;
	}}
>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m['tasks.merged_work_title']({ number: shown?.number ?? 0 })}</AlertDialog.Title>
			<AlertDialog.Description class="break-words">{m['tasks.merged_work_body']()}</AlertDialog.Description>
			{#if shown?.url}<a
					class="inline-flex w-fit items-center gap-1 text-sm underline underline-offset-2"
					href={shown.url}
					target="_blank"
					rel="noopener noreferrer"><ExternalLink size={14} />{m['tasks.merged_work_link']({ number: shown.number })}</a
				>{/if}
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
			<AlertDialog.Action
				onclick={() => {
					pullRequest = null;
					onconfirm();
				}}>{m['tasks.merged_work_confirm']()}</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
