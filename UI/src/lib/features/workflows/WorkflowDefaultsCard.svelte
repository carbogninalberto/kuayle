<script lang="ts">
	import { Label } from '$lib/components/ui/label/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { workflows, type WorkflowList, type WorkflowScope } from '$lib/api/workflows';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';

	// The workflow new tasks use by default: the workspace's, overridden per
	// team and per project (the most specific wins). A missing team or project
	// row inherits; a stored null chooses the built-in Standard workflow.
	let {
		slug,
		list,
		workspaceId,
		teams,
		projects,
		onchange
	}: {
		slug: string;
		list: WorkflowList;
		workspaceId: string;
		teams: { id: string; name: string }[];
		projects: { id: string; name: string }[];
		onchange: (list: WorkflowList) => void;
	} = $props();

	const INHERIT = 'inherit';
	let busy = $state<string | null>(null);

	const choices = $derived(
		list.workflows
			.filter((w) => !w.archived)
			.map((w) => ({ value: w.id, label: w.builtin ? m['workflows.standard']() : w.name }))
	);

	function current(scope: WorkflowScope, scopeId: string): string {
		const row = list.defaults.find((d) => d.scope === scope && d.scope_id === scopeId);
		if (!row) return scope === 'workspace' ? 'standard' : INHERIT;
		return row.workflow_id ?? 'standard';
	}

	async function choose(scope: WorkflowScope, scopeId: string, value: string) {
		if (!list.can_manage || value === current(scope, scopeId)) return;
		busy = `${scope}:${scopeId}`;
		try {
			onchange(await workflows.setDefault(slug, scope, scopeId, value === INHERIT ? null : value));
			appToast.success(m['workflows.default_set']());
		} catch (error) {
			appToast.apiError(error, m['workflows.failed_save']());
		} finally {
			busy = null;
		}
	}

	const rows = $derived([
		{ scope: 'workspace' as const, id: workspaceId, label: m['workflows.scope_workspace']() },
		...teams.map((t) => ({ scope: 'team' as const, id: t.id, label: m['workflows.scope_team']({ name: t.name }) })),
		...projects.map((p) => ({
			scope: 'project' as const,
			id: p.id,
			label: m['workflows.scope_project']({ name: p.name })
		}))
	]);
</script>

<section
	aria-labelledby="workflow-defaults-title"
	class="space-y-4 rounded-xl border bg-card p-5"
	data-testid="workflow-defaults"
>
	<div>
		<h2 id="workflow-defaults-title" class="text-sm font-semibold">{m['workflows.defaults_title']()}</h2>
		<p class="text-xs text-muted-foreground">{m['workflows.defaults_desc']()}</p>
	</div>
	<div class="space-y-2">
		{#each rows as row (`${row.scope}:${row.id}`)}
			{@const id = `workflow-default-${row.scope}-${row.id}`}
			<div class="grid items-center gap-1.5 sm:grid-cols-[minmax(0,1fr)_16rem] sm:gap-3">
				<Label for={id} class="truncate text-sm">{row.label}</Label>
				<div class="flex sm:justify-end">
					<SelectField
						{id}
						disabled={!list.can_manage || busy !== null}
						bind:value={() => current(row.scope, row.id), (value: string) => void choose(row.scope, row.id, value)}
						options={[
							...(row.scope === 'workspace' ? [] : [{ value: INHERIT, label: m['workflows.inherit']() }]),
							...choices
						]}
					/>
				</div>
			</div>
		{/each}
	</div>
</section>
