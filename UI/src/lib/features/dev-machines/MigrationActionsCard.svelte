<script lang="ts">
	import { Database, LoaderCircle, Pencil, Plus, Trash2 } from '@lucide/svelte';
	import * as Card from '$lib/components/ui/card/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import {
		createMachineMigrationAction,
		deleteMachineMigrationAction,
		listMachineMigrationActions,
		updateMachineMigrationAction
	} from '$lib/api/dev-machines';
	import type { MigrationAction } from '$lib/types/dev-machine';
	import { appToast, getErrorMessage } from '$lib/features/toast/toast';
	import { getLocale, m } from '$lib/features/i18n/messages';

	let {
		slug,
		machineId,
		canManage
	}: {
		slug: string;
		machineId: string;
		// Only the machine's creator or a workspace owner/admin configures migrations.
		canManage: boolean;
	} = $props();

	const POLL_MS = 10_000;
	const titleId = $props.id();

	let actions = $state<MigrationAction[]>([]);
	let loaded = $state(false);
	let failed = $state(false);
	let card = $state<HTMLElement | null>(null);
	let visible = $state(true);
	let open = $state(false);
	let editing = $state<MigrationAction | null>(null);
	let name = $state('');
	let description = $state('');
	let command = $state('');
	let secret = $state('');
	let saving = $state(false);
	let formError = $state('');
	let removing = $state<MigrationAction | null>(null);
	let removeOpen = $state(false);
	let request = 0;

	// The API requires every field but the secret on edit, which keeps the stored one.
	const canSave = $derived(
		!!name.trim() && !!description.trim() && !!command.trim() && (!!editing || !!secret.trim())
	);

	async function load(target: string) {
		const id = ++request;
		try {
			const list = await listMachineMigrationActions(slug, target);
			if (id !== request || target !== machineId) return;
			actions = list.actions ?? [];
			failed = false;
		} catch {
			// The next poll retries; a transient failure keeps the last list.
			if (id === request) failed = !loaded;
		} finally {
			if (id === request) loaded = true;
		}
	}

	$effect(() => {
		const target = machineId;
		actions = [];
		loaded = false;
		failed = false;
		void load(target);
	});

	// Poll only while the card is on screen and the tab is visible.
	$effect(() => {
		if (!card || typeof IntersectionObserver === 'undefined') return;
		const observer = new IntersectionObserver(([entry]) => (visible = entry.isIntersecting));
		observer.observe(card);
		return () => observer.disconnect();
	});
	$effect(() => {
		if (!visible) return;
		const target = machineId;
		const timer = setInterval(() => {
			if (document.visibilityState === 'visible') void load(target);
		}, POLL_MS);
		return () => clearInterval(timer);
	});

	function startAdd() {
		editing = null;
		name = '';
		description = '';
		command = '';
		secret = '';
		formError = '';
		open = true;
	}

	function startEdit(action: MigrationAction) {
		editing = action;
		name = action.name;
		description = action.description ?? '';
		command = action.command;
		secret = '';
		formError = '';
		open = true;
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!canSave || saving) return;
		saving = true;
		formError = '';
		const input = {
			name: name.trim(),
			description: description.trim(),
			command: command.trim(),
			// Omitted on edit: the stored secret stays.
			secret: editing || !secret.trim() ? undefined : secret
		};
		try {
			const saved = editing
				? await updateMachineMigrationAction(slug, machineId, editing.id, input)
				: await createMachineMigrationAction(slug, machineId, input);
			actions = [...actions.filter((a) => a.id !== saved.id), saved].sort((a, b) =>
				a.name.localeCompare(b.name)
			);
			open = false;
		} catch (error) {
			formError = getErrorMessage(error, m['machines.migration_error']());
		} finally {
			saving = false;
		}
	}

	function askRemove(action: MigrationAction) {
		removing = action;
		removeOpen = true;
	}

	async function remove() {
		const action = removing;
		removeOpen = false;
		if (!action) return;
		try {
			await deleteMachineMigrationAction(slug, machineId, action.id);
			actions = actions.filter((a) => a.id !== action.id);
		} catch (error) {
			appToast.apiError(error, m['machines.migration_error']());
		}
	}

	const updated = (action: MigrationAction) =>
		m['machines.migration_updated']({ date: new Date(action.updated_at).toLocaleString(getLocale()) });
</script>

<Card.Root bind:ref={card} role="region" aria-labelledby={titleId}>
	<Card.Header>
		<Card.Title id={titleId} class="flex items-center gap-2 text-sm"
			><Database class="size-4 text-muted-foreground" />{m['machines.migrations']()}<Badge variant="secondary"
				>{actions.length}</Badge
			></Card.Title
		>
		<Card.Description>{m['machines.migrations_desc']()}</Card.Description>
	</Card.Header>
	<Card.Content class="min-w-0 space-y-3">
		{#if actions.length}
			<ul class="divide-y rounded-lg border">
				{#each actions as action (action.id)}
					<li class="min-w-0 space-y-1.5 px-3 py-2">
						<div class="flex min-w-0 items-center gap-2">
							<span class="truncate font-mono text-sm font-medium">{action.name}</span>
							<span class="ml-auto shrink-0 text-xs text-muted-foreground">{updated(action)}</span>
							{#if canManage}
								<div class="flex shrink-0 items-center gap-0.5">
									<Button
										variant="ghost"
										size="icon-sm"
										aria-label={m['machines.migration_edit']({ name: action.name })}
										onclick={() => startEdit(action)}><Pencil /></Button
									>
									<Button
										variant="ghost"
										size="icon-sm"
										class="text-destructive hover:text-destructive"
										aria-label={m['machines.migration_remove']({ name: action.name })}
										onclick={() => askRemove(action)}><Trash2 /></Button
									>
								</div>
							{/if}
						</div>
						{#if action.description}
							<p class="text-xs text-muted-foreground">{action.description}</p>
						{/if}
						<!-- The exact command the assistant runs, as sh -c. -->
						<CodeBlock.Root size="sm"
							><CodeBlock.Content code={action.command} wrap class="max-h-24 text-xs" /></CodeBlock.Root
						>
					</li>
				{/each}
			</ul>
		{:else if loaded && failed}
			<p role="alert" class="text-xs text-destructive">{m['machines.migration_error']()}</p>
		{:else if loaded}
			<p class="text-xs text-muted-foreground">{m['machines.migrations_empty']()}</p>
		{/if}
		{#if canManage}
			<Button variant="outline" size="sm" onclick={() => startAdd()}><Plus />{m['machines.migration_add']()}</Button>
		{/if}
	</Card.Content>
</Card.Root>

<Dialog.Root bind:open={open}>
	<Dialog.Content>
		<form onsubmit={save} class="space-y-4">
			<Dialog.Header>
				<Dialog.Title
					>{editing
						? m['machines.migration_edit_title']({ name: editing.name })
						: m['machines.migration_add']()}</Dialog.Title
				>
				<Dialog.Description>{m['machines.migration_secret_hint']()}</Dialog.Description>
			</Dialog.Header>
			<div class="space-y-1.5">
				<Label for={`${titleId}-name`}>{m['machines.migration_name']()}</Label>
				<Input id={`${titleId}-name`} bind:value={name} maxlength={100} autocomplete="off" required />
			</div>
			<div class="space-y-1.5">
				<Label for={`${titleId}-description`}>{m['machines.migration_description']()}</Label>
				<Input
					id={`${titleId}-description`}
					bind:value={description}
					maxlength={500}
					autocomplete="off"
					required
				/>
			</div>
			<div class="space-y-1.5">
				<Label for={`${titleId}-command`}>{m['machines.migration_command']()}</Label>
				<Textarea
					id={`${titleId}-command`}
					bind:value={command}
					class="font-mono text-xs"
					autocomplete="off"
					required
				/>
				<p class="text-xs text-muted-foreground">{m['machines.migration_command_hint']()}</p>
			</div>
			<div class="space-y-1.5">
				<Label for={`${titleId}-secret`}>{m['machines.migration_secret']()}</Label>
				<Input
					id={`${titleId}-secret`}
					type="password"
					bind:value={secret}
					autocomplete="off"
					placeholder={editing ? m['machines.migration_secret_keep']() : undefined}
				/>
				<p class="text-xs text-muted-foreground">{m['machines.migration_secret_hint']()}</p>
			</div>
			{#if formError}<p role="alert" class="text-xs text-destructive">{formError}</p>{/if}
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (open = false)}>{m['common.cancel']()}</Button>
				<Button type="submit" disabled={!canSave || saving}
					>{#if saving}<LoaderCircle class="animate-spin" />{/if}{editing ? m['common.save']() : m['machines.migration_add']()}</Button
				>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>

<AlertDialog.Root bind:open={removeOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title
				>{m['machines.migration_remove_title']({ name: removing ? removing.name : '' })}</AlertDialog.Title
			>
			<AlertDialog.Description>{m['machines.migration_remove_desc']()}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={() => void remove()}>{m['common.remove']()}</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
