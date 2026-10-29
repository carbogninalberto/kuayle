<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { ShieldCheck, PencilLine, CircleSlash } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Item from '$lib/components/ui/item/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import {
		assistantCapabilities,
		type AssistantCapabilities,
		type AssistantCapability
	} from '$lib/api/assistant-capabilities';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import AssistantSkills from './AssistantSkills.svelte';

	let { slug }: { slug: string } = $props();
	let capabilities = $state<AssistantCapabilities | null>(null);
	let failed = $state(false);
	let saving = $state<string | null>(null);

	// Tools with a chat label reuse it; others show their name. Descriptions
	// stay in English because they are what the model receives.
	const labels = m as unknown as Record<string, (() => string) | undefined>;
	const label = (tool: AssistantCapability) => labels[`assistant.tool_${tool.name}`]?.() ?? tool.name;
	// The tab is in the URL so chat cards can link to Skills.
	const tab = $derived(page.url.searchParams.get('tab') === 'skills' ? 'skills' : 'tools');
	function selectTab(value: string) {
		const url = new URL(page.url);
		url.search = value === 'skills' ? '?tab=skills' : '';
		void goto(url.pathname + url.search, { replaceState: true, noScroll: true, keepFocus: true });
	}
	const enabledCount = $derived(capabilities?.tools.filter((tool) => tool.enabled).length ?? 0);

	async function load() {
		failed = false;
		try {
			capabilities = await assistantCapabilities.list(slug);
		} catch (err) {
			failed = true;
			appToast.apiError(err, m['assistant_settings.failed_load']());
		}
	}

	async function toggle(tool: AssistantCapability, enabled: boolean) {
		if (!capabilities || saving) return;
		saving = tool.name;
		try {
			const updated = await assistantCapabilities.set(slug, tool.name, enabled);
			capabilities.tools = capabilities.tools.map((entry) => (entry.name === updated.name ? updated : entry));
			appToast.success(
				updated.enabled
					? m['assistant_settings.enabled_toast']({ name: label(updated) })
					: m['assistant_settings.disabled_toast']({ name: label(updated) })
			);
		} catch (err) {
			appToast.apiError(err, m['assistant_settings.failed_update']());
		} finally {
			saving = null;
		}
	}

	onMount(() => {
		void load();
	});
</script>

<div class="mx-auto w-full max-w-3xl space-y-6 px-4 py-8 sm:px-8 sm:py-10">
	<div class="space-y-2">
		<h1 class="text-2xl font-semibold">{m['assistant_settings.title']()}</h1>
		<p class="text-sm text-muted-foreground">{m['assistant_settings.desc']()}</p>
	</div>

	<Tabs.Root value={tab} onValueChange={selectTab} class="gap-4">
		<Tabs.List>
			<Tabs.Trigger value="tools">{m['assistant_settings.tab_tools']()}</Tabs.Trigger>
			<Tabs.Trigger value="skills">{m['assistant_settings.tab_skills']()}</Tabs.Trigger>
			<!-- Custom tools arrive in a later release. -->
			<Tabs.Trigger value="custom-tools" disabled title={m['assistant_settings.coming_soon']()}
				>{m['assistant_settings.tab_custom_tools']()}</Tabs.Trigger
			>
		</Tabs.List>

		<Tabs.Content value="tools" class="space-y-4">
			{#if capabilities}
				<section aria-labelledby="assistant-tools-heading" class="space-y-3">
					<div class="flex flex-wrap items-baseline justify-between gap-2">
						<h2 id="assistant-tools-heading" class="text-sm font-medium">{m['assistant_settings.tools_heading']()}</h2>
						<p class="text-xs text-muted-foreground tabular-nums">
							{m['assistant_settings.enabled_count']({
								enabled: String(enabledCount),
								total: String(capabilities.tools.length)
							})}
						</p>
					</div>
					<p class="text-xs text-muted-foreground">
						{capabilities.can_manage ? m['assistant_settings.tools_hint']() : m['assistant_settings.admin_only']()}
					</p>
					<Item.Group class="gap-0 rounded-xl border bg-card">
						{#each capabilities.tools as tool, index (tool.name)}
							{#if index > 0}<Item.Separator />{/if}
							<Item.Root role="listitem" aria-labelledby="assistant-tool-{tool.name}" class="items-start">
								<Item.Content class="min-w-0">
									<Item.Title class="flex flex-wrap items-center gap-2">
										<span id="assistant-tool-{tool.name}">{label(tool)}</span>
										{#if label(tool) !== tool.name}
											<code class="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground"
												>{tool.name}</code
											>
										{/if}
										{#if tool.requires_approval}
											<Badge variant="secondary" class="font-normal"
												><ShieldCheck class="size-3" />{m['assistant_settings.approval_required']()}</Badge
											>
										{:else if tool.changes_data}
											<Badge variant="outline" class="font-normal"
												><PencilLine class="size-3" />{m['assistant_settings.changes_data']()}</Badge
											>
										{/if}
										{#if !tool.available}
											<Badge variant="outline" class="font-normal text-muted-foreground"
												><CircleSlash class="size-3" />{m['assistant_settings.unavailable']()}</Badge
											>
										{/if}
									</Item.Title>
									<Item.Description class="line-clamp-none text-xs break-words">{tool.description}</Item.Description>
								</Item.Content>
								<Item.Actions>
									{#if capabilities.can_manage}
										<Switch
											checked={tool.enabled}
											disabled={saving !== null}
											aria-label={m['assistant_settings.toggle_label']({ name: label(tool) })}
											onCheckedChange={(enabled) => toggle(tool, enabled)}
										/>
									{:else}
										<Badge variant={tool.enabled ? 'secondary' : 'outline'} class="font-normal">
											{tool.enabled ? m['assistant_settings.state_enabled']() : m['assistant_settings.state_disabled']()}
										</Badge>
									{/if}
								</Item.Actions>
							</Item.Root>
						{/each}
					</Item.Group>
				</section>
			{:else if failed}
				<Button variant="outline" onclick={load}>{m['assistant_settings.reload']()}</Button>
			{:else}
				<p role="status" class="text-sm text-muted-foreground">{m['assistant_settings.loading']()}</p>
			{/if}
		</Tabs.Content>
		<Tabs.Content value="skills" class="space-y-4"><AssistantSkills {slug} /></Tabs.Content>
		<Tabs.Content value="custom-tools"></Tabs.Content>
	</Tabs.Root>
</div>
