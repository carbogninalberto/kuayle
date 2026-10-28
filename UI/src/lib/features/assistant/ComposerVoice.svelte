<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { Mic, Check, X, LoaderCircle } from '@lucide/svelte';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import { voice } from '$lib/api/voice';
	import { appToast } from '$lib/features/toast/toast';
	import { canRecord, VoiceRecorder } from '$lib/features/voice/voice-recorder.svelte';
	import { formatDuration, type VoiceProblem } from '$lib/features/voice/voice-rules';
	import { m } from '$lib/paraglide/messages.js';

	// Composer microphone (AI Elements Prompt Input speech button): records up
	// to the workspace limit with a visible timer, can be cancelled, and puts
	// the transcript in the composer for the user to review and send. Hidden
	// unless the workspace has a ready voice model and the browser can record.
	let {
		slug,
		disabled = false,
		ensureConversation,
		ontranscript
	}: {
		slug: string;
		disabled?: boolean;
		ensureConversation: () => Promise<string>;
		ontranscript: (text: string) => void;
	} = $props();

	let available = $state(false);
	let maxSeconds = $state(300);
	let recorder = $state<VoiceRecorder | null>(null);

	const problems: Record<VoiceProblem, () => string> = {
		denied: () => m['voice.problem_denied'](),
		no_microphone: () => m['voice.problem_no_microphone'](),
		unsupported: () => m['voice.problem_unsupported'](),
		too_long: () => m['voice.problem_too_long'](),
		too_large: () => m['voice.problem_too_large'](),
		busy: () => m['voice.problem_busy'](),
		not_ready: () => m['voice.problem_not_ready'](),
		off: () => m['voice.problem_off'](),
		empty: () => m['voice.problem_empty'](),
		failed: () => m['voice.problem_failed']()
	};

	onMount(() => {
		let disposed = false;
		if (canRecord())
			void voice
				.status(untrack(() => slug))
				.then((status) => {
					if (disposed) return;
					maxSeconds = status.max_seconds;
					available = status.ready;
					recorder = new VoiceRecorder(
						status.max_seconds,
						async (audio) => {
							const conversation = await ensureConversation();
							const { text } = await voice.transcribe(slug, conversation, audio);
							if (text.trim()) ontranscript(text);
							else appToast.info(m['voice.no_speech']());
						},
						(problem) => {
							if (problem === 'off') available = false;
							appToast.error(problems[problem]());
						}
					);
				})
				.catch(() => {
					/* Voice stays hidden when its status is unavailable. */
				});
		return () => {
			disposed = true;
			recorder?.dispose();
		};
	});
</script>

{#if available && recorder}
	{#if recorder.status === 'recording'}
		<span
			class="flex items-center gap-1.5 px-1 text-xs text-muted-foreground tabular-nums"
			role="timer"
			aria-label={m['voice.recording']({ elapsed: formatDuration(recorder.elapsed), max: formatDuration(maxSeconds) })}
		>
			<span class="size-2 shrink-0 animate-pulse rounded-full bg-destructive" aria-hidden="true"></span>
			{formatDuration(recorder.elapsed)} / {formatDuration(maxSeconds)}
		</span>
		<InputGroup.Button size="icon-sm" aria-label={m['voice.cancel']()} onclick={() => recorder?.cancel()}
			><X size={16} /></InputGroup.Button
		>
		<InputGroup.Button size="icon-sm" variant="secondary" aria-label={m['voice.stop']()} onclick={() => recorder?.stop()}
			><Check size={16} /></InputGroup.Button
		>
	{:else}
		<InputGroup.Button
			size="icon-sm"
			aria-label={recorder.status === 'transcribing' ? m['voice.transcribing']() : m['voice.record']()}
			aria-busy={recorder.status !== 'idle'}
			disabled={disabled || recorder.status !== 'idle'}
			onclick={() => void recorder?.start()}
		>
			{#if recorder.status === 'idle'}<Mic size={16} />{:else}<LoaderCircle size={16} class="animate-spin" />{/if}
		</InputGroup.Button>
	{/if}
{/if}
