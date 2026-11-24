<script lang="ts">
	import { ArrowLeft, FlaskConical } from '@lucide/svelte';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import * as Breadcrumb from '$lib/components/ui/breadcrumb/index.js';
	import type { AgentProfile, PipelineDefinition } from '$lib/api/workflows';
	import { m } from '$lib/features/i18n/messages';

	// An experimental pipeline (KUA-252), read only: its phases in order with
	// the checks each requires, and the roles of its roster. Pipelines have no
	// editor yet, and the server starts no task on one.
	let {
		name,
		pipeline,
		profiles,
		backHref
	}: { name: string; pipeline: PipelineDefinition; profiles: AgentProfile[]; backHref: string } = $props();

	const profileName = (id: string) => profiles.find((p) => p.id === id)?.name ?? id;
</script>

<div class="flex h-full min-h-0 flex-col" data-testid="pipeline-view">
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
					<h1 class="truncate text-sm font-medium">{name}</h1>
				</Breadcrumb.Item>
				<Breadcrumb.Item><Badge variant="secondary">{m['workflows.pipeline']()}</Badge></Breadcrumb.Item>
				<Breadcrumb.Item class="hidden sm:inline-flex">
					<Badge variant="outline">{m['workflows.read_only']()}</Badge>
				</Breadcrumb.Item>
			</Breadcrumb.List>
		</Breadcrumb.Root>
	</header>
	<div class="min-h-0 flex-1 overflow-y-auto">
		<div class="mx-auto max-w-3xl space-y-6 p-4 sm:p-6">
			<Alert.Root data-testid="pipeline-notice">
				<FlaskConical aria-hidden="true" />
				<Alert.Description>{m['workflows.pipeline_notice']()}</Alert.Description>
			</Alert.Root>
			<section aria-labelledby="pipeline-phases" class="space-y-2">
				<h2 id="pipeline-phases" class="text-sm font-medium">{m['workflows.pipeline_phases']()}</h2>
				<ol class="divide-y rounded-lg border text-sm">
					{#each pipeline.phases as phase, index (phase.id)}
						<li class="space-y-1.5 px-3 py-2" data-testid="pipeline-phase-{phase.id}">
							<div class="flex flex-wrap items-baseline gap-x-2">
								<span class="text-muted-foreground tabular-nums">{index + 1}.</span>
								<span class="font-medium">{phase.label}</span>
								<code class="text-xs text-muted-foreground">{phase.owner}</code>
							</div>
							{#if phase.checks?.length}
								<ul class="flex flex-wrap gap-1.5 pl-5">
									{#each phase.checks as check (check.id)}
										<li>
											<Badge variant={check.required ? 'secondary' : 'outline'} class="font-mono">
												{check.fact}{check.role ? ` · ${check.role}` : ''} ·
												{check.required ? m['workflows.pipeline_required']() : m['workflows.pipeline_advisory']()}
											</Badge>
										</li>
									{/each}
								</ul>
							{/if}
						</li>
					{/each}
				</ol>
			</section>
			<section aria-labelledby="pipeline-roles" class="space-y-2">
				<h2 id="pipeline-roles" class="text-sm font-medium">{m['workflows.pipeline_roles']()}</h2>
				<ul class="divide-y rounded-lg border text-sm">
					{#each pipeline.roster as member (member.role)}
						<li class="flex flex-wrap items-baseline gap-x-2 px-3 py-2" data-testid="pipeline-role-{member.role}">
							<code class="font-medium">{member.role}</code>
							<span class="text-muted-foreground">{profileName(member.profile_id)}</span>
							{#if member.writer}<Badge variant="outline">{m['workflows.pipeline_writer']()}</Badge>{/if}
						</li>
					{/each}
				</ul>
			</section>
		</div>
	</div>
</div>
