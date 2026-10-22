<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { flip } from 'svelte/animate';
	import { Monitor, Sun, Moon, ArrowUp, ArrowDown, GripVertical } from '@lucide/svelte';
	import * as Select from '$lib/components/ui/select';
	import { Switch } from '$lib/components/ui/switch';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { preferencesState } from '$lib/features/preferences/preferences.state.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale, locales } from '$lib/paraglide/runtime.js';

	const LOCALE_LABELS: Record<string, string> = {
		en: 'English',
		it: 'Italiano',
		'zh-CN': '简体中文',
		'zh-TW': '繁體中文'
	};
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
	import { getPriorityLabel } from '$lib/types/issue';
	import { getCategoryLabel, type StatusCategory } from '$lib/types/team-status';

	const slug = $derived(page.params.workspaceSlug ?? '');

	const fontSizeLabels = $derived<Record<string, string>>({
		small: m['prefs.font_size.small'](),
		default: m['prefs.font_size.default'](),
		large: m['prefs.font_size.large'](),
	});

	const lightThemeLabels = $derived<Record<string, string>>({
		light: m['prefs.theme.light'](),
		'rose-light': m['prefs.theme.rose_light'](),
		'blue-light': m['prefs.theme.blue_light'](),
	});

	const darkThemeLabels = $derived<Record<string, string>>({
		dark: m['prefs.theme.dark'](),
		'dark-gray': m['prefs.theme.dark_gray'](),
		'amethyst-dark': m['prefs.theme.amethyst_dark'](),
		'emerald-dark': m['prefs.theme.emerald_dark'](),
		'cyber-77': m['prefs.theme.cyber_77'](),
		'blade-49': m['prefs.theme.blade_49'](),
		'pipboy': m['prefs.theme.pipboy'](),
	});

	const workflowSortLabels = $derived<Record<string, string>>({
		default: m['prefs.workflow_order'](),
		'active-first': m['prefs.active_first'](),
		custom: m['prefs.custom'](),
	});

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
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{m['prefs.title']()}</h1>

	<!-- Interface and theme -->
	<!-- Language -->
	<div class="mt-3 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['prefs.language']()}</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['prefs.language_desc']()}</p>
			</div>
			<Select.Root
				type="single"
				value={getLocale()}
				onValueChange={(v) => {
					if (v) setLocale(v as (typeof locales)[number]);
				}}
			>
				<Select.Trigger size="sm" class="w-[130px]">
					{LOCALE_LABELS[getLocale()] ?? 'English'}
				</Select.Trigger>
				<Select.Content>
					{#each locales as locale}
						<Select.Item value={locale}>{LOCALE_LABELS[locale] ?? locale}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>
	</div>

	<h2 class="mt-8 text-sm font-medium text-[var(--color-text-secondary)]">{m['prefs.interface_theme']()}</h2>

	<div class="mt-3 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<!-- Font size -->
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['prefs.font_size']()}</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['prefs.font_size_desc']()}</p>
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
					<Select.Item value="small">{m['prefs.font_size.small']()}</Select.Item>
					<Select.Item value="default">{m['prefs.font_size.default']()}</Select.Item>
					<Select.Item value="large">{m['prefs.font_size.large']()}</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<!-- Pointer cursors -->
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['prefs.pointer_cursors']()}</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['prefs.pointer_cursors_desc']()}</p>
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
				<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['prefs.interface_theme_label']()}</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['prefs.interface_theme_desc']()}</p>
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
				<ToggleGroup.Item value="system" aria-label={m['prefs.system_pref']()}>
					<Monitor size={14} />
				</ToggleGroup.Item>
				<ToggleGroup.Item value="light" aria-label={m['prefs.light_mode']()}>
					<Sun size={14} />
				</ToggleGroup.Item>
				<ToggleGroup.Item value="dark" aria-label={m['prefs.dark_mode']()}>
					<Moon size={14} />
				</ToggleGroup.Item>
			</ToggleGroup.Root>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<!-- Light theme variant -->
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['prefs.light_theme']()}</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['prefs.light_theme_desc']()}</p>
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
					<Select.Item value="light">{m['prefs.theme.light']()}</Select.Item>
					<Select.Item value="rose-light">{m['prefs.theme.rose_light']()}</Select.Item>
					<Select.Item value="blue-light">{m['prefs.theme.blue_light']()}</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<!-- Dark theme variant -->
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['prefs.dark_theme']()}</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['prefs.dark_theme_desc']()}</p>
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
					<Select.Item value="dark">{m['prefs.theme.dark']()}</Select.Item>
					<Select.Item value="dark-gray">{m['prefs.theme.dark_gray']()}</Select.Item>
					<Select.Item value="amethyst-dark">{m['prefs.theme.amethyst_dark']()}</Select.Item>
					<Select.Item value="emerald-dark">{m['prefs.theme.emerald_dark']()}</Select.Item>
					<Select.Item value="cyber-77">{m['prefs.theme.cyber_77']()}</Select.Item>
					<Select.Item value="blade-49">{m['prefs.theme.blade_49']()}</Select.Item>
					<Select.Item value="pipboy">{m['prefs.theme.pipboy']()}</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>
	</div>

	<!-- Issue list display -->
	<h2 class="mt-8 text-sm font-medium text-[var(--color-text-secondary)]">{m['prefs.issue_list_display']()}</h2>

	<div class="mt-3 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['prefs.workflow_sorting']()}</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['prefs.workflow_sorting_desc']()}</p>
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
					<Select.Item value="default">{m['prefs.workflow_order']()}</Select.Item>
					<Select.Item value="active-first">{m['prefs.active_first']()}</Select.Item>
					<Select.Item value="custom">{m['prefs.custom']()}</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>

		{#if preferencesState.workflowSortMode === 'custom'}
			<div class="border-t border-[var(--app-border)]"></div>
			<div class="px-5 py-4">
				<p class="mb-2 text-xs text-[var(--color-text-tertiary)]">{m['prefs.custom_category_order']()}</p>
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
								<span class="text-sm text-[var(--color-text-primary)] transition-colors group-hover:text-[var(--color-text-primary)]">{getCategoryLabel(category)}</span>
							</div>
							<div class="flex items-center gap-1 opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-within:opacity-100">
								<Button variant="ghost" size="sm"
									onclick={() => moveWorkflowCategory(category, -1)}
									disabled={index === 0}
									class="rounded p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] disabled:opacity-30"
									aria-label={m['prefs.move_up']({ name: getCategoryLabel(category) })}
								>
									<ArrowUp size={13} />
								</Button>
								<Button variant="ghost" size="sm"
									onclick={() => moveWorkflowCategory(category, 1)}
									disabled={index === preferencesState.workflowSortOrder.length - 1}
									class="rounded p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] disabled:opacity-30"
									aria-label={m['prefs.move_down']({ name: getCategoryLabel(category) })}
								>
									<ArrowDown size={13} />
								</Button>
							</div>
						</div>
					{/each}
				</div>
			</div>
		{/if}
	</div>

	<!-- Issue creation -->
	<h2 class="mt-8 text-sm font-medium text-[var(--color-text-secondary)]">{m['prefs.issue_creation']()}</h2>

	<div class="mt-3 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['prefs.default_prefill']()}</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['prefs.default_prefill_desc']()}</p>
			</div>
			<Button variant="outline" size="sm"
				type="button"
				disabled={issueDefaultsLoading || Object.keys(issueDefaults).length === 0}
				onclick={clearDefaults}
				class="rounded-md border px-2 py-1 text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)] disabled:opacity-40"
			>
				{m['prefs.clear_defaults']()}
			</Button>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<div class="grid gap-4 px-5 py-4 sm:grid-cols-2">
			<div>
				<p class="mb-1.5 text-xs text-[var(--color-text-tertiary)]">{m['prefs.team']()}</p>
				<Select.Root
					type="single"
					value={issueDefaults.teamId ?? 'none'}
					onValueChange={(v) => setDefaultTeam(v === 'none' ? undefined : v)}
				>
					<Select.Trigger size="sm" class="w-full">
						{teams.find((team) => team.id === issueDefaults.teamId)?.name ?? m['prefs.no_default']()}
					</Select.Trigger>
					<Select.Content>
						<Select.Item value="none">{m['prefs.no_default']()}</Select.Item>
						{#each teams as team (team.id)}
							<Select.Item value={team.id}>{team.name}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>

			<div>
				<p class="mb-1.5 text-xs text-[var(--color-text-tertiary)]">{m['prefs.status']()}</p>
				<Select.Root
					type="single"
					value={issueDefaults.statusId ?? 'none'}
					onValueChange={(v) => setDefaultStatus(v === 'none' ? undefined : v)}
					disabled={!issueDefaults.teamId}
				>
					<Select.Trigger size="sm" class="w-full">
						{statuses.find((status) => status.id === issueDefaults.statusId)?.name ?? m['prefs.no_default']()}
					</Select.Trigger>
					<Select.Content>
						<Select.Item value="none">{m['prefs.no_default']()}</Select.Item>
						{#each statuses as status (status.id)}
							<Select.Item value={status.id}>{status.name}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>

			<div>
				<p class="mb-1.5 text-xs text-[var(--color-text-tertiary)]">{m['prefs.priority']()}</p>
				<Select.Root
					type="single"
					value={issueDefaults.priority === undefined ? 'none' : String(issueDefaults.priority)}
					onValueChange={(v) => setDefaultPriority(v === 'none' ? undefined : Number(v) as IssuePriority)}
				>
					<Select.Trigger size="sm" class="w-full">
						{issueDefaults.priority === undefined ? m['prefs.no_default']() : getPriorityLabel(issueDefaults.priority)}
					</Select.Trigger>
					<Select.Content>
						<Select.Item value="none">{m['prefs.no_default']()}</Select.Item>
						{#each priorityValues as value (value)}
							<Select.Item value={String(value)}>{getPriorityLabel(value)}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>

			<div>
				<p class="mb-1.5 text-xs text-[var(--color-text-tertiary)]">{m['prefs.project']()}</p>
				<Select.Root
					type="single"
					value={issueDefaults.projectId ?? 'none'}
					onValueChange={(v) => setDefaultProject(v === 'none' ? undefined : v)}
				>
					<Select.Trigger size="sm" class="w-full">
						{projects.find((project) => project.id === issueDefaults.projectId)?.name ?? m['prefs.no_default']()}
					</Select.Trigger>
					<Select.Content>
						<Select.Item value="none">{m['prefs.no_default']()}</Select.Item>
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
				<p class="mb-2 text-xs text-[var(--color-text-tertiary)]">{m['prefs.assignees']()}</p>
				<div class="max-h-44 space-y-1 overflow-y-auto rounded-md border border-[var(--app-border)] p-1">
					{#each members as member (member.user_id)}
						<Button variant="ghost" size="sm"
							type="button"
							onclick={() => toggleDefaultAssignee(member.user_id)}
							class="h-auto justify-start whitespace-normal flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm text-[var(--color-text-primary)]"
						>
							<Checkbox checked={issueDefaults.assigneeIds?.includes(member.user_id) ?? false} />
							<span class="truncate">{member.name || member.email}</span>
						</Button>
					{/each}
					{#if members.length === 0}
						<p class="px-2 py-1.5 text-sm text-[var(--color-text-tertiary)]">{m['prefs.no_members']()}</p>
					{/if}
				</div>
			</div>

			<div>
				<p class="mb-2 text-xs text-[var(--color-text-tertiary)]">{m['prefs.labels']()}</p>
				<div class="max-h-44 space-y-1 overflow-y-auto rounded-md border border-[var(--app-border)] p-1">
					{#each labels as label (label.id)}
						<Button variant="ghost" size="sm"
							type="button"
							onclick={() => toggleDefaultLabel(label.id)}
							class="h-auto justify-start whitespace-normal flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm text-[var(--color-text-primary)]"
						>
							<Checkbox checked={issueDefaults.labelIds?.includes(label.id) ?? false} />
							<span class="h-2.5 w-2.5 rounded-full" style="background-color: {label.color}"></span>
							<span class="truncate">{label.name}</span>
						</Button>
					{/each}
					{#if labels.length === 0}
						<p class="px-2 py-1.5 text-sm text-[var(--color-text-tertiary)]">{m['prefs.no_labels']()}</p>
					{/if}
				</div>
			</div>
		</div>
	</div>
</div>
