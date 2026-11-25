<script lang="ts">
	import { CircleAlert, Plus, Trash2, UserRound } from '@lucide/svelte';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import * as RadioGroup from '$lib/components/ui/radio-group/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import type { AgentProfile, PipelineDefinition, WorkflowIssue } from '$lib/api/workflows';
	import { m } from '$lib/features/i18n/messages';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { behaviourLabel, profileTitle } from './labels';
	import { PIPELINE_LIMITS, addRole, removeRole, setLead, setWriter, updateRole, validRole } from './pipeline-editor';
	import { pipelineIssueLabel } from './pipeline-labels';

	// The pipeline's roster: each role, the agent profile that plays it, which
	// one role writes the code and which work only when addressed; and the
	// optional lead. Profiles are the workspace's agent profiles (Settings →
	// Agents); an archived one is no longer offered.
	let {
		pipeline,
		profiles,
		issues,
		editable,
		onchange
	}: {
		pipeline: PipelineDefinition;
		profiles: AgentProfile[];
		issues: WorkflowIssue[];
		editable: boolean;
		onchange: (next: PipelineDefinition) => void;
	} = $props();

	const active = $derived(profiles.filter((profile) => !profile.archived_at));
	const profileOf = (id: string | undefined) => profiles.find((profile) => profile.id === id);
	// The profiles offered for a role: the active ones, and the one it has.
	const profileOptions = (current: string | undefined) => {
		const own = profileOf(current);
		return [
			{ value: '', label: m['workflows.editor.node_profile_placeholder']() },
			...[...active, ...(own && own.archived_at ? [own] : [])].map((profile) => ({
				value: profile.id,
				label: profileTitle(profile)
			}))
		];
	};
	const writer = $derived(pipeline.roster.findIndex((member) => member.writer));
</script>

<div class="space-y-5" data-testid="pipeline-roster">
	{#if issues.length}<Alert.Root variant="destructive" data-testid="pipeline-roster-issues">
			<CircleAlert />
			<Alert.Description>
				{#each issues as issue, i (i)}<span class="block">{pipelineIssueLabel(issue)}</span>{/each}
			</Alert.Description>
		</Alert.Root>{/if}

	<section class="space-y-2" aria-labelledby="pipeline-roles-title">
		<div class="flex items-center justify-between gap-2">
			<h3 id="pipeline-roles-title" class="text-sm font-medium">{m['workflows.pipeline_roles']()}</h3>
			{#if editable}
				<DropdownMenu.Root>
					<DropdownMenu.Trigger>
						{#snippet child({ props })}<Button
								{...props}
								size="sm"
								variant="outline"
								disabled={pipeline.roster.length >= PIPELINE_LIMITS.roster || active.length === 0}
								data-testid="pipeline-add-role"><Plus />{m['workflows.pipeline_editor.add_role']()}</Button
							>{/snippet}
					</DropdownMenu.Trigger>
					<DropdownMenu.Content align="end" class="w-64">
						<DropdownMenu.Label class="text-xs text-muted-foreground"
							>{m['workflows.editor.node_profile']()}</DropdownMenu.Label
						>
						{#each active as profile (profile.id)}
							<DropdownMenu.Item class="gap-2.5" onclick={() => onchange(addRole(pipeline, profile).pipeline)}>
								<AgentLogo agent={profile.harness} size="sm" />
								<span class="min-w-0">
									<span class="block truncate text-sm">{profileTitle(profile)}</span>
									<span class="block text-xs text-muted-foreground">{behaviourLabel(profile.behaviour)}</span>
								</span>
							</DropdownMenu.Item>
						{/each}
					</DropdownMenu.Content>
				</DropdownMenu.Root>
			{/if}
		</div>
		<p class="text-xs text-muted-foreground">{m['workflows.pipeline_editor.role_help']()}</p>
		<RadioGroup.Root
			value={writer >= 0 ? String(writer) : ''}
			onValueChange={(value) => value && onchange(setWriter(pipeline, Number(value)))}
			disabled={!editable}
			aria-label={m['workflows.pipeline_writer']()}
			class="gap-2"
		>
			{#each pipeline.roster as member, i (i)}
				{@const profile = profileOf(member.profile_id)}
				<div class="space-y-3 rounded-lg border bg-card p-3" data-testid="pipeline-role-{i}">
					<div class="flex items-end gap-2">
						{#if profile}<AgentLogo agent={profile.harness} class="mb-1 shrink-0" />{:else}<span
								class="mb-1 flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground"
								><UserRound class="size-4" aria-hidden="true" /></span
							>{/if}
						<div class="min-w-0 flex-1 space-y-1.5">
							<Label for="role-{i}-name">{m['workflows.pipeline_editor.role']()}</Label>
							<Input
								id="role-{i}-name"
								value={member.role}
								maxlength={32}
								autocomplete="off"
								spellcheck={false}
								class="font-mono"
								aria-invalid={!validRole(member.role) ||
									pipeline.roster.some((other, j) => j !== i && other.role === member.role)}
								disabled={!editable}
								oninput={(event) => onchange(updateRole(pipeline, i, { role: event.currentTarget.value }))}
							/>
						</div>
						{#if editable}<Button
								size="icon"
								variant="ghost"
								class="shrink-0 text-muted-foreground hover:text-destructive"
								aria-label={m['workflows.pipeline_editor.remove_role']({ role: member.role })}
								title={m['workflows.pipeline_editor.remove_role']({ role: member.role })}
								disabled={pipeline.roster.length <= 1}
								onclick={() => onchange(removeRole(pipeline, i))}><Trash2 /></Button
							>{/if}
					</div>
					<div class="space-y-1.5">
						<Label for="role-{i}-profile">{m['workflows.editor.node_profile']()}</Label>
						<SelectField
							id="role-{i}-profile"
							bind:value={
								() => member.profile_id ?? '', (id: string) => onchange(updateRole(pipeline, i, { profile_id: id }))
							}
							disabled={!editable}
							options={profileOptions(member.profile_id)}
						/>
						{#if profile}<p class="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
								<Badge variant="outline">{behaviourLabel(profile.behaviour)}</Badge>
								{#if profile.archived_at}<Badge variant="outline">{m['workflows.archived']()}</Badge>{/if}
							</p>{/if}
					</div>
					<div class="flex flex-wrap items-center justify-between gap-3">
						<div class="flex items-center gap-2">
							<RadioGroup.Item value={String(i)} id="role-{i}-writer" />
							<Label for="role-{i}-writer" class="font-normal">{m['workflows.pipeline_writer']()}</Label>
						</div>
						<div class="flex items-center gap-2">
							<Label for="role-{i}-on-call" class="font-normal">{m['workflows.pipeline_editor.on_call']()}</Label>
							<Switch
								id="role-{i}-on-call"
								checked={!!member.on_call}
								disabled={!editable || !!member.writer}
								aria-describedby="pipeline-on-call-help"
								onCheckedChange={(on) => onchange(updateRole(pipeline, i, { on_call: on }))}
							/>
						</div>
					</div>
				</div>
			{/each}
		</RadioGroup.Root>
		<p id="pipeline-on-call-help" class="text-xs text-muted-foreground">
			{m['workflows.pipeline_editor.on_call_help']()}
		</p>
	</section>

	<section class="space-y-1.5" aria-labelledby="pipeline-lead-title">
		<Label id="pipeline-lead-title" for="pipeline-lead">{m['workflows.pipeline_editor.lead']()}</Label>
		<SelectField
			id="pipeline-lead"
			bind:value={
				() => pipeline.lead?.profile_id ?? 'none',
				(id: string) => onchange(setLead(pipeline, id === 'none' ? null : id))
			}
			disabled={!editable}
			aria-describedby="pipeline-lead-help"
			options={[
				{ value: 'none', label: m['workflows.pipeline_editor.no_lead']() },
				...profileOptions(pipeline.lead?.profile_id).slice(1)
			]}
		/>
		<p id="pipeline-lead-help" class="text-xs text-muted-foreground">{m['workflows.pipeline_editor.lead_help']()}</p>
	</section>
</div>
