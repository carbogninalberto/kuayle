<script lang="ts">
	import MarkLabel from '$lib/components/MarkLabel.svelte';
	import * as Table from '$lib/components/ui/table';
	import {
		alternativePath,
		hostingLabel,
		mcpMark,
		pricingSummary,
		type Competitor
	} from '$lib/data/competitor-types';
	import { KUAYLE } from '$lib/data/kuayle-facts';

	let {
		competitors,
		caption
	}: {
		competitors: Competitor[];
		caption: string;
	} = $props();
</script>

<div class="rounded-xl border border-border bg-card/40">
	<Table.Root class="min-w-[68rem] table-fixed text-xs">
		<Table.Caption class="px-4 pb-4 text-left">{caption}</Table.Caption>
		<Table.Header>
			<Table.Row>
				<Table.Head class="w-[9rem] px-3">Tool</Table.Head>
				<Table.Head class="w-[8rem] px-3">Licence</Table.Head>
				<Table.Head class="w-[9rem] px-3">Hosting</Table.Head>
				<Table.Head class="w-[11rem] px-3">Price</Table.Head>
				<Table.Head class="px-3">SSO and SCIM</Table.Head>
				<Table.Head class="w-[6.5rem] px-3">MCP server</Table.Head>
				<Table.Head class="px-3">Importers</Table.Head>
			</Table.Row>
		</Table.Header>
		<Table.Body>
			<Table.Row class="bg-brand-400/5 align-top">
				<Table.Head scope="row" class="px-3 py-3 font-semibold whitespace-normal text-brand-200">Kuayle</Table.Head>
				<Table.Cell class="px-3 py-3 whitespace-normal">Apache 2.0</Table.Cell>
				<Table.Cell class="px-3 py-3 whitespace-normal">Self-hosted</Table.Cell>
				<Table.Cell class="px-3 py-3 whitespace-normal">Free, no paid tier</Table.Cell>
				<Table.Cell class="px-3 py-3 whitespace-normal text-muted-foreground">{KUAYLE.sso.text}</Table.Cell>
				<Table.Cell class="px-3 py-3 whitespace-normal"><MarkLabel mark={KUAYLE.mcp.mark} /></Table.Cell>
				<Table.Cell class="px-3 py-3 whitespace-normal text-muted-foreground">{KUAYLE.importers.text}</Table.Cell>
			</Table.Row>
			{#each competitors as competitor}
				{@const mcp = mcpMark(competitor)}
				<Table.Row id={competitor.slug} class="align-top">
					<Table.Head scope="row" class="px-3 py-3 font-medium whitespace-normal text-foreground">
						{#if competitor.tier === 'A'}
							<a href={alternativePath(competitor)} class="text-brand-300 underline-offset-4 hover:underline">{competitor.displayName ?? competitor.name}</a>
						{:else}
							<a href={competitor.website} target="_blank" rel="noopener noreferrer" class="underline-offset-4 hover:underline">{competitor.displayName ?? competitor.name}</a>
						{/if}
						{#if competitor.status !== 'active'}
							<span class="mt-1 block text-[0.7rem] font-normal text-amber-200">{competitor.status === 'discontinued' ? 'Discontinued' : 'Maintenance mode'}</span>
						{/if}
					</Table.Head>
					<Table.Cell class="px-3 py-3 whitespace-normal">{competitor.license}</Table.Cell>
					<Table.Cell class="px-3 py-3 whitespace-normal">{hostingLabel(competitor.hosting)}</Table.Cell>
					<Table.Cell class="px-3 py-3 whitespace-normal">{pricingSummary(competitor)}</Table.Cell>
					<Table.Cell class="px-3 py-3 whitespace-normal text-muted-foreground">{competitor.sso ?? 'Not documented'}</Table.Cell>
					<Table.Cell class="px-3 py-3 whitespace-normal">
						{#if mcp}<MarkLabel mark={mcp} />{:else}<span class="text-muted-foreground">Not documented</span>{/if}
					</Table.Cell>
					<Table.Cell class="px-3 py-3 whitespace-normal text-muted-foreground">{competitor.importers ?? 'Not documented'}</Table.Cell>
				</Table.Row>
			{/each}
		</Table.Body>
	</Table.Root>
</div>
