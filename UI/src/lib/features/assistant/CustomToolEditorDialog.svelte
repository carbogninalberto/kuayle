<script lang="ts">
	import { untrack } from 'svelte';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import {
		assistantTools,
		type AssistantCustomTool,
		type AssistantCustomToolContent,
		type AssistantToolInterpreter,
		type AssistantToolRunMode,
		type AssistantToolStatus
	} from '$lib/api/assistant-tools';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import CustomToolFields from './CustomToolFields.svelte';
	import CustomToolRunFields from './CustomToolRunFields.svelte';
	import { buildSchema, paramsFromSchema, type ParamDraft } from './tool-schema';

	// Creates a custom tool, or saves an edit of `tool` as its next version.
	let {
		open = $bindable(false),
		slug,
		interpreters,
		tool = null,
		initial = null,
		onsaved
	}: {
		open?: boolean;
		slug: string;
		interpreters: AssistantToolInterpreter[];
		tool?: AssistantCustomTool | null;
		/** The content to start editing from (a pending draft, or the tool's own). */
		initial?: AssistantCustomToolContent | null;
		onsaved: (tool: AssistantCustomTool) => void;
	} = $props();

	const toolName = /^[a-z][a-z0-9_]{0,47}$/;
	let name = $state('');
	let description = $state('');
	let interpreter = $state<AssistantToolInterpreter>('sh');
	let script = $state('');
	let params = $state<ParamDraft[]>([]);
	let runMode = $state<AssistantToolRunMode>('approval');
	let readOnly = $state(false);
	let timeout = $state('600');
	let view = $state('builder');
	let saving = $state(false);

	// Reset the form each time the dialog opens, not when `initial` changes.
	$effect(() => {
		if (!open) return;
		untrack(() => {
			name = initial?.name ?? '';
			description = initial?.description ?? '';
			interpreter = initial?.interpreter ?? interpreters[0] ?? 'sh';
			script = initial?.script ?? '';
			params = paramsFromSchema(initial?.parameters);
			runMode = initial?.run_mode ?? 'approval';
			readOnly = initial?.read_only ?? false;
			timeout = String(initial?.timeout_seconds ?? 600);
			view = 'builder';
		});
	});

	const built = $derived(buildSchema(params));
	const timeoutSeconds = $derived(Number(timeout));
	const timeoutValid = $derived(Number.isInteger(timeoutSeconds) && timeoutSeconds >= 10 && timeoutSeconds <= 1800);
	const nameValid = $derived(toolName.test(name.trim()));
	const valid = $derived(
		nameValid && description.trim() !== '' && script.trim() !== '' && !built.error && timeoutValid
	);

	async function save(status: AssistantToolStatus | undefined) {
		if (!valid || saving || !built.schema) return;
		saving = true;
		const content: AssistantCustomToolContent = {
			name: name.trim(),
			description: description.trim(),
			interpreter,
			script,
			parameters: built.schema,
			run_mode: runMode,
			read_only: readOnly,
			timeout_seconds: timeoutSeconds
		};
		try {
			const saved = tool
				? await assistantTools.update(slug, tool.id, status ? { ...content, status } : content)
				: await assistantTools.create(slug, { ...content, status: status ?? 'active' });
			appToast.success(
				tool
					? m['assistant_custom_tools.saved_toast']({ name: saved.name })
					: m['assistant_custom_tools.created_toast']({ name: saved.name })
			);
			open = false;
			onsaved(saved);
		} catch (err) {
			appToast.apiError(err, m['assistant_custom_tools.failed_save']());
		} finally {
			saving = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
		<Dialog.Header>
			<Dialog.Title
				>{tool
					? m['assistant_custom_tools.edit_title']({ name: tool.name })
					: m['assistant_custom_tools.new_title']()}</Dialog.Title
			>
			<Dialog.Description>{m['assistant_custom_tools.editor_desc']()}</Dialog.Description>
		</Dialog.Header>
		<form
			class="grid min-w-0 gap-4"
			onsubmit={(event) => {
				event.preventDefault();
				void save(tool?.status === 'draft' ? undefined : 'active');
			}}
		>
			<CustomToolFields bind:name bind:description bind:interpreter bind:script bind:params bind:view {interpreters} />
			<CustomToolRunFields bind:runMode bind:readOnly bind:timeout />
			<Dialog.Footer class="flex-wrap gap-2">
				<Button type="button" variant="outline" onclick={() => (open = false)}
					>{m['assistant_custom_tools.cancel']()}</Button
				>
				{#if tool?.status === 'draft'}
					<Button type="submit" disabled={!valid || saving}>{m['assistant_custom_tools.save_draft_only']()}</Button>
				{:else}
					<Button type="button" variant="outline" disabled={!valid || saving} onclick={() => void save('draft')}
						>{m['assistant_custom_tools.save_draft']()}</Button
					>
					<Button type="submit" disabled={!valid || saving}
						>{tool ? m['assistant_custom_tools.save_apply']() : m['assistant_custom_tools.create']()}</Button
					>
				{/if}
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
