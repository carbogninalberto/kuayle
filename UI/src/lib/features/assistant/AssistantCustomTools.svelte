<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import {
		ArrowLeft,
		Bot,
		Plus,
		UserRound,
		PencilLine,
		Trash2,
		History,
		Play,
		ShieldCheck,
		Zap,
		Eye,
		ListChecks
	} from '@lucide/svelte';
	import { Alert, AlertDescription, AlertAction } from '$lib/components/ui/alert/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import * as Item from '$lib/components/ui/item/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import ConfirmDialog from '$lib/components/shared/ConfirmDialog.svelte';
	import {
		assistantTools,
		type AssistantCustomTool,
		type AssistantCustomToolContent,
		type AssistantCustomToolList,
		type AssistantCustomToolVersion,
		type AssistantToolInterpreter,
		type AssistantToolRun
	} from '$lib/api/assistant-tools';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import CustomToolEditorDialog from './CustomToolEditorDialog.svelte';
	import CustomToolReviewDialog from './CustomToolReviewDialog.svelte';
	import CustomToolTestRunDialog from './CustomToolTestRunDialog.svelte';
	import ToolRunView, { runStatusLabel } from './ToolRunView.svelte';
	import { runModeLabel } from './custom-tool-labels';

	// Settings → Assistant → Custom tools. Write and test-run controls are shown
	// to owners and admins (`can_manage`); the server enforces every one.
	let { slug }: { slug: string } = $props();

	let list = $state<AssistantCustomToolList | null>(null);
	let failed = $state(false);
	let toggling = $state<string | null>(null);

	let tool = $state<AssistantCustomTool | null>(null);
	let versions = $state<AssistantCustomToolVersion[]>([]);
	let runs = $state<AssistantToolRun[]>([]);
	let expanded = $state<Record<number, boolean>>({});
	let openRun = $state<string | null>(null);
	let busy = $state(false);

	let creating = $state(false);
	let editing = $state(false);
	let testing = $state(false);
	let reviewing = $state<AssistantCustomToolVersion | null>(null);
	let reviewOpen = $state(false);
	let deleting = $state(false);

	// The open tool lives in the URL so chat cards can deep-link to it.
	const selected = $derived(page.url.searchParams.get('tool'));
	const manage = $derived(list?.can_manage ?? false);
	const interpreters = $derived<AssistantToolInterpreter[]>(list?.interpreters ?? ['sh']);
	// The draft waiting for activation: a pending edit, or a draft tool itself.
	const draft = $derived.by(() => {
		if (!tool) return null;
		const version = tool.status === 'draft' ? tool.current_version : tool.pending_version;
		return versions.find((entry) => entry.version === version) ?? null;
	});
	const content = (from: AssistantCustomTool | AssistantCustomToolVersion): AssistantCustomToolContent => ({
		name: from.name,
		description: from.description,
		parameters: from.parameters,
		script: from.script ?? '',
		interpreter: from.interpreter,
		run_mode: from.run_mode,
		read_only: from.read_only,
		timeout_seconds: from.timeout_seconds
	});
	// Editing continues from a pending draft when there is one.
	const editFrom = $derived(tool ? content(draft && tool.status === 'active' ? draft : tool) : null);
	const parameters = $derived(Object.entries(tool?.parameters.properties ?? {}));
	const required = $derived(new Set(tool?.parameters.required ?? []));

	const when = (at: string) => new Date(at).toLocaleString(getLocale(), { dateStyle: 'medium', timeStyle: 'short' });
	const activatable = (entry: AssistantCustomToolVersion) =>
		!!tool &&
		entry.status === 'draft' &&
		(entry.version > tool.current_version || (tool.status === 'draft' && entry.version === tool.current_version));
	const typeText = (field: AssistantCustomTool['parameters']['properties'][string]) =>
		field.enum ? field.enum.join(' | ') : field.type;
	const bounds = (field: AssistantCustomTool['parameters']['properties'][string]) =>
		[
			field.minLength !== undefined ? `minLength ${field.minLength}` : '',
			field.maxLength !== undefined ? `maxLength ${field.maxLength}` : '',
			field.minimum !== undefined ? `minimum ${field.minimum}` : '',
			field.maximum !== undefined ? `maximum ${field.maximum}` : ''
		]
			.filter(Boolean)
			.join(' · ');

	function review(version: AssistantCustomToolVersion) {
		reviewing = version;
		reviewOpen = true;
	}

	async function load() {
		failed = false;
		try {
			list = await assistantTools.list(slug);
		} catch (err) {
			failed = true;
			appToast.apiError(err, m['assistant_custom_tools.failed_load']());
		}
	}

	let detailRequest = 0;
	async function loadDetail(id: string) {
		const request = ++detailRequest;
		try {
			const [one, history, ran] = await Promise.all([
				assistantTools.get(slug, id),
				assistantTools.versions(slug, id),
				assistantTools.runs(slug, id)
			]);
			if (request !== detailRequest) return;
			tool = one;
			versions = history.versions;
			runs = ran.runs;
		} catch (err) {
			if (request !== detailRequest) return;
			appToast.apiError(err, m['assistant_custom_tools.failed_load']());
			void open(null);
		}
	}

	async function loadRuns() {
		if (!tool) return;
		const id = tool.id;
		try {
			const ran = await assistantTools.runs(slug, id);
			if (tool?.id === id) runs = ran.runs;
		} catch {
			// The history stays as it was; the next refresh retries.
		}
	}

	$effect(() => {
		const id = selected;
		tool = null;
		versions = [];
		runs = [];
		expanded = {};
		openRun = null;
		if (id) void loadDetail(id);
		else detailRequest++;
	});

	function open(id: string | null) {
		const url = new URL(page.url);
		url.searchParams.set('tab', 'custom-tools');
		if (id) url.searchParams.set('tool', id);
		else url.searchParams.delete('tool');
		return goto(url.pathname + url.search, { noScroll: true, keepFocus: !id });
	}

	// After a write, refresh the list and the open tool.
	async function refresh(updated: AssistantCustomTool) {
		await load();
		if (selected === updated.id) await loadDetail(updated.id);
		else await open(updated.id);
	}

	async function toggle(entry: AssistantCustomTool, enabled: boolean) {
		if (toggling) return;
		toggling = entry.id;
		try {
			const updated = await assistantTools.setEnabled(slug, entry.id, enabled);
			if (list) list.tools = list.tools.map((one) => (one.id === updated.id ? { ...one, ...updated } : one));
			if (tool?.id === updated.id) tool = { ...tool, enabled: updated.enabled };
			appToast.success(
				updated.enabled
					? m['assistant_custom_tools.enabled_toast']({ name: updated.name })
					: m['assistant_custom_tools.disabled_toast']({ name: updated.name })
			);
		} catch (err) {
			appToast.apiError(err, m['assistant_custom_tools.failed_save']());
		} finally {
			toggling = null;
		}
	}

	async function restore(version: number) {
		if (!tool || busy) return;
		busy = true;
		try {
			const updated = await assistantTools.restore(slug, tool.id, version);
			appToast.success(
				m['assistant_custom_tools.restored_toast']({ version: String(version), next: String(updated.latest_version) })
			);
			await refresh(updated);
		} catch (err) {
			appToast.apiError(err, m['assistant_custom_tools.failed_save']());
		} finally {
			busy = false;
		}
	}

	async function remove() {
		if (!tool || busy) return;
		const { id, name } = tool;
		busy = true;
		try {
			await assistantTools.remove(slug, id);
			deleting = false;
			appToast.success(m['assistant_custom_tools.deleted_toast']({ name }));
			await load();
			await open(null);
		} catch (err) {
			appToast.apiError(err, m['assistant_custom_tools.failed_save']());
		} finally {
			busy = false;
		}
	}

	onMount(() => {
		void load();
	});
</script>

{#snippet badges(entry: AssistantCustomTool)}
	<Badge variant={entry.status === 'active' ? 'secondary' : 'outline'} class="font-normal"
		>{entry.status === 'active'
			? m['assistant_custom_tools.status_active']()
			: m['assistant_custom_tools.status_draft']()}</Badge
	>
	<Badge variant="outline" class="font-normal tabular-nums"
		>{m['assistant_custom_tools.version_short']({ version: String(entry.current_version) })}</Badge
	>
	{#if entry.pending_version}
		<Badge variant="outline" class="border-[var(--status-warning)]/40 font-normal text-[var(--status-warning)]"
			>{m['assistant_custom_tools.pending']({ version: String(entry.pending_version) })}</Badge
		>
	{/if}
	<Badge variant="outline" class="font-normal"
		>{#if entry.run_mode === 'auto'}<Zap class="size-3" />{:else}<ShieldCheck class="size-3" />{/if}{runModeLabel(
			entry.run_mode
		)}</Badge
	>
	{#if entry.read_only}
		<Badge variant="outline" class="font-normal"><Eye class="size-3" />{m['assistant_custom_tools.read_only']()}</Badge>
	{/if}
	<Badge variant="outline" class="font-mono font-normal">{entry.interpreter}</Badge>
	<Badge variant="outline" class="font-normal text-muted-foreground"
		>{#if entry.author_kind === 'assistant'}<Bot class="size-3" />{m[
				'assistant_custom_tools.by_assistant'
			]()}{:else}<UserRound class="size-3" />{m['assistant_custom_tools.by_user']()}{/if}</Badge
	>
{/snippet}

{#snippet enabledControl(entry: AssistantCustomTool)}
	{#if manage}
		<Switch
			checked={entry.enabled}
			disabled={toggling !== null}
			aria-label={m['assistant_custom_tools.toggle_label']({ name: entry.name })}
			onCheckedChange={(enabled) => toggle(entry, enabled)}
		/>
	{:else}
		<Badge variant={entry.enabled ? 'secondary' : 'outline'} class="font-normal">
			{entry.enabled ? m['assistant_custom_tools.state_enabled']() : m['assistant_custom_tools.state_disabled']()}
		</Badge>
	{/if}
{/snippet}

{#if selected}
	<div class="min-w-0 space-y-5">
		<Button variant="ghost" size="sm" class="-ml-2" onclick={() => open(null)}
			><ArrowLeft />{m['assistant_custom_tools.back']()}</Button
		>
		{#if tool}
			<section aria-labelledby="assistant-tool-heading" class="min-w-0 space-y-3">
				<div class="flex flex-wrap items-start justify-between gap-3">
					<div class="min-w-0 space-y-2">
						<h2 id="assistant-tool-heading" class="font-mono text-lg font-semibold break-words">{tool.name}</h2>
						<code class="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] break-all text-muted-foreground"
							>custom_{tool.name}</code
						>
						<div class="flex flex-wrap items-center gap-1.5">{@render badges(tool)}</div>
					</div>
					<div class="flex shrink-0 items-center gap-2">{@render enabledControl(tool)}</div>
				</div>
				<p class="text-sm break-words text-muted-foreground">{tool.description}</p>
				<p class="text-xs text-muted-foreground">
					{m['assistant_custom_tools.updated']({ time: when(tool.updated_at) })} · {m[
						'assistant_custom_tools.timeout_value'
					]({ seconds: String(tool.timeout_seconds) })}
				</p>
				{#if manage}
					<div class="flex flex-wrap gap-2">
						<Button variant="outline" size="sm" onclick={() => (editing = true)}
							><PencilLine />{m['assistant_custom_tools.edit']()}</Button
						>
						<Button variant="outline" size="sm" onclick={() => (testing = true)}
							><Play />{m['assistant_custom_tools.test_run']()}</Button
						>
						<Button variant="outline" size="sm" class="text-destructive" onclick={() => (deleting = true)}
							><Trash2 />{m['assistant_custom_tools.delete']()}</Button
						>
					</div>
				{/if}
				{#if draft}
					<Alert class="border-[var(--status-warning)]/40 bg-[var(--status-warning)]/5">
						<AlertDescription class="text-foreground">
							{tool.status === 'draft'
								? m['assistant_custom_tools.draft_tool']()
								: m['assistant_custom_tools.draft_pending']({ version: String(draft.version) })}
						</AlertDescription>
						{#if manage}
							<AlertAction>
								<Button size="sm" onclick={() => review(draft)}>{m['assistant_custom_tools.review_draft']()}</Button>
							</AlertAction>
						{/if}
					</Alert>
				{/if}
				<h3 class="text-sm font-medium">{m['assistant_custom_tools.field_parameters']()}</h3>
				{#if parameters.length}
					<ul class="divide-y rounded-xl border bg-card text-xs">
						{#each parameters as [name, field] (name)}
							<li class="space-y-0.5 px-3 py-2 break-words">
								<p>
									<code class="font-mono font-medium">{name}</code>
									<span class="font-mono text-muted-foreground">{typeText(field)}</span>
									{#if required.has(name)}<Badge variant="outline" class="ml-1 font-normal"
											>{m['assistant_custom_tools.param_required']()}</Badge
										>{/if}
								</p>
								{#if field.description}<p class="text-muted-foreground">{field.description}</p>{/if}
								{#if bounds(field)}<p class="font-mono text-muted-foreground">{bounds(field)}</p>{/if}
							</li>
						{/each}
					</ul>
				{:else}
					<p class="text-xs text-muted-foreground">{m['assistant_custom_tools.no_parameters']()}</p>
				{/if}
				<CodeBlock.Root size="sm"
					><CodeBlock.Header
						><CodeBlock.Title>{m['assistant_custom_tools.raw_schema']()}</CodeBlock.Title></CodeBlock.Header
					><CodeBlock.Content
						code={JSON.stringify(tool.parameters, null, 2)}
						wrap
						class="max-h-48 text-xs"
					/></CodeBlock.Root
				>
				<CodeBlock.Root size="sm"
					><CodeBlock.Header
						><CodeBlock.Title
							>{m['assistant_custom_tools.script_title']({ interpreter: tool.interpreter })}</CodeBlock.Title
						></CodeBlock.Header
					><CodeBlock.Content code={tool.script ?? ''} wrap class="max-h-96 text-xs" /></CodeBlock.Root
				>
			</section>

			<section aria-labelledby="assistant-tool-history" class="space-y-3">
				<h3 id="assistant-tool-history" class="flex items-center gap-2 text-sm font-medium">
					<History class="size-4" />{m['assistant_custom_tools.history']()}
				</h3>
				<Item.Group aria-labelledby="assistant-tool-history" class="gap-0 rounded-xl border bg-card">
					{#each versions as entry, index (entry.version)}
						{#if index > 0}<Item.Separator />{/if}
						<Item.Root role="listitem" aria-labelledby="assistant-tool-version-{entry.version}" class="flex-wrap items-start">
							<Item.Content class="min-w-0">
								<Item.Title class="flex flex-wrap items-center gap-2">
									<span id="assistant-tool-version-{entry.version}"
										>{m['assistant_custom_tools.version_label']({ version: String(entry.version) })}</span
									>
									<Badge variant={entry.status === 'active' ? 'secondary' : 'outline'} class="font-normal"
										>{entry.status === 'active'
											? m['assistant_custom_tools.status_active']()
											: m['assistant_custom_tools.status_draft']()}</Badge
									>
									{#if entry.version === tool.current_version && tool.status === 'active'}
										<Badge variant="outline" class="font-normal">{m['assistant_custom_tools.in_effect']()}</Badge>
									{/if}
								</Item.Title>
								<Item.Description class="line-clamp-none text-xs">
									{entry.author_kind === 'assistant'
										? m['assistant_custom_tools.by_assistant']()
										: m['assistant_custom_tools.by_user']()}
									· {runModeLabel(entry.run_mode)} · {when(entry.created_at)}
								</Item.Description>
							</Item.Content>
							<Item.Actions class="flex-wrap">
								<Button
									variant="link"
									size="sm"
									aria-expanded={expanded[entry.version] ?? false}
									aria-controls="assistant-tool-version-{entry.version}-script"
									onclick={() => (expanded[entry.version] = !expanded[entry.version])}
									>{expanded[entry.version]
										? m['assistant_custom_tools.hide_script']()
										: m['assistant_custom_tools.show_script']()}</Button
								>
								{#if manage && activatable(entry)}
									<Button size="sm" variant="outline" disabled={busy} onclick={() => review(entry)}
										>{m['assistant_custom_tools.activate']()}</Button
									>
								{/if}
								{#if manage && entry.version !== tool.current_version}
									<Button size="sm" variant="outline" disabled={busy} onclick={() => restore(entry.version)}
										>{m['assistant_custom_tools.restore']()}</Button
									>
								{/if}
							</Item.Actions>
							{#if expanded[entry.version]}
								<div id="assistant-tool-version-{entry.version}-script" class="min-w-0 basis-full space-y-1">
									{#if entry.name !== tool.name || entry.description !== tool.description}
										<p class="text-xs break-words text-muted-foreground">{entry.name} — {entry.description}</p>
									{/if}
									<CodeBlock.Root size="sm"
										><CodeBlock.Content code={entry.script} wrap class="max-h-64 text-xs" /></CodeBlock.Root
									>
								</div>
							{/if}
						</Item.Root>
					{/each}
				</Item.Group>
			</section>

			<section aria-labelledby="assistant-tool-runs" class="space-y-3">
				<h3 id="assistant-tool-runs" class="flex items-center gap-2 text-sm font-medium">
					<ListChecks class="size-4" />{m['assistant_custom_tools.runs']()}
				</h3>
				{#if runs.length}
					<Item.Group aria-labelledby="assistant-tool-runs" class="gap-0 rounded-xl border bg-card">
						{#each runs as entry, index (entry.id)}
							{#if index > 0}<Item.Separator />{/if}
							<Item.Root role="listitem" aria-labelledby="assistant-tool-run-{entry.id}" class="flex-wrap items-start">
								<Item.Content class="min-w-0">
									<Item.Title class="flex flex-wrap items-center gap-2">
										<span id="assistant-tool-run-{entry.id}">{runStatusLabel(entry.status)}</span>
										<Badge variant="outline" class="font-normal tabular-nums"
											>{m['assistant_custom_tools.version_short']({ version: String(entry.tool_version) })}</Badge
										>
										{#if entry.test}<Badge variant="outline" class="font-normal"
												>{m['assistant_custom_tools.run_test']()}</Badge
											>{/if}
										{#if entry.exit_code !== null}<span class="text-xs text-muted-foreground tabular-nums"
												>{m['assistant_custom_tools.exit_code']({ code: String(entry.exit_code) })}</span
											>{/if}
									</Item.Title>
									<Item.Description class="line-clamp-none text-xs">{when(entry.created_at)}</Item.Description>
								</Item.Content>
								<Item.Actions>
									<Button
										variant="link"
										size="sm"
										aria-expanded={openRun === entry.id}
										aria-controls="assistant-tool-run-{entry.id}-detail"
										onclick={() => (openRun = openRun === entry.id ? null : entry.id)}
										>{openRun === entry.id
											? m['assistant_custom_tools.hide_details']()
											: m['assistant_custom_tools.details']()}</Button
									>
								</Item.Actions>
								{#if openRun === entry.id}
									<div id="assistant-tool-run-{entry.id}-detail" class="min-w-0 basis-full">
										<ToolRunView
											status={entry.status}
											exitCode={entry.exit_code}
											output={entry.output}
											truncated={entry.output_truncated}
											error={entry.error}
											durationMs={entry.duration_ms}
											inputs={entry.inputs}
										/>
									</div>
								{/if}
							</Item.Root>
						{/each}
					</Item.Group>
				{:else}
					<p class="rounded-xl border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
						{m['assistant_custom_tools.runs_empty']()}
					</p>
				{/if}
			</section>

			{#if manage}
				<CustomToolEditorDialog bind:open={editing} {slug} {interpreters} {tool} initial={editFrom} onsaved={refresh} />
				<CustomToolTestRunDialog bind:open={testing} {slug} {tool} {versions} onran={loadRuns} />
				{#if reviewing}
					<CustomToolReviewDialog bind:open={reviewOpen} {slug} {tool} draft={reviewing} onactivated={refresh} />
				{/if}
			{/if}
			{#if deleting}
				<ConfirmDialog
					title={m['assistant_custom_tools.delete_title']({ name: tool.name })}
					message={m['assistant_custom_tools.delete_message']()}
					confirmLabel={m['assistant_custom_tools.delete']()}
					onconfirm={remove}
					oncancel={() => (deleting = false)}
				/>
			{/if}
		{:else}
			<p role="status" class="text-sm text-muted-foreground">{m['assistant_custom_tools.loading']()}</p>
		{/if}
	</div>
{:else if list}
	<section aria-labelledby="assistant-custom-tools-heading" class="min-w-0 space-y-3">
		<div class="flex flex-wrap items-center justify-between gap-2">
			<h2 id="assistant-custom-tools-heading" class="text-sm font-medium">{m['assistant_custom_tools.heading']()}</h2>
			{#if manage}
				<Button size="sm" onclick={() => (creating = true)}><Plus />{m['assistant_custom_tools.new']()}</Button>
			{/if}
		</div>
		<p class="text-xs text-muted-foreground">
			{manage ? m['assistant_custom_tools.hint']() : m['assistant_custom_tools.admin_only']()}
		</p>
		{#if list.tools.length}
			<Item.Group aria-label={m['assistant_custom_tools.heading']()} class="gap-0 rounded-xl border bg-card">
				{#each list.tools as entry, index (entry.id)}
					{#if index > 0}<Item.Separator />{/if}
					<Item.Root role="listitem" aria-labelledby="assistant-custom-tool-{entry.id}" class="items-start">
						<Item.Content class="min-w-0 gap-1.5">
							<Item.Title class="flex max-w-full flex-wrap items-center gap-2">
								<Button
									id="assistant-custom-tool-{entry.id}"
									variant="link"
									class="h-auto max-w-full justify-start p-0 text-left font-mono text-sm font-medium break-all whitespace-normal text-foreground"
									onclick={() => open(entry.id)}>{entry.name}</Button
								>
								<code class="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] break-all text-muted-foreground"
									>custom_{entry.name}</code
								>
							</Item.Title>
							<Item.Description class="line-clamp-2 text-xs break-words">{entry.description}</Item.Description>
							<div class="flex flex-wrap items-center gap-1.5">{@render badges(entry)}</div>
							<p class="text-xs text-muted-foreground">
								{m['assistant_custom_tools.updated']({ time: when(entry.updated_at) })}
							</p>
						</Item.Content>
						<Item.Actions>{@render enabledControl(entry)}</Item.Actions>
					</Item.Root>
				{/each}
			</Item.Group>
		{:else}
			<p class="rounded-xl border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
				{m['assistant_custom_tools.empty']()}
			</p>
		{/if}
	</section>
	{#if manage}
		<CustomToolEditorDialog bind:open={creating} {slug} {interpreters} onsaved={refresh} />
	{/if}
{:else if failed}
	<Button variant="outline" onclick={load}>{m['assistant_custom_tools.reload']()}</Button>
{:else}
	<p role="status" class="text-sm text-muted-foreground">{m['assistant_custom_tools.loading']()}</p>
{/if}
