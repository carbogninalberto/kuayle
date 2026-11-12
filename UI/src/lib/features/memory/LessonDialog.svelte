<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import type { Lesson } from '$lib/api/memory';
	import { m } from '$lib/features/i18n/messages';

	// One lesson's text: written by hand for a repository, edited, or edited
	// on the way to approving a proposal. The primary button says which.
	let {
		lesson,
		repositories = [],
		maxLength,
		busy = false,
		onsubmit,
		onclose
	}: {
		// The lesson being edited or approved; absent when one is added.
		lesson?: Lesson;
		// Where a new lesson can be written.
		repositories?: string[];
		maxLength: number;
		busy?: boolean;
		onsubmit: (text: string, repository: string) => void;
		onclose: () => void;
	} = $props();

	// svelte-ignore state_referenced_locally
	let text = $state(lesson?.text ?? ''),
		// svelte-ignore state_referenced_locally
		repository = $state(lesson?.repository ?? repositories[0] ?? '');
	const title = $derived(
		!lesson ? m['lessons.add']() : lesson.state === 'proposed' ? m['workflows.task.approve']() : m['common.edit']()
	);
	const action = $derived(
		!lesson ? m['team_settings.add']() : lesson.state === 'proposed' ? m['workflows.task.approve']() : m['common.save']()
	);
	const ready = $derived(!!text.trim() && !!repository && !busy);
	function submit(event: SubmitEvent) {
		event.preventDefault();
		if (ready) onsubmit(text.trim(), repository);
	}
</script>

<Dialog.Root open onOpenChange={(open) => !open && onclose()}>
	<Dialog.Content class="sm:max-w-lg">
		<form onsubmit={submit} class="space-y-4" data-testid="lesson-form">
			<Dialog.Header>
				<Dialog.Title>{title}</Dialog.Title>
				<Dialog.Description class="font-mono text-xs break-all"
					>{lesson || repositories.length === 1 ? repository : ''}</Dialog.Description
				>
			</Dialog.Header>
			{#if !lesson && repositories.length > 1}<div class="space-y-1.5">
					<Label for="lesson-repository">{m['machines.repository']()}</Label>
					<SelectField
						id="lesson-repository"
						bind:value={repository}
						options={repositories.map((name) => ({ value: name, label: name }))}
					/>
				</div>{/if}
			<div class="space-y-1.5">
				<Label for="lesson-text" class={lesson || repositories.length < 2 ? 'sr-only' : ''}>{m['lessons.lesson']()}</Label>
				<Textarea
					id="lesson-text"
					bind:value={text}
					rows={4}
					maxlength={maxLength}
					required
					onkeydown={(event) => {
						if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) event.currentTarget.form?.requestSubmit();
					}}
				/>
			</div>
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={onclose}>{m['common.cancel']()}</Button>
				<Button type="submit" disabled={!ready}>{action}</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
