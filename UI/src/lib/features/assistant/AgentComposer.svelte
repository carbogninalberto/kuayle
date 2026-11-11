<script lang="ts">
	import { tick } from 'svelte';
	import { ArrowUp, LoaderCircle, Play, Zap } from '@lucide/svelte';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { agentTasks, type AgentTask, type LiveSendMode } from '$lib/api/agent-tasks';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { getErrorMessage } from '$lib/features/toast/toast';
	import type { ConversationState } from './conversation-state.svelte';
	import { sendOptions } from './agent-conversation';
	import { m } from '$lib/features/i18n/messages';

	// The composer of an agent's view: it writes straight to that task's
	// agent, under the transcript, like a local session. Enter sends: the
	// working agent reads it between its steps, otherwise the next iteration
	// does. The one extra action is "now": interrupt the working agent so it
	// changes course at once, or start an iteration when none runs (also
	// Ctrl/⌘+Enter). Nothing here explains itself; what happened to a message
	// is shown on the message. The server decides who may send.
	let { chat, task }: { chat: ConversationState; task: AgentTask } = $props();
	const options = $derived(sendOptions(task, authState.user?.id));
	let text = $state(''),
		sending = $state<'' | 'plain' | LiveSendMode>(''),
		error = $state(''),
		textarea = $state<HTMLTextAreaElement | null>(null);
	let disposed = false;
	$effect(() => () => {
		disposed = true;
	});
	const body = $derived(text.trim());
	const tooLong = $derived([...body].length > 4000);
	const ready = $derived(!!body && !tooLong && !sending);
	const nowLabel = $derived(
		options.now === 'interrupt' ? m['tasks.live_interrupt_send']() : m['tasks.live_start_send']()
	);

	async function send(mode?: LiveSendMode) {
		if (!ready) return;
		const sent = body;
		sending = mode ?? 'plain';
		error = '';
		try {
			await agentTasks.sendLiveMessage(chat.slug, task.id, sent, mode);
			if (disposed) return;
			if (text.trim() === sent) text = '';
			// The timeline reloads and shows the message where it was taken.
			chat.taskLiveCursor = { ...chat.taskLiveCursor, [task.id]: -Date.now() };
			if (mode) chat.acceptTask(await agentTasks.get(chat.slug, task.id), false, chat.liveTasks[task.id] ?? null);
		} catch (e) {
			if (!disposed) error = getErrorMessage(e, m['tasks.live_send_error']());
		} finally {
			if (!disposed) {
				sending = '';
				await tick();
				textarea?.focus();
			}
		}
	}
	// Other parts of the view (a question's answer) bring the cursor here.
	$effect(() => {
		if (chat.composerFocus) void tick().then(() => textarea?.focus());
	});
</script>

{#if options.open}
	<div class="shrink-0 px-3 pt-1 pb-3 md:px-6 md:pb-4" data-testid="agent-composer">
		<form
			class="mx-auto w-full max-w-3xl"
			aria-label={m['tasks.live_composer']()}
			onsubmit={(event) => {
				event.preventDefault();
				void send();
			}}
		>
			{#if error}<p role="alert" class="mb-1.5 text-sm break-words text-destructive">{error}</p>{/if}
			<InputGroup.Root class="rounded-2xl bg-background shadow-sm">
				<InputGroup.Textarea
					bind:ref={textarea}
					bind:value={text}
					rows={1}
					aria-label={m['tasks.live_composer']()}
					aria-invalid={tooLong || undefined}
					placeholder={m['tasks.live_placeholder']()}
					maxlength={16000}
					class="max-h-48 min-h-11"
					onkeydown={(event) => {
						if (event.key !== 'Enter' || event.shiftKey || event.isComposing) return;
						event.preventDefault();
						void send((event.metaKey || event.ctrlKey) && options.now ? options.now : undefined);
					}}
				/>
				<InputGroup.Addon align="inline-end" class="gap-1 self-end pb-1.5">
					<Tooltip.Provider delayDuration={300}>
						{#if options.now}
							<Tooltip.Root>
								<Tooltip.Trigger>
									{#snippet child({ props })}
										<InputGroup.Button
											{...props}
											size="icon-sm"
											variant="ghost"
											class="rounded-full"
											disabled={!ready}
											aria-label={nowLabel}
											onclick={() => void send(options.now ?? undefined)}
											>{#if sending === options.now}<LoaderCircle
													class="animate-spin motion-reduce:animate-none"
												/>{:else if options.now === 'interrupt'}<Zap />{:else}<Play />{/if}</InputGroup.Button
										>
									{/snippet}
								</Tooltip.Trigger>
								<Tooltip.Content side="top">{nowLabel}</Tooltip.Content>
							</Tooltip.Root>
						{/if}
						<Tooltip.Root>
							<Tooltip.Trigger>
								{#snippet child({ props })}
									<InputGroup.Button
										{...props}
										type="submit"
										size="icon-sm"
										variant="default"
										class="rounded-full"
										disabled={!ready}
										aria-label={m['tasks.live_send']()}
										>{#if sending === 'plain'}<LoaderCircle
												class="animate-spin motion-reduce:animate-none"
											/>{:else}<ArrowUp />{/if}</InputGroup.Button
									>
								{/snippet}
							</Tooltip.Trigger>
							<Tooltip.Content side="top">{m['tasks.live_send']()}</Tooltip.Content>
						</Tooltip.Root>
					</Tooltip.Provider>
				</InputGroup.Addon>
			</InputGroup.Root>
		</form>
	</div>
{/if}
