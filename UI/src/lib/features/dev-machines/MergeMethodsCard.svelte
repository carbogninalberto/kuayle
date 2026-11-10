<script lang="ts">
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { getMergeMethods, setMergeMethod, type MergeMethod } from '$lib/api/dev-machines';
	import type { GitHubRepo } from '$lib/types/github';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';

	// How the assistant's merge_pull_request merges each repository. Without a
	// choice it uses the first method the repository allows on GitHub. A choice
	// is saved at once, apart from the policy form the card sits in.
	let { slug, repositories, disabled = false }: { slug: string; repositories: GitHubRepo[]; disabled?: boolean } = $props();
	let methods = $state<Record<string, MergeMethod | ''>>({});
	let saving = $state<string | null>(null);

	$effect(() => {
		getMergeMethods(slug)
			.then((loaded) => (methods = { ...loaded }))
			.catch((error) => appToast.apiError(error, m['settings.dev_machines.merge_methods']()));
	});

	async function choose(repository: string, method: MergeMethod | '') {
		const previous = methods[repository] ?? '';
		methods[repository] = method;
		saving = repository;
		try {
			await setMergeMethod(slug, repository, method);
		} catch (error) {
			methods[repository] = previous;
			appToast.apiError(error, m['settings.dev_machines.merge_methods']());
		} finally {
			saving = null;
		}
	}

	const options = $derived([
		{ value: '' as MergeMethod | '', label: m['settings.dev_machines.merge_method_default']() },
		{ value: 'squash' as MergeMethod | '', label: m['settings.dev_machines.merge_method_squash']() },
		{ value: 'merge' as MergeMethod | '', label: m['settings.dev_machines.merge_method_merge']() },
		{ value: 'rebase' as MergeMethod | '', label: m['settings.dev_machines.merge_method_rebase']() }
	]);
	const active = $derived(repositories.filter((repo) => repo.is_active));
</script>

<section
	class="space-y-2 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-4 py-3"
	aria-labelledby="merge-methods-title"
	data-testid="merge-methods"
>
	<div>
		<h2 id="merge-methods-title" class="text-sm font-semibold">{m['settings.dev_machines.merge_methods']()}</h2>
		<p class="text-xs text-[var(--color-text-tertiary)]">
			{m['settings.dev_machines.merge_methods_desc']()}
			{m['settings.dev_machines.merge_methods_saved_at_once']()}
		</p>
	</div>
	{#if active.length}
		<ul class="grid gap-x-6 gap-y-1.5 lg:grid-cols-2">
			{#each active as repo (repo.id)}
				<li class="flex min-w-0 items-center justify-between gap-3">
					<span class="min-w-0 truncate font-mono text-xs" title={repo.full_name}>{repo.full_name}</span>
					<SelectField
						size="sm"
						bind:value={() => methods[repo.full_name] ?? '', (next: MergeMethod | '') => void choose(repo.full_name, next)}
						aria-label={m['settings.dev_machines.merge_method_for']({ repository: repo.full_name })}
						disabled={disabled || saving === repo.full_name}
						{options}
					/>
				</li>
			{/each}
		</ul>
	{:else}
		<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.dev_machines.merge_methods_empty']()}</p>
	{/if}
</section>
