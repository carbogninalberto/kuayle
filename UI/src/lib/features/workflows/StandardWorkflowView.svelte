<script lang="ts">
	import { ArrowLeft, Info } from '@lucide/svelte';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import * as Breadcrumb from '$lib/components/ui/breadcrumb/index.js';
	import { m } from '$lib/features/i18n/messages';

	// The built-in Standard flow, read only: every task without a workflow
	// runs it. It is not edited or copied; a workspace that wants another flow
	// creates a pipeline.
	let { description, backHref }: { description: string; backHref: string } = $props();
</script>

<div class="flex h-full min-h-0 flex-col" data-testid="standard-workflow-view">
	<header class="flex h-14 shrink-0 items-center border-b px-3 sm:px-4">
		<Breadcrumb.Root class="min-w-0">
			<Breadcrumb.List class="flex-nowrap gap-2 sm:gap-3">
				<Breadcrumb.Item class="min-w-0">
					<Breadcrumb.Link href={backHref} title={m['workflows.title']()} class="flex min-w-0 items-center gap-1.5">
						<ArrowLeft class="size-4 shrink-0" aria-hidden="true" />
						<span class="truncate">{m['workflows.title']()}</span>
					</Breadcrumb.Link>
				</Breadcrumb.Item>
				<Breadcrumb.Separator class="shrink-0">/</Breadcrumb.Separator>
				<Breadcrumb.Item aria-current="page" class="min-w-0 text-foreground">
					<h1 class="truncate text-sm font-medium">{m['workflows.standard']()}</h1>
				</Breadcrumb.Item>
				<Breadcrumb.Item><Badge variant="secondary">{m['workflows.builtin']()}</Badge></Breadcrumb.Item>
				<Breadcrumb.Item class="hidden sm:inline-flex">
					<Badge variant="outline">{m['workflows.read_only']()}</Badge>
				</Breadcrumb.Item>
			</Breadcrumb.List>
		</Breadcrumb.Root>
	</header>
	<div class="min-h-0 flex-1 overflow-y-auto">
		<div class="mx-auto max-w-3xl space-y-4 p-4 sm:p-6">
			{#if description}<p class="text-sm wrap-anywhere" data-testid="standard-workflow-description">{description}</p>{/if}
			<Alert.Root data-testid="standard-workflow-notice">
				<Info aria-hidden="true" />
				<Alert.Description>{m['workflows.standard_view_note']()}</Alert.Description>
			</Alert.Root>
		</div>
	</div>
</div>
