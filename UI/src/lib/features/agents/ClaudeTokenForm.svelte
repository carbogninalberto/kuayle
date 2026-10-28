<script lang="ts">
	import { LoaderCircle } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Password } from '$lib/components/ui/password';
	import { agentSignIns, type AgentSignIn } from '$lib/api/agent-sign-ins';
	import { m } from '$lib/paraglide/messages.js';
	import { tokenProblem } from './sign-in';

	// Saves the user's Claude Code token (the output of `claude setup-token`).
	// The value never comes back from the API, so the field is cleared once saved.
	let {
		slug,
		saved = false,
		submitLabel,
		onsaved
	}: {
		slug: string;
		/** A token is already saved: the field replaces it. */
		saved?: boolean;
		submitLabel?: string;
		onsaved: (signIn: AgentSignIn) => void | Promise<void>;
	} = $props();
	const id = $props.id();
	let value = $state(''),
		busy = $state(false),
		error = $state('');

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (busy) return;
		const problem = tokenProblem(value);
		if (problem) {
			error = problem === 'empty' ? m['settings.agents.claude_token_empty']() : m['settings.agents.claude_token_invalid']();
			return;
		}
		busy = true;
		error = '';
		try {
			const signIn = await agentSignIns.saveToken(slug, 'claude-code', value.trim());
			value = '';
			await onsaved(signIn);
		} catch (cause) {
			const status = (cause as { status?: number }).status;
			error = status === 400 ? m['settings.agents.claude_token_invalid']() : m['settings.agents.claude_token_failed']();
		} finally {
			busy = false;
		}
	}
</script>

<form class="space-y-2" onsubmit={submit} novalidate>
	<Label for="{id}-token">{m['settings.agents.claude_token_label']()}</Label>
	<div class="flex flex-col gap-2 sm:flex-row">
		<div class="min-w-0 sm:flex-1">
			<Password
				id="{id}-token"
				bind:value
				class="font-mono"
				autocomplete="off"
				spellcheck={false}
				placeholder={saved ? m['settings.agents.claude_token_replace']() : 'sk-ant-oat01-…'}
				aria-invalid={error ? true : undefined}
				aria-describedby={error ? `${id}-error` : undefined}
				disabled={busy}
				oninput={() => (error = '')}
			/>
		</div>
		<Button type="submit" size="sm" class="sm:self-center" disabled={busy}
			>{#if busy}<LoaderCircle class="animate-spin" />{m['settings.agents.claude_token_saving']()}{:else}{submitLabel ??
					m['settings.agents.claude_token_save']()}{/if}</Button
		>
	</div>
	{#if error}<p id="{id}-error" role="alert" class="text-xs text-destructive">{error}</p>{/if}
</form>
