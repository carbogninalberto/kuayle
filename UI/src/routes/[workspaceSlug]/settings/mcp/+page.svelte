<script lang="ts">
	import { SettingsPageHeader, SettingsSection } from '$lib/components/settings';
	import { page } from '$app/state';
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { CODEX_TOKEN_VARIABLE, TOKEN_PLACEHOLDER, mcpClientSetups, mcpServerUrl } from '$lib/features/mcp/setup';
	import { m } from '$lib/features/i18n/messages';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const url = $derived(mcpServerUrl(page.url.origin, slug));
	const setups = $derived(mcpClientSetups(url));
	let client = $state('claude-code');
</script>

{#snippet snippet(label: string, code: string)}
	<CodeBlock.Root aria-label={label}>
		<CodeBlock.Actions>
			<CodeBlock.CopyButton value={code} aria-label={m['settings.tokens.copy']()} />
		</CodeBlock.Actions>
		<CodeBlock.Content {code} wrap />
	</CodeBlock.Root>
{/snippet}

<div class="settings-page mx-auto w-full max-w-3xl space-y-6 px-4 py-8 sm:px-8 sm:py-10">
	<SettingsPageHeader title={m['settings.mcp.title']()} description={m['settings.mcp.desc']()} />

	<SettingsSection title={m['settings.mcp.url']()}>
		{@render snippet(m['settings.mcp.url'](), url)}
		<p class="mt-3 text-sm text-muted-foreground">
			{m['settings.mcp.token_hint']({ placeholder: TOKEN_PLACEHOLDER })}
			<a href="/{slug}/settings/tokens" class="font-medium text-foreground underline underline-offset-4">
				{m['settings.mcp.token_link']()}
			</a>
		</p>
	</SettingsSection>

	<Tabs.Root bind:value={client} class="gap-4">
		<Tabs.List class="max-w-full flex-wrap group-data-horizontal/tabs:h-auto">
			{#each setups as setup (setup.id)}
				<Tabs.Trigger value={setup.id}>{setup.name}</Tabs.Trigger>
			{/each}
		</Tabs.List>
		{#each setups as setup (setup.id)}
			<Tabs.Content value={setup.id} class="space-y-2">
				<p class="text-sm text-muted-foreground">
					{setup.file ? m['settings.mcp.file_hint']({ file: setup.file }) : m['settings.mcp.run_hint']()}
					{#if setup.id === 'codex'}
						{m['settings.mcp.env_hint']({ variable: CODEX_TOKEN_VARIABLE })}
					{/if}
				</p>
				{@render snippet(setup.name, setup.code)}
			</Tabs.Content>
		{/each}
	</Tabs.Root>
</div>

<PageTitle parts={[m['settings.mcp.title'](), m['settings.title']()]} />
