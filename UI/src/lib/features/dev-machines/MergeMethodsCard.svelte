<script lang="ts">
	import { Label } from '$lib/components/ui/label';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { getMergeMethods, setMergeMethod, type MergeMethod } from '$lib/api/dev-machines';
	import type { GitHubRepo } from '$lib/types/github';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';

	// How the assistant's merge_pull_request merges each repository. Without a
	// choice it uses the first method the repository allows on GitHub.
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
</script>

<section
	class="space-y-3 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5"
	data-testid="merge-methods"
>
	<div>
		<h2 class="text-sm font-semibold">{m['settings.dev_machines.merge_methods']()}</h2>
		<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.dev_machines.merge_methods_desc']()}</p>
	</div>
	{#each repositories.filter((repo) => repo.is_active) as repo (repo.id)}
		<div class="grid items-center gap-2 sm:grid-cols-[1fr_16rem]">
			<Label class="min-w-0 truncate font-mono text-xs">{repo.full_name}</Label>
			<SelectField
				bind:value={() => methods[repo.full_name] ?? '', (next: MergeMethod | '') => void choose(repo.full_name, next)}
				aria-label={m['settings.dev_machines.merge_method_for']({ repository: repo.full_name })}
				disabled={disabled || saving === repo.full_name}
				{options}
			/>
		</div>
	{:else}
		<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.dev_machines.merge_methods_empty']()}</p>
	{/each}
</section>
