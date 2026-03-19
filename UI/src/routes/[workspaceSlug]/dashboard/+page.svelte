<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { listIssues } from '$lib/api/issues';
	import { STATUS_LABELS, type IssueStatus } from '$lib/types/issue';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let statusCounts = $state<Record<string, number>>({});
	let totalIssues = $state(0);
	let loading = $state(true);

	onMount(async () => {
		try {
			const res = await listIssues(slug, { per_page: '1' });
			totalIssues = res.total_count;

			// Fetch counts per status
			for (const status of Object.keys(STATUS_LABELS)) {
				const r = await listIssues(slug, { status, per_page: '1' });
				statusCounts[status] = r.total_count;
			}
			statusCounts = { ...statusCounts };
		} finally {
			loading = false;
		}
	});
</script>

<div class="h-full">
	<div class="flex h-[49px] items-center border-b border-[var(--app-border)] px-6">
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Dashboard</h1>
	</div>

	<div class="p-6">
		{#if loading}
			<p class="text-sm text-[var(--color-text-secondary)]">Loading dashboard...</p>
		{:else}
			<div class="mb-6">
				<p class="text-3xl font-bold text-[var(--color-text-primary)]">{totalIssues}</p>
				<p class="text-sm text-[var(--color-text-secondary)]">Total issues</p>
			</div>

			<div class="grid grid-cols-3 gap-4">
				{#each Object.entries(STATUS_LABELS) as [status, label]}
					<div
						class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4"
					>
						<p class="text-2xl font-semibold text-[var(--color-text-primary)]">
							{statusCounts[status] ?? 0}
						</p>
						<p class="text-sm text-[var(--color-text-secondary)]">{label}</p>
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>
