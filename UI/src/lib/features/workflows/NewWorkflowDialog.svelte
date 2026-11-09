<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { agentProfiles, workflows, type WorkflowSaveInput, type WorkflowSummary } from '$lib/api/workflows';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { nodeTypeLabel } from './labels';
	import { starterGraph } from './workflow-graph';

	// Creates a workflow from a template. Blank starts with one agent step run
	// by the built-in Implementer; the other templates are built by the server.
	let {
		slug,
		oncreated,
		onclose
	}: { slug: string; oncreated: (created: WorkflowSummary) => void; onclose: () => void } = $props();

	type Template = 'blank' | 'review_loop' | 'full_cycle' | 'standard';
	let name = $state(''),
		description = $state(''),
		template = $state<Template>('review_loop'),
		saving = $state(false);

	const hints: Record<Template, () => string> = {
		review_loop: m['workflows.template_hint_review_loop'],
		full_cycle: m['workflows.template_hint_full_cycle'],
		standard: m['workflows.template_hint_standard'],
		blank: m['workflows.template_hint_blank']
	};

	async function create(event: SubmitEvent) {
		event.preventDefault();
		if (saving || !name.trim()) return;
		saving = true;
		try {
			const input: WorkflowSaveInput = { name: name.trim(), description: description.trim() };
			if (template === 'blank') {
				const { profiles } = await agentProfiles.list(slug);
				const implementer = profiles.find((p) => p.builtin_key === 'implementer' && !p.archived_at);
				input.graph = starterGraph(
					{ start: nodeTypeLabel('start'), agent: nodeTypeLabel('agent'), end: nodeTypeLabel('end') },
					implementer?.id
				);
			} else input.template = template;
			const created = await workflows.create(slug, input);
			appToast.success(m['workflows.created']());
			oncreated(created);
		} catch (error) {
			appToast.apiError(error, m['workflows.failed_save']());
		} finally {
			saving = false;
		}
	}
</script>

<Dialog.Root
	open
	onOpenChange={(open) => {
		if (!open) onclose();
	}}
>
	<Dialog.Content class="sm:max-w-lg">
		<form onsubmit={create} class="space-y-4" data-testid="new-workflow-form">
			<Dialog.Header>
				<Dialog.Title>{m['workflows.new']()}</Dialog.Title>
				<Dialog.Description>{m['workflows.new_desc']()}</Dialog.Description>
			</Dialog.Header>
			<div class="space-y-1.5">
				<Label for="new-workflow-name">{m['workflows.name']()}</Label>
				<Input id="new-workflow-name" bind:value={name} maxlength={80} required />
			</div>
			<div class="space-y-1.5">
				<Label for="new-workflow-description">{m['workflows.description']()}</Label>
				<Textarea id="new-workflow-description" bind:value={description} maxlength={500} rows={2} />
			</div>
			<div class="space-y-1.5">
				<Label for="new-workflow-template">{m['workflows.template']()}</Label>
				<SelectField
					id="new-workflow-template"
					bind:value={template}
					options={[
						{ value: 'review_loop', label: m['workflows.template_review_loop']() },
						{ value: 'full_cycle', label: m['workflows.template_full_cycle']() },
						{ value: 'standard', label: m['workflows.template_standard']() },
						{ value: 'blank', label: m['workflows.template_blank']() }
					]}
				/>
				<p class="text-xs text-muted-foreground">{hints[template]()}</p>
			</div>
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={onclose}>{m['common.cancel']()}</Button>
				<Button type="submit" disabled={saving || !name.trim()}>{m['workflows.create']()}</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
