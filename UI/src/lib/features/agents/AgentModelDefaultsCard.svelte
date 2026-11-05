<script lang="ts">
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { agentModelDefaults, modelServers } from '$lib/api/dev-machines';
	import type { AgentModelDefault, ModelServer } from '$lib/types/dev-machine';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import AgentLogo from './AgentLogo.svelte';
	import { agentInfo } from './agents';
	import { AGENT_EFFORT, effortChoice, modelDefaultInput, overridingServer } from './model-choice';

	// The workspace default model and effort of Codex and Claude Code on their
	// own sign-in. Members see them; owners and admins change them.
	let { slug, canAdmin }: { slug: string; canAdmin: boolean } = $props();
	let defaults = $state<AgentModelDefault[]>([]),
		servers = $state<ModelServer[]>([]),
		drafts = $state<Record<string, { model: string; effort: string }>>({}),
		loading = $state(true),
		failed = $state(false),
		saving = $state('');

	function draftFrom(value: AgentModelDefault) {
		return { model: value.model, effort: effortChoice(value.thinking) };
	}

	async function load() {
		loading = true;
		failed = false;
		try {
			const [loaded, loadedServers] = await Promise.all([agentModelDefaults.list(slug), modelServers.list(slug)]);
			defaults = loaded;
			servers = loadedServers;
			drafts = Object.fromEntries(loaded.map((value) => [value.provider, draftFrom(value)]));
		} catch {
			failed = true;
		} finally {
			loading = false;
		}
	}
	onMount(load);

	function changed(value: AgentModelDefault) {
		const draft = drafts[value.provider];
		const input = modelDefaultInput(draft.model, draft.effort);
		return input.model !== value.model || input.thinking !== value.thinking;
	}

	async function save(value: AgentModelDefault, event: SubmitEvent) {
		event.preventDefault();
		if (!canAdmin || saving) return;
		saving = value.provider;
		const draft = drafts[value.provider];
		try {
			const saved = await agentModelDefaults.save(slug, value.provider, modelDefaultInput(draft.model, draft.effort));
			defaults = defaults.map((item) => (item.provider === saved.provider ? saved : item));
			drafts[saved.provider] = draftFrom(saved);
			appToast.success(m['settings.agents.defaults_saved']());
		} catch (error) {
			appToast.apiError(error, m['settings.agents.defaults_save_failed']());
		} finally {
			saving = '';
		}
	}
</script>

<section aria-labelledby="agent-model-defaults-title" class="space-y-4 rounded-xl border bg-card p-5">
	<div>
		<h2 id="agent-model-defaults-title" class="text-sm font-semibold">{m['settings.agents.defaults_title']()}</h2>
		<p class="text-xs text-muted-foreground">{m['settings.agents.defaults_desc']()}</p>
	</div>
	{#if loading}<p role="status" class="text-xs text-muted-foreground">{m['common.loading']()}</p>
	{:else if failed}
		<div class="flex items-center justify-between gap-3">
			<p role="alert" class="text-sm text-destructive">{m['settings.agents.defaults_failed']()}</p>
			<Button size="sm" variant="outline" onclick={load}>{m['machines.refresh']()}</Button>
		</div>
	{:else}
		<div class="space-y-3">
			{#each defaults as value (value.provider)}
				{@const agent = agentInfo(value.provider)}
				{@const server = overridingServer(servers, value.provider)}
				<form class="space-y-2 rounded-lg border p-3" onsubmit={(event) => save(value, event)}>
					<div class="flex items-center gap-2 text-sm font-medium">
						<AgentLogo agent={value.provider} size="sm" />{agent.name}
					</div>
					<div class="grid items-end gap-3 sm:grid-cols-[1fr_12rem_auto]">
						<div class="space-y-1.5">
							<Label for="agent-default-model-{value.provider}">{m['settings.agents.defaults_model']()}</Label>
							<Input
								id="agent-default-model-{value.provider}"
								bind:value={drafts[value.provider].model}
								placeholder={m['settings.agents.defaults_model_placeholder']()}
								maxlength={128}
								autocomplete="off"
								class="font-mono"
								disabled={!canAdmin}
							/>
						</div>
						<div class="space-y-1.5">
							<Label for="agent-default-effort-{value.provider}">{m['settings.agents.defaults_effort']()}</Label>
							<SelectField
								bind:value={drafts[value.provider].effort}
								options={[
									{ value: AGENT_EFFORT, label: m['settings.agents.effort_agent']() },
									...value.thinking_levels.map((level) => ({
										value: level,
										label: m[`settings.agents.thinking_${level}`]()
									}))
								]}
								id="agent-default-effort-{value.provider}"
								disabled={!canAdmin}
							/>
						</div>
						{#if canAdmin}<Button
								type="submit"
								size="sm"
								aria-label={m['settings.agents.defaults_save_named']({ name: agent.name })}
								disabled={!!saving || !changed(value)}>{m['common.save']()}</Button
							>{/if}
					</div>
					{#if server}<p class="text-xs text-muted-foreground">
							{m['settings.agents.defaults_server_note']({ server: server.name })}
						</p>{/if}
				</form>
			{/each}
		</div>
	{/if}
</section>
