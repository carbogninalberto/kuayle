<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import {
		assistantTools,
		type AssistantCustomTool,
		type AssistantCustomToolVersion
	} from '$lib/api/assistant-tools';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { diffLines } from './skill-diff';
	import { schemaSummary } from './tool-schema';
	import { runModeLabel } from './custom-tool-labels';

	// Shows a draft against the version in effect before an admin activates
	// it: every setting that changes and a line diff of the script, as text.
	let {
		open = $bindable(false),
		slug,
		tool,
		draft,
		onactivated
	}: {
		open?: boolean;
		slug: string;
		/** The tool with the script in effect. */
		tool: AssistantCustomTool;
		draft: AssistantCustomToolVersion;
		onactivated: (tool: AssistantCustomTool) => void;
	} = $props();

	let activating = $state(false);
	// A draft tool has nothing in effect yet: show its whole script.
	const replaces = $derived(tool.status === 'active');
	const lines = $derived(
		replaces
			? diffLines(tool.script ?? '', draft.script)
			: draft.script.split('\n').map((text) => ({ kind: 'same' as const, text }))
	);
	const yesNo = (value: boolean) => (value ? m['assistant_custom_tools.yes']() : m['assistant_custom_tools.no']());
	const none = (text: string) => text || m['assistant_custom_tools.no_parameters']();
	const changes = $derived(
		replaces
			? [
					[m['assistant_custom_tools.field_name'](), tool.name, draft.name],
					[m['assistant_custom_tools.field_description'](), tool.description, draft.description],
					[m['assistant_custom_tools.field_interpreter'](), tool.interpreter, draft.interpreter],
					[m['assistant_custom_tools.field_run_mode'](), runModeLabel(tool.run_mode), runModeLabel(draft.run_mode)],
					[m['assistant_custom_tools.read_only'](), yesNo(tool.read_only), yesNo(draft.read_only)],
					[
						m['assistant_custom_tools.field_timeout'](),
						String(tool.timeout_seconds),
						String(draft.timeout_seconds)
					],
					[
						m['assistant_custom_tools.field_parameters'](),
						none(schemaSummary(tool.parameters)),
						none(schemaSummary(draft.parameters))
					]
				].filter(([, before, after]) => before !== after)
			: []
	);

	async function activate() {
		if (activating) return;
		activating = true;
		try {
			const updated = await assistantTools.activate(slug, tool.id, draft.version);
			appToast.success(
				m['assistant_custom_tools.activated_toast']({ name: updated.name, version: String(draft.version) })
			);
			open = false;
			onactivated(updated);
		} catch (err) {
			appToast.apiError(err, m['assistant_custom_tools.failed_save']());
		} finally {
			activating = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
		<Dialog.Header>
			<Dialog.Title>{m['assistant_custom_tools.review_title']({ version: String(draft.version) })}</Dialog.Title>
			<Dialog.Description>
				{replaces
					? m['assistant_custom_tools.review_update']({ version: String(tool.current_version) })
					: m['assistant_custom_tools.review_new']()}
			</Dialog.Description>
		</Dialog.Header>
		{#if replaces}
			{#if changes.length}
				<ul class="space-y-1 text-xs break-words">
					{#each changes as [field, before, after] (field)}
						<li>{m['assistant_custom_tools.change']({ field, before, after })}</li>
					{/each}
				</ul>
			{/if}
		{:else}
			<dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs break-words">
				<dt class="text-muted-foreground">{m['assistant_custom_tools.field_description']()}</dt>
				<dd>{draft.description}</dd>
				<dt class="text-muted-foreground">{m['assistant_custom_tools.field_parameters']()}</dt>
				<dd class="font-mono">{none(schemaSummary(draft.parameters))}</dd>
				<dt class="text-muted-foreground">{m['assistant_custom_tools.field_run_mode']()}</dt>
				<dd>
					{runModeLabel(draft.run_mode)}{draft.read_only ? ` · ${m['assistant_custom_tools.read_only']()}` : ''}
				</dd>
				<dt class="text-muted-foreground">{m['assistant_custom_tools.field_interpreter']()}</dt>
				<dd class="font-mono">{draft.interpreter}</dd>
			</dl>
		{/if}
		<pre
			aria-label={m['assistant_custom_tools.field_script']()}
			class="max-h-[50vh] overflow-auto rounded-lg border bg-muted/40 py-2 font-mono text-xs leading-relaxed break-words whitespace-pre-wrap">{#each lines as line, index (index)}<div
					class="px-3 {line.kind === 'add'
						? 'bg-[var(--status-success)]/10 text-[var(--status-success)]'
						: line.kind === 'del'
							? 'bg-destructive/10 text-destructive line-through'
							: ''}"
					aria-label={line.kind === 'add'
						? m['assistant_custom_tools.diff_added']()
						: line.kind === 'del'
							? m['assistant_custom_tools.diff_removed']()
							: undefined}
					role={line.kind === 'same' ? undefined : 'group'}><span aria-hidden="true" class="select-none"
						>{line.kind === 'add' ? '+ ' : line.kind === 'del' ? '- ' : '  '}</span
					>{line.text || ' '}</div>{/each}</pre>
		<Dialog.Footer class="gap-2">
			<Button variant="outline" onclick={() => (open = false)}>{m['assistant_custom_tools.cancel']()}</Button>
			<Button disabled={activating} onclick={activate}>{m['assistant_custom_tools.activate']()}</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
