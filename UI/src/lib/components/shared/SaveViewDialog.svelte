<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { createView } from '$lib/api/views';
	import type { Team } from '$lib/types/team';
	import type { ViewFilter, ViewScope } from '$lib/types/view';
	import { Bookmark, Building2, Check, CircleUser, SquareUser } from '@lucide/svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

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

	const scopeOptions = $derived<Array<{
		value: ViewScope;
		label: string;
		description: string;
		icon: typeof CircleUser;
	}>>([
		{
			value: 'personal',
			label: m['sharedComponents.save_view.scope_personal'](),
			description: m['sharedComponents.save_view.scope_personal_desc'](),
			icon: CircleUser
		},
		{
			value: 'workspace',
			label: m['sharedComponents.save_view.scope_workspace'](),
			description: m['sharedComponents.save_view.scope_workspace_desc'](),
			icon: Building2
		},
		{
			value: 'team',
			label: m['sharedComponents.save_view.scope_team'](),
			description: m['sharedComponents.save_view.scope_team_desc'](),
			icon: SquareUser
		}
	]);

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
			appToast.error(m['sharedComponents.save_view.toast.team_only']());
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
			appToast.success(m['sharedComponents.save_view.toast.saved']());
			open = false;
		} catch (err: any) {
			appToast.apiError(err, m['sharedComponents.save_view.toast.save_failed']());
		}
	}
</script>

<Dialog.Root bind:open>
	{#if showTrigger}
		<Dialog.Trigger
			class="flex items-center gap-1 rounded-md border border-[var(--app-border)] px-2 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)]"
			title={m['sharedComponents.save_view.trigger_title']()}
		>
			<Bookmark size={12} />
			{m['sharedComponents.save_view.trigger']()}
		</Dialog.Trigger>
	{/if}

	<Dialog.Content
		class="sm:max-w-[420px] border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0 overflow-hidden rounded-xl"
	>
		<form onsubmit={handleSubmit}>
			<div class="px-5 pt-5 pb-4 space-y-4">
				<div>
					<h2 class="text-base font-semibold text-[var(--color-text-primary)]">{m['sharedComponents.save_view.title']()}</h2>
					<p class="mt-0.5 text-xs text-[var(--color-text-tertiary)]">{m['sharedComponents.save_view.description']()}</p>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['sharedComponents.save_view.name']()}</Label>
					<Input
						bind:value={name}
						placeholder={m['sharedComponents.save_view.name_placeholder']()}
						required
						class=""
					/>
				</div>

				<div class="space-y-1.5">
					<Label class="text-xs text-[var(--color-text-secondary)]"
						>{m['sharedComponents.save_view.description_label']()} <span class="text-[var(--color-text-tertiary)]">{m['sharedComponents.save_view.optional']()}</span></Label
					>
					<Input
						bind:value={description}
						placeholder={m['sharedComponents.save_view.description_placeholder']()}
						class=""
					/>
				</div>

				<div class="space-y-2">
					<Label class="text-xs text-[var(--color-text-secondary)]">{m['sharedComponents.save_view.visibility']()}</Label>
					<div class="grid gap-2">
						{#each visibleScopeOptions as option}
							{@const Icon = option.icon}
							<Button variant="ghost" size="sm"
								type="button"
								onclick={() => (scope = option.value)}
								class="h-auto justify-start whitespace-normal flex items-center gap-3 rounded-lg border p-3 text-left transition-colors {scope === option.value
									? 'border-[var(--app-accent)] bg-[var(--app-accent)]/10'
									: 'border-[var(--app-border)] bg-[var(--color-bg)] hover:bg-[var(--color-bg-hover)]'}"
							>
								<Icon size={16} class="shrink-0 text-[var(--color-text-secondary)]" />
								<span class="min-w-0 flex-1">
									<span class="block text-sm font-medium text-[var(--color-text-primary)]">{option.label}</span>
									<span class="block text-xs text-[var(--color-text-tertiary)]">
										{option.value === 'team' && currentTeam ? m['sharedComponents.save_view.shared_with_team']({ teamName: currentTeam.name }) : option.description}
									</span>
								</span>
								{#if scope === option.value}
									<Check size={15} class="shrink-0 text-[var(--app-accent-light)]" />
								{/if}
							</Button>
						{/each}
					</div>
				</div>

				{#if scope === 'team' && currentTeam}
					<div
						class="flex items-center gap-2 rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-xs text-[var(--color-text-secondary)]"
					>
						<SquareUser size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<span>{m['sharedComponents.save_view.saved_to_team']({ teamName: currentTeam.name })}</span>
					</div>
				{/if}
			</div>

			<div class="flex justify-end gap-2 border-t border-[var(--app-border)] px-5 py-3">
				<Button variant="outline" size="sm" type="button" onclick={() => (open = false)}>{m['sharedComponents.save_view.cancel']()}</Button>
				<Button size="sm" type="submit" disabled={!name.trim()}>{m['sharedComponents.save_view.save']()}</Button>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>
