<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { createView } from '$lib/api/views';
	import type { Team } from '$lib/types/team';
	import type { ViewFilter, ViewScope } from '$lib/types/view';
	import { Bookmark, Building2, Check, CircleUser, SquareUser } from 'lucide-svelte';
	import { appToast } from '$lib/features/toast/toast';

	let {
		open = $bindable(false),
		showTrigger = true,
		filters,
		slug,
		teams = [],
		defaultTeamId,
		defaultScope = 'personal'
	}: {
		open?: boolean;
		showTrigger?: boolean;
		filters: ViewFilter;
		slug: string;
		teams?: Team[];
		defaultTeamId?: string;
		defaultScope?: ViewScope;
	} = $props();

	let name = $state('');
	let description = $state('');
	let scope = $state<ViewScope>('personal');

	const scopeOptions: Array<{
		value: ViewScope;
		label: string;
		description: string;
		icon: typeof CircleUser;
	}> = [
		{
			value: 'personal',
			label: 'Personal',
			description: 'Only visible to you',
			icon: CircleUser
		},
		{
			value: 'workspace',
			label: 'Workspace',
			description: 'Shared with everyone',
			icon: Building2
		},
		{
			value: 'team',
			label: 'Team',
			description: 'Shared in a team section',
			icon: SquareUser
		}
	];

	let currentTeam = $derived(teams.find((team) => team.id === defaultTeamId));
	let visibleScopeOptions = $derived(scopeOptions.filter((option) => option.value !== 'team' || defaultTeamId));

	$effect(() => {
		if (open) {
			name = '';
			description = '';
			scope = defaultScope === 'team' && !defaultTeamId ? 'personal' : defaultScope;
		}
	});

	$effect(() => {
		if (scope === 'team' && !defaultTeamId) scope = 'personal';
	});

	async function handleSubmit(e: Event) {
		e.preventDefault();
		if (!name.trim()) return;
		if (scope === 'team' && !defaultTeamId) {
			appToast.error('Team views can only be saved from a team');
			return;
		}

		const nextFilters: ViewFilter = {
			...filters,
			...(defaultTeamId ? { team: defaultTeamId } : {}),
			view_scope: scope
		};
		if (scope === 'team') {
			nextFilters.team = defaultTeamId;
			nextFilters.view_team = defaultTeamId;
		} else {
			delete nextFilters.view_team;
		}

		try {
			await createView(slug, {
				name: name.trim(),
				description: description.trim() || undefined,
				filters: nextFilters,
				is_shared: scope !== 'personal'
			});
			appToast.success('View saved');
			open = false;
		} catch (err: any) {
			appToast.apiError(err, 'Failed to save view');
		}
	}
</script>

<Dialog.Root bind:open>
	{#if showTrigger}
		<Dialog.Trigger
			class="flex items-center gap-1 rounded-md border border-[var(--app-border)] px-2 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)]"
			title="Save as view"
		>
			<Bookmark size={12} />
			Save view
		</Dialog.Trigger>
	{/if}

	<Dialog.Content
		class="sm:max-w-[420px] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 overflow-hidden rounded-xl"
	>
		<form onsubmit={handleSubmit}>
			<div class="px-5 pt-5 pb-4 space-y-4">
				<div>
					<h2 class="text-base font-semibold text-[var(--color-text-primary)]">Save view</h2>
					<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">Save the current filters as a reusable view.</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">Name</Label>
					<Input
						bind:value={name}
						placeholder="e.g. Active bugs"
						required
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]"
						>Description <span class="text-[var(--color-text-tertiary)]">(optional)</span></Label
					>
					<Input
						bind:value={description}
						placeholder="What does this view show?"
						class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
					/>
				</div>

				<div class="space-y-2">
					<Label class="text-xs text-[var(--color-text-secondary)]">Visibility</Label>
					<div class="grid gap-2">
						{#each visibleScopeOptions as option}
							{@const Icon = option.icon}
							<button
								type="button"
								onclick={() => (scope = option.value)}
								class="flex items-center gap-3 rounded-lg border p-3 text-left transition-colors {scope === option.value
									? 'border-[var(--app-accent)] bg-[var(--app-accent)]/10'
									: 'border-[var(--app-border)] bg-[var(--color-bg)] hover:bg-[var(--color-bg-hover)]'}"
							>
								<Icon size={16} class="shrink-0 text-[var(--color-text-secondary)]" />
								<span class="min-w-0 flex-1">
									<span class="block text-sm font-medium text-[var(--color-text-primary)]">{option.label}</span>
									<span class="block text-xs text-[var(--color-text-tertiary)]">
										{option.value === 'team' && currentTeam ? `Shared with ${currentTeam.name}` : option.description}
									</span>
								</span>
								{#if scope === option.value}
									<Check size={15} class="shrink-0 text-[var(--app-accent-light)]" />
								{/if}
							</button>
						{/each}
					</div>
				</div>

				{#if scope === 'team' && currentTeam}
					<div
						class="flex items-center gap-2 rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-xs text-[var(--color-text-secondary)]"
					>
						<SquareUser size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<span>This view will be saved to {currentTeam.name}.</span>
					</div>
				{/if}
			</div>

			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={() => (open = false)}>Cancel</Button>
				<Button size="sm" type="submit" disabled={!name.trim()}>Save view</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>
