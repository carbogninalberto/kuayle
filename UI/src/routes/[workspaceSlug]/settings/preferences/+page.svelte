<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { flip } from 'svelte/animate';
	import { Monitor, Sun, Moon, ArrowUp, ArrowDown, GripVertical } from 'lucide-svelte';
	import * as Select from '$lib/components/ui/select';
	import { Switch } from '$lib/components/ui/switch';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { preferencesState } from '$lib/features/preferences/preferences.state.svelte';
	import { clearIssueCreateDefaults, getIssueCreateDefaults, setIssueCreateDefaults, type IssueCreateDefaults } from '$lib/features/issues/create-defaults';
	import { listLabels } from '$lib/api/labels';
	import { listMembers } from '$lib/api/members';
	import { listProjects } from '$lib/api/projects';
	import { listTeams } from '$lib/api/teams';
	import { listTeamStatuses } from '$lib/api/team-statuses';
	import type { Label } from '$lib/types/label';
	import type { Project } from '$lib/types/project';
	import type { Team } from '$lib/types/team';
	import type { TeamStatus } from '$lib/types/team-status';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { IssuePriority } from '$lib/types/issue';
	import { PRIORITY_LABELS } from '$lib/types/issue';
	import { CATEGORY_LABELS, type StatusCategory } from '$lib/types/team-status';

	const slug = $derived(page.params.workspaceSlug ?? '');

	const fontSizeLabels: Record<string, string> = {
		small: 'Small',
		default: 'Default',
		large: 'Large',
	};

	const lightThemeLabels: Record<string, string> = {
		light: 'Light',
		'rose-light': 'Rose Light',
		'blue-light': 'Blue Light',
	};

	const darkThemeLabels: Record<string, string> = {
		dark: 'Dark',
		'dark-gray': 'Dark Gray',
		'amethyst-dark': 'Amethyst Dark',
		'emerald-dark': 'Emerald Dark',
		'cyber-77': 'Cyber 77',
		'blade-49': 'Blade 49',
		'pipboy': 'Pip-Boy',
	};

	const workflowSortLabels: Record<string, string> = {
		default: 'Workflow order',
		'active-first': 'Active first',
		custom: 'Custom',
	};

	let dragCategory = $state<StatusCategory | null>(null);
	let dragOverCategory = $state<StatusCategory | null>(null);
	let dropIndicator = $state<'above' | 'below'>('below');
	let issueDefaults = $state<IssueCreateDefaults>({});
	let teams = $state<Team[]>([]);
	let projects = $state<Project[]>([]);
	let labels = $state<Label[]>([]);
	let members = $state<WorkspaceMember[]>([]);
	let statuses = $state<TeamStatus[]>([]);
	let issueDefaultsLoading = $state(true);

	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];

	onMount(async () => {
		issueDefaults = getIssueCreateDefaults(slug);
		try {
			const [t, p, l, m] = await Promise.all([
				listTeams(slug),
				listProjects(slug),
				listLabels(slug),
				listMembers(slug)
			]);
			teams = t;
			projects = p;
			labels = l;
			members = m;
			if (issueDefaults.teamId) {
				statuses = await listTeamStatuses(slug, issueDefaults.teamId);
			}
		} finally {
			issueDefaultsLoading = false;
		}
	});

	function saveIssueDefaults(next: IssueCreateDefaults) {
		issueDefaults = next;
		setIssueCreateDefaults(slug, next);
	}

	async function setDefaultTeam(teamId: string | undefined) {
		statuses = [];
		const next = { ...issueDefaults, teamId, statusId: undefined };
		saveIssueDefaults(next);
		if (teamId) {
			statuses = await listTeamStatuses(slug, teamId);
		}
	}

	function setDefaultPriority(priority: IssuePriority | undefined) {
		saveIssueDefaults({ ...issueDefaults, priority });
	}

	function setDefaultProject(projectId: string | null | undefined) {
		saveIssueDefaults({ ...issueDefaults, projectId });
	}

	function setDefaultStatus(statusId: string | undefined) {
		saveIssueDefaults({ ...issueDefaults, statusId });
	}

	function toggleDefaultAssignee(userId: string) {
		const current = issueDefaults.assigneeIds ?? [];
		const assigneeIds = current.includes(userId)
			? current.filter((id) => id !== userId)
			: [...current, userId];
		saveIssueDefaults({ ...issueDefaults, assigneeIds: assigneeIds.length > 0 ? assigneeIds : undefined });
	}

	function toggleDefaultLabel(labelId: string) {
		const current = issueDefaults.labelIds ?? [];
		const labelIds = current.includes(labelId)
			? current.filter((id) => id !== labelId)
			: [...current, labelId];
		saveIssueDefaults({ ...issueDefaults, labelIds: labelIds.length > 0 ? labelIds : undefined });
	}

	function clearDefaults() {
		clearIssueCreateDefaults(slug);
		issueDefaults = {};
		statuses = [];
	}

	function moveWorkflowCategory(category: StatusCategory, direction: -1 | 1) {
		const order = [...preferencesState.workflowSortOrder];
		const index = order.indexOf(category);
		const nextIndex = index + direction;
		if (index < 0 || nextIndex < 0 || nextIndex >= order.length) return;
		[order[index], order[nextIndex]] = [order[nextIndex], order[index]];
		preferencesState.setWorkflowSortOrder(order);
	}

	function handleWorkflowDragStart(e: DragEvent, category: StatusCategory) {
		dragCategory = category;
		if (e.dataTransfer) {
			e.dataTransfer.effectAllowed = 'move';
			e.dataTransfer.setData('text/plain', category);
		}
	}

	function handleWorkflowDragOver(e: DragEvent, category: StatusCategory) {
		if (!dragCategory) return;
		e.preventDefault();
		if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
		dragOverCategory = category;
		const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
		dropIndicator = e.clientY < rect.top + rect.height / 2 ? 'above' : 'below';
	}

	function handleWorkflowDragEnd() {
		dragCategory = null;
		dragOverCategory = null;
		dropIndicator = 'below';
	}

	function handleWorkflowDrop(e: DragEvent, targetCategory: StatusCategory) {
		e.preventDefault();
		const sourceCategory = (e.dataTransfer?.getData('text/plain') || dragCategory) as StatusCategory | null;
		if (!sourceCategory || sourceCategory === targetCategory) {
			handleWorkflowDragEnd();
			return;
		}

		const order = [...preferencesState.workflowSortOrder];
		const sourceIndex = order.indexOf(sourceCategory);
		const targetIndex = order.indexOf(targetCategory);
		if (sourceIndex === -1 || targetIndex === -1) {
			handleWorkflowDragEnd();
			return;
		}

		const [moved] = order.splice(sourceIndex, 1);
		const adjustedTargetIndex = order.indexOf(targetCategory);
		const insertIndex = dropIndicator === 'below' ? adjustedTargetIndex + 1 : adjustedTargetIndex;
		order.splice(insertIndex, 0, moved);
		preferencesState.setWorkflowSortOrder(order);
		handleWorkflowDragEnd();
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">Preferences</h1>

	<!-- Interface and theme -->
	<h2 class="mt-8 text-sm font-medium text-[var(--color-text-secondary)]">Interface and theme</h2>

	<div class="mt-3 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<!-- Font size -->
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Font size</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Set the font size for the interface.</p>
			</div>
			<Select.Root
				type="single"
				value={preferencesState.fontSize}
				onValueChange={(v) => {
					if (v) preferencesState.setFontSize(v as 'small' | 'default' | 'large');
				}}
			>
				<Select.Trigger size="sm" class="w-[130px]">
					{fontSizeLabels[preferencesState.fontSize]}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="small">Small</Select.Item>
					<Select.Item value="default">Default</Select.Item>
					<Select.Item value="large">Large</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<!-- Pointer cursors -->
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Use pointer cursors</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Display a pointer cursor on interactive elements.</p>
			</div>
			<Switch
				size="sm"
				checked={preferencesState.pointerCursors}
				onCheckedChange={(v) => preferencesState.setPointerCursors(v)}
			/>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<!-- Interface theme -->
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Interface theme</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Select your preferred color mode.</p>
			</div>
			<ToggleGroup.Root
				type="single"
				variant="outline"
				size="sm"
				value={preferencesState.themeMode}
				onValueChange={(v) => {
					if (v) preferencesState.setThemeMode(v as 'system' | 'light' | 'dark');
				}}
			>
				<ToggleGroup.Item value="system" aria-label="System preference">
					<Monitor size={14} />
				</ToggleGroup.Item>
				<ToggleGroup.Item value="light" aria-label="Light mode">
					<Sun size={14} />
				</ToggleGroup.Item>
				<ToggleGroup.Item value="dark" aria-label="Dark mode">
					<Moon size={14} />
				</ToggleGroup.Item>
			</ToggleGroup.Root>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<!-- Light theme variant -->
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Light theme</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Theme variant used in light mode.</p>
			</div>
			<Select.Root
				type="single"
				value={preferencesState.lightTheme}
				onValueChange={(v) => {
					if (v) preferencesState.setLightTheme(v as 'light' | 'rose-light' | 'blue-light');
				}}
			>
				<Select.Trigger size="sm" class="w-[130px]">
					{lightThemeLabels[preferencesState.lightTheme]}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="light">Light</Select.Item>
					<Select.Item value="rose-light">Rose Light</Select.Item>
					<Select.Item value="blue-light">Blue Light</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<!-- Dark theme variant -->
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Dark theme</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Theme variant used in dark mode.</p>
			</div>
			<Select.Root
				type="single"
				value={preferencesState.darkTheme}
				onValueChange={(v) => {
					if (v) preferencesState.setDarkTheme(v as 'dark' | 'dark-gray' | 'amethyst-dark' | 'emerald-dark' | 'cyber-77' | 'blade-49' | 'pipboy');
				}}
			>
				<Select.Trigger size="sm" class="w-[130px]">
					{darkThemeLabels[preferencesState.darkTheme]}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="dark">Dark</Select.Item>
					<Select.Item value="dark-gray">Dark Gray</Select.Item>
					<Select.Item value="amethyst-dark">Amethyst Dark</Select.Item>
					<Select.Item value="emerald-dark">Emerald Dark</Select.Item>
					<Select.Item value="cyber-77">Cyber 77</Select.Item>
					<Select.Item value="blade-49">Blade 49</Select.Item>
					<Select.Item value="pipboy">Pip-Boy</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>
	</div>

	<!-- Issue list display -->
	<h2 class="mt-8 text-sm font-medium text-[var(--color-text-secondary)]">Issue list display</h2>

	<div class="mt-3 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Workflow group sorting</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Controls status group order in issue lists. Kanban keeps the workflow order.</p>
			</div>
			<Select.Root
				type="single"
				value={preferencesState.workflowSortMode}
				onValueChange={(v) => {
					if (v) preferencesState.setWorkflowSortMode(v as 'default' | 'active-first' | 'custom');
				}}
			>
				<Select.Trigger size="sm" class="w-[145px]">
					{workflowSortLabels[preferencesState.workflowSortMode]}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="default">Workflow order</Select.Item>
					<Select.Item value="active-first">Active first</Select.Item>
					<Select.Item value="custom">Custom</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>

		{#if preferencesState.workflowSortMode === 'custom'}
			<div class="border-t border-[var(--app-border)]"></div>
			<div class="px-5 py-4">
				<p class="mb-2 text-xs text-[var(--color-text-tertiary)]">Custom category order</p>
				<div class="space-y-1">
					{#each preferencesState.workflowSortOrder as category, index (category)}
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<div
							animate:flip={{ duration: 180 }}
							class="group relative flex items-center justify-between rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-2 py-2 transition-[background-color,border-color,box-shadow,opacity,scale] duration-200 ease-out hover:border-[var(--app-accent)]/40 hover:bg-[var(--color-bg-hover)]/40 hover:shadow-sm {dragCategory === category ? 'scale-[0.99] opacity-70' : ''}"
							draggable="true"
							ondragstart={(e) => handleWorkflowDragStart(e, category)}
							ondragover={(e) => handleWorkflowDragOver(e, category)}
							ondragleave={() => (dragOverCategory = null)}
							ondragend={handleWorkflowDragEnd}
							ondrop={(e) => handleWorkflowDrop(e, category)}
						>
							{#if dragOverCategory === category && dragCategory !== category}
								<div class="absolute {dropIndicator === 'above' ? '-top-1' : '-bottom-1'} left-2 right-2 h-0.5 rounded-full bg-[var(--app-accent)] shadow-[0_0_12px_var(--app-accent)] transition-all"></div>
							{/if}
							<div class="flex items-center gap-2">
								<span class="cursor-grab rounded p-1 text-[var(--color-text-tertiary)] transition-colors group-hover:text-[var(--color-text-secondary)] active:cursor-grabbing">
									<GripVertical size={14} />
								</span>
								<span class="text-sm text-[var(--color-text-primary)] transition-colors group-hover:text-[var(--color-text-primary)]">{CATEGORY_LABELS[category]}</span>
							</div>
							<div class="flex items-center gap-1 opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-within:opacity-100">
								<button
									onclick={() => moveWorkflowCategory(category, -1)}
									disabled={index === 0}
									class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)] disabled:opacity-30"
									aria-label="Move {CATEGORY_LABELS[category]} up"
								>
									<ArrowUp size={13} />
								</button>
								<button
									onclick={() => moveWorkflowCategory(category, 1)}
									disabled={index === preferencesState.workflowSortOrder.length - 1}
									class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)] disabled:opacity-30"
									aria-label="Move {CATEGORY_LABELS[category]} down"
								>
									<ArrowDown size={13} />
								</button>
							</div>
						</div>
					{/each}
				</div>
			</div>
		{/if}
	</div>

	<!-- Issue creation -->
	<h2 class="mt-8 text-sm font-medium text-[var(--color-text-secondary)]">Issue creation</h2>

	<div class="mt-3 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Default prefill</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Values used when opening the create issue dialog. Current page filters can still override these.</p>
			</div>
			<button
				type="button"
				disabled={issueDefaultsLoading || Object.keys(issueDefaults).length === 0}
				onclick={clearDefaults}
				class="rounded-md border border-[var(--app-border)] px-2 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)] disabled:opacity-40"
			>
				Clear defaults
			</button>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<div class="grid gap-4 px-5 py-4 sm:grid-cols-2">
			<div>
				<p class="mb-1.5 text-xs text-[var(--color-text-tertiary)]">Team</p>
				<Select.Root
					type="single"
					value={issueDefaults.teamId ?? 'none'}
					onValueChange={(v) => setDefaultTeam(v === 'none' ? undefined : v)}
				>
					<Select.Trigger size="sm" class="w-full">
						{teams.find((team) => team.id === issueDefaults.teamId)?.name ?? 'No default'}
					</Select.Trigger>
					<Select.Content>
						<Select.Item value="none">No default</Select.Item>
						{#each teams as team (team.id)}
							<Select.Item value={team.id}>{team.name}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>

			<div>
				<p class="mb-1.5 text-xs text-[var(--color-text-tertiary)]">Status</p>
				<Select.Root
					type="single"
					value={issueDefaults.statusId ?? 'none'}
					onValueChange={(v) => setDefaultStatus(v === 'none' ? undefined : v)}
					disabled={!issueDefaults.teamId}
				>
					<Select.Trigger size="sm" class="w-full">
						{statuses.find((status) => status.id === issueDefaults.statusId)?.name ?? 'No default'}
					</Select.Trigger>
					<Select.Content>
						<Select.Item value="none">No default</Select.Item>
						{#each statuses as status (status.id)}
							<Select.Item value={status.id}>{status.name}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>

			<div>
				<p class="mb-1.5 text-xs text-[var(--color-text-tertiary)]">Priority</p>
				<Select.Root
					type="single"
					value={issueDefaults.priority === undefined ? 'none' : String(issueDefaults.priority)}
					onValueChange={(v) => setDefaultPriority(v === 'none' ? undefined : Number(v) as IssuePriority)}
				>
					<Select.Trigger size="sm" class="w-full">
						{issueDefaults.priority === undefined ? 'No default' : PRIORITY_LABELS[issueDefaults.priority]}
					</Select.Trigger>
					<Select.Content>
						<Select.Item value="none">No default</Select.Item>
						{#each priorityValues as value (value)}
							<Select.Item value={String(value)}>{PRIORITY_LABELS[value]}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>

			<div>
				<p class="mb-1.5 text-xs text-[var(--color-text-tertiary)]">Project</p>
				<Select.Root
					type="single"
					value={issueDefaults.projectId ?? 'none'}
					onValueChange={(v) => setDefaultProject(v === 'none' ? undefined : v)}
				>
					<Select.Trigger size="sm" class="w-full">
						{projects.find((project) => project.id === issueDefaults.projectId)?.name ?? 'No default'}
					</Select.Trigger>
					<Select.Content>
						<Select.Item value="none">No default</Select.Item>
						{#each projects as project (project.id)}
							<Select.Item value={project.id}>{project.name}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<div class="grid gap-4 px-5 py-4 sm:grid-cols-2">
			<div>
				<p class="mb-2 text-xs text-[var(--color-text-tertiary)]">Assignees</p>
				<div class="max-h-44 space-y-1 overflow-y-auto rounded-md border border-[var(--app-border)] p-1">
					{#each members as member (member.user_id)}
						<button
							type="button"
							onclick={() => toggleDefaultAssignee(member.user_id)}
							class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
						>
							<Checkbox checked={issueDefaults.assigneeIds?.includes(member.user_id) ?? false} />
							<span class="truncate">{member.name || member.email}</span>
						</button>
					{/each}
					{#if members.length === 0}
						<p class="px-2 py-1.5 text-sm text-[var(--color-text-tertiary)]">No members found</p>
					{/if}
				</div>
			</div>

			<div>
				<p class="mb-2 text-xs text-[var(--color-text-tertiary)]">Labels</p>
				<div class="max-h-44 space-y-1 overflow-y-auto rounded-md border border-[var(--app-border)] p-1">
					{#each labels as label (label.id)}
						<button
							type="button"
							onclick={() => toggleDefaultLabel(label.id)}
							class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
						>
							<Checkbox checked={issueDefaults.labelIds?.includes(label.id) ?? false} />
							<span class="h-2.5 w-2.5 rounded-full" style="background-color: {label.color}"></span>
							<span class="truncate">{label.name}</span>
						</button>
					{/each}
					{#if labels.length === 0}
						<p class="px-2 py-1.5 text-sm text-[var(--color-text-tertiary)]">No labels found</p>
					{/if}
				</div>
			</div>
		</div>
	</div>
</div>
