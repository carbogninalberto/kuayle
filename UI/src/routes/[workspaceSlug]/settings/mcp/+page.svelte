<script lang="ts">
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

<div class="mx-auto w-full max-w-3xl space-y-6 px-4 py-8 sm:px-8 sm:py-10">
	<div class="space-y-2">
		<h1 class="text-2xl font-semibold">{m['settings.mcp.title']()}</h1>
		<p class="text-sm text-muted-foreground">{m['settings.mcp.desc']()}</p>
	</div>

	<section class="space-y-2">
		<h2 class="text-sm font-medium">{m['settings.mcp.url']()}</h2>
		{@render snippet(m['settings.mcp.url'](), url)}
		<p class="text-sm text-muted-foreground">
			{m['settings.mcp.token_hint']({ placeholder: TOKEN_PLACEHOLDER })}
			<a href="/{slug}/settings/tokens" class="font-medium text-foreground underline underline-offset-4">
				{m['settings.mcp.token_link']()}
			</a>
		</p>
	</section>

	<Tabs.Root bind:value={client} class="gap-4">
		<Tabs.List>
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
