<script lang="ts">
	import { CircleHelp, LoaderCircle, PackageSearch, RefreshCw } from '@lucide/svelte';
	import * as Card from '$lib/components/ui/card/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { getEnvironmentStatus, refreshPackageIndex } from '$lib/api/dev-machines';
	import type { EnvironmentStatus } from '$lib/types/dev-machine';
	import { appToast, getErrorMessage } from '$lib/features/toast/toast';
	import { m, getLocale } from '$lib/features/i18n/messages';
	import { formatRelativeTime } from '$lib/utils/format';

	let {
		slug,
		machineId,
		running,
		canManage
	}: {
		slug: string;
		machineId: string;
		running: boolean;
		// Only the machine creator may refresh; the server enforces it too.
		canManage: boolean;
	} = $props();

	// The manager re-probes at most once a minute; a refresh in progress is
	// followed closely so its result shows as soon as it is recorded.
	const POLL_MS = 60_000;
	const REFRESH_POLL_MS = 2_000;
	const COMMAND = 'sudo apt-get update';
	const titleId = $props.id();

	let status = $state<EnvironmentStatus | null>(null);
	let requesting = $state(false);
	let request = 0;

	const index = $derived(status?.package_index ?? null);
	const indexState = $derived(running ? (index?.state ?? 'unknown') : 'unknown');
	const refresh = $derived(index?.refresh ?? null);
	const refreshing = $derived(requesting || refresh?.state === 'queued' || refresh?.state === 'running');
	const finished = $derived(refresh && (refresh.state === 'succeeded' || refresh.state === 'failed') ? refresh : null);
	const canRefresh = $derived(canManage && running && !!index?.sudo_available);

	async function load(target: string) {
		const id = ++request;
		try {
			const next = await getEnvironmentStatus(slug, target);
			if (id === request && target === machineId) status = next;
		} catch {
			// The next poll retries; the check is informational only.
		}
	}

	$effect(() => {
		const target = machineId;
		void running;
		status = null;
		void load(target);
	});

	$effect(() => {
		const target = machineId;
		const every = refreshing ? REFRESH_POLL_MS : POLL_MS;
		const timer = setInterval(() => {
			if (document.visibilityState === 'visible') void load(target);
		}, every);
		return () => clearInterval(timer);
	});

	async function startRefresh() {
		if (refreshing || !canRefresh) return;
		requesting = true;
		try {
			request++;
			status = await refreshPackageIndex(slug, machineId);
		} catch (error) {
			appToast.error(getErrorMessage(error, m['machines.env_refresh']()));
		} finally {
			requesting = false;
		}
	}
</script>

<Card.Root role="region" aria-labelledby={titleId} data-testid="environment-status">
	<Card.Header>
		<Card.Title id={titleId} class="flex items-center gap-2 text-sm"
			><PackageSearch class="size-4 text-muted-foreground" aria-hidden="true" />{m['machines.env_status']()}</Card.Title
		>
		<Card.Description>{m['machines.env_status_desc']()}</Card.Description>
	</Card.Header>
	<Card.Content class="space-y-3">
		<div
			class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between"
			data-testid="env-check-package-index"
			data-state={indexState}
		>
			<div class="flex min-w-0 items-center gap-2">
				<span class="text-sm font-medium">{m['machines.env_package_index']()}</span>
				{#if indexState === 'absent'}
					<Popover.Root>
						<Popover.Trigger
							aria-label={m['machines.env_package_index_about']()}
							class="inline-flex items-center gap-1 rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
						>
							<Badge
								variant="outline"
								class="rounded-full border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-amber-400"
								data-testid="env-package-index-badge"
							>
								{m['machines.env_package_index_missing']()}
								<CircleHelp class="size-3" aria-hidden="true" />
							</Badge>
						</Popover.Trigger>
						<Popover.Content class="w-80 space-y-3 text-xs" align="start" data-testid="env-package-index-popover">
							<p>{m['machines.env_package_index_expected']()}</p>
							{#if !canRefresh}
								<p class="text-muted-foreground">{m['machines.env_refresh_command']()}</p>
								<CodeBlock.Root aria-label={COMMAND}>
									<CodeBlock.Actions>
										<CodeBlock.CopyButton value={COMMAND} />
									</CodeBlock.Actions>
									<CodeBlock.Content code={COMMAND} />
								</CodeBlock.Root>
								{#if !index?.sudo_available}<p class="text-muted-foreground">
										{m['machines.env_refresh_sudo_hint']()}
									</p>{/if}
							{/if}
						</Popover.Content>
					</Popover.Root>
				{:else if indexState === 'present'}
					<span class="text-xs text-muted-foreground" data-testid="env-package-index-fetched">
						{m['machines.env_package_index_fetched']({
							time: index?.index_mtime ? formatRelativeTime(index.index_mtime, getLocale()) : ''
						})}
					</span>
				{:else}
					<span class="text-xs text-muted-foreground" data-testid="env-package-index-unknown"
						><span aria-hidden="true">—</span><span class="sr-only">{m['machines.env_package_index_unknown']()}</span
						></span
					>
				{/if}
			</div>
			{#if canRefresh && (indexState === 'absent' || refreshing || finished)}
				<Button size="sm" variant="outline" disabled={refreshing} onclick={startRefresh}>
					{#if refreshing}<LoaderCircle class="animate-spin" aria-hidden="true" />{m[
							'machines.env_refreshing'
						]()}{:else}<RefreshCw aria-hidden="true" />{m['machines.env_refresh']()}{/if}
				</Button>
			{/if}
		</div>
		{#if finished && running}
			<div class="space-y-2" data-testid="env-refresh-result">
				<p class="text-xs text-muted-foreground">
					{m['machines.env_refresh_exit']({ code: String(finished.exit_code ?? '—') })}
				</p>
				{#if finished.output}
					<CodeBlock.Root aria-label={m['machines.env_refresh_output']()}>
						<CodeBlock.Content code={finished.output} wrap />
					</CodeBlock.Root>
				{/if}
				{#if finished.state === 'failed'}<p class="text-xs text-muted-foreground">
						{m['machines.env_refresh_proxy_note']()}
					</p>{/if}
			</div>
		{/if}
	</Card.Content>
</Card.Root>
