<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { m } from '$lib/features/i18n/messages';
	import {
		DEFAULT_PULL_REQUEST_TEMPLATE,
		PULL_REQUEST_TEMPLATE_PLACEHOLDERS,
		pullRequestTemplateProblem
	} from './pull-request-template';
	// inherits: a team, project or issue scope, where an empty template uses
	// the next wider one; the workspace's empty template is Kuayle's default.
	let {
		value = $bindable(''),
		inherits = false,
		disabled = false
	}: { value?: string; inherits?: boolean; disabled?: boolean } = $props();
	const id = $props.id();
	const problem = $derived(pullRequestTemplateProblem(value));
	const isDefault = $derived(value.trim() === DEFAULT_PULL_REQUEST_TEMPLATE.trim());
	const placeholders = PULL_REQUEST_TEMPLATE_PLACEHOLDERS.map((name) => `{{${name}}}`).join(', ');
</script>

<div class="min-w-0 space-y-1.5 sm:col-span-2">
	<div class="flex min-w-0 flex-wrap items-center justify-between gap-2">
		<Label for={id}>{m['tasks.pull_request_template']()}</Label>
		<Button type="button" variant="outline" size="sm" disabled={disabled || isDefault} onclick={() => (value = DEFAULT_PULL_REQUEST_TEMPLATE)}>{m['tasks.pull_request_template_reset']()}</Button>
	</div>
	<Textarea
		{id}
		bind:value
		{disabled}
		rows={8}
		spellcheck={false}
		aria-invalid={problem !== null}
		aria-describedby={`${id}-help${problem ? ` ${id}-error` : ''}`}
		placeholder={inherits ? m['tasks.pull_request_template_inherit']() : undefined}
		class="font-mono text-xs"
	/>
	<p id={`${id}-help`} class="text-xs text-muted-foreground">{m['tasks.pull_request_template_help']({ placeholders })}</p>
	{#if problem?.kind === 'too_long'}
		<p id={`${id}-error`} role="alert" class="text-sm text-destructive">{m['tasks.pull_request_template_too_long']()}</p>
	{:else if problem?.kind === 'unknown_placeholder'}
		<p id={`${id}-error`} role="alert" class="text-sm text-destructive">{m['tasks.pull_request_template_unknown']({ name: `{{${problem.name}}}` })}</p>
	{/if}
</div>
