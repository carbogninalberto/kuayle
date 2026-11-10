<script lang="ts">
	import { LoaderCircle } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Password } from '$lib/components/ui/password';
	import { agentSignIns, type AgentSignIn } from '$lib/api/agent-sign-ins';
	import { m } from '$lib/features/i18n/messages';
	import { tokenProblem } from './sign-in';

	// Saves a token the user pastes for an agent: the Claude Code token (the
	// output of `claude setup-token`) or an OpenCode API key. The value never
	// comes back from the API, so the field is cleared once saved.
	let {
		slug,
		provider = 'claude-code',
		saved = false,
		submitLabel,
		onsaved
	}: {
		slug: string;
		provider?: 'claude-code' | 'opencode';
		/** A token is already saved: the field replaces it. */
		saved?: boolean;
		submitLabel?: string;
		onsaved: (signIn: AgentSignIn) => void | Promise<void>;
	} = $props();
	const id = $props.id();
	const copy = $derived(
		provider === 'opencode'
			? {
					label: m['settings.agents.opencode_key_label'](),
					// Kuayle assumes no key format, so it suggests none.
					placeholder: '',
					replace: m['settings.agents.opencode_key_replace'](),
					save: m['settings.agents.opencode_key_save'](),
					empty: m['settings.agents.opencode_key_invalid'](),
					invalid: m['settings.agents.opencode_key_invalid'](),
					failed: m['settings.agents.opencode_key_failed']()
				}
			: {
					label: m['settings.agents.claude_token_label'](),
					placeholder: 'sk-ant-oat01-…',
					replace: m['settings.agents.claude_token_replace'](),
					save: m['settings.agents.claude_token_save'](),
					empty: m['settings.agents.claude_token_empty'](),
					invalid: m['settings.agents.claude_token_invalid'](),
					failed: m['settings.agents.claude_token_failed']()
				}
	);
	let value = $state(''),
		busy = $state(false),
		error = $state('');

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (busy) return;
		const problem = tokenProblem(value);
		if (problem) {
			error = problem === 'empty' ? copy.empty : copy.invalid;
			return;
		}
		busy = true;
		error = '';
		try {
			const signIn = await agentSignIns.saveToken(slug, provider, value.trim());
			value = '';
			await onsaved(signIn);
		} catch (cause) {
			const status = (cause as { status?: number }).status;
			error = status === 400 ? copy.invalid : copy.failed;
		} finally {
			busy = false;
		}
	}
</script>

<form class="space-y-2" onsubmit={submit} novalidate>
	<Label for="{id}-token">{copy.label}</Label>
	<div class="flex flex-col gap-2 sm:flex-row">
		<div class="min-w-0 sm:flex-1">
			<Password
				id="{id}-token"
				bind:value
				class="font-mono"
				autocomplete="off"
				spellcheck={false}
				placeholder={saved ? copy.replace : copy.placeholder}
				aria-invalid={error ? true : undefined}
				aria-describedby={error ? `${id}-error` : undefined}
				disabled={busy}
				oninput={() => (error = '')}
			/>
		</div>
		<Button type="submit" size="sm" class="sm:self-center" disabled={busy}
			>{#if busy}<LoaderCircle class="animate-spin" />{m['settings.agents.claude_token_saving']()}{:else}{submitLabel ?? copy.save}{/if}</Button
		>
	</div>
	{#if error}<p id="{id}-error" role="alert" class="text-xs text-destructive">{error}</p>{/if}
</form>
