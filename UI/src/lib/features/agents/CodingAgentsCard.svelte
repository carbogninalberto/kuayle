<script lang="ts">
	import { Switch } from '$lib/components/ui/switch/index.js';
	import type { DevMachinePolicy } from '$lib/types/dev-machine';
	import { m } from '$lib/paraglide/messages.js';
	import AgentLogo from './AgentLogo.svelte';
	import { agents } from './agents';

	// Which coding agents members may start. Codex and Claude Code can use a
	// model server; the others, or those without a matching server, use their
	// own sign-in.
	let {
		policy,
		canAdmin,
		saving,
		onchange
	}: {
		policy: DevMachinePolicy;
		canAdmin: boolean;
		saving: boolean;
		onchange: (allowed: string[], allowCustom: boolean) => void;
	} = $props();

	function toggle(id: string) {
		const allowed = policy.allowed_providers.includes(id)
			? policy.allowed_providers.filter((item) => item !== id)
			: [...policy.allowed_providers, id];
		onchange(allowed, policy.allow_custom_providers);
	}
</script>

<section aria-labelledby="coding-agents-title" class="space-y-4 rounded-xl border bg-card p-5">
	<div>
		<h2 id="coding-agents-title" class="text-sm font-semibold">{m['settings.agents.agents_title']()}</h2>
		<p class="text-xs text-muted-foreground">{m['settings.agents.agents_desc']()}</p>
	</div>
	<div class="grid gap-2 sm:grid-cols-2">
		{#each agents as agent (agent.id)}
			<label class="flex items-center gap-3 rounded-lg border p-3">
				<AgentLogo agent={agent.id} size="lg" />
				<span class="min-w-0 flex-1">
					<span class="block text-sm font-medium">{agent.name}</span>
					<span class="block truncate text-xs text-muted-foreground"
						>{agent.protocol === 'openai'
							? m['settings.agents.uses_openai']()
							: agent.protocol === 'anthropic'
								? m['settings.agents.uses_anthropic']()
								: m['settings.agents.own_sign_in']()}</span
					>
				</span>
				<Switch
					aria-label={m['settings.agents.allow_named']({ name: agent.name })}
					checked={policy.allowed_providers.includes(agent.id)}
					disabled={!canAdmin || saving}
					onCheckedChange={() => toggle(agent.id)}
				/>
			</label>
		{/each}
	</div>
	<label class="flex items-center justify-between gap-4 text-sm">
		<span>{m['settings.dev_machines.allow_custom']()}</span>
		<Switch
			checked={policy.allow_custom_providers}
			disabled={!canAdmin || saving}
			onCheckedChange={(checked) => onchange(policy.allowed_providers, checked)}
		/>
	</label>
</section>
