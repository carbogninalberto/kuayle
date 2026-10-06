<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import type { Team } from '$lib/types/team';
	import { listTeams, updateTeam } from '$lib/api/teams';
	import { getWorkspace } from '$lib/api/workspaces';
	import TeamPrivacySettings from '$lib/features/teams/TeamPrivacySettings.svelte';
	import TeamIcon from '$lib/components/shared/TeamIcon.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import * as Select from '$lib/components/ui/select';
	import * as Popover from '$lib/components/ui/popover';
	import { getGitHubStatus } from '$lib/api/github';
	import { deleteDevMachineScopeSetting, getDevMachineScopeSetting, listDevMachineEnvironments, updateDevMachineScopeSetting } from '$lib/api/dev-machines';
	import type { GitHubRepo } from '$lib/types/github';
	import type { DevMachineEnvironment } from '$lib/types/dev-machine';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';
	import { Check, Search, X } from 'lucide-svelte';
	import * as LucideIcons from 'lucide-svelte';
	import type { Component } from 'svelte';
	import Database from 'emoji-picker-element/database';
	import type { Emoji } from 'emoji-picker-element/shared';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');

	let team = $state<Team | null>(null);
	let loading = $state(true);
	let editingDetails = $state(false);
	let editName = $state('');
	let editDescription = $state('');
	let issueCopyPrompt = $state('');
	let savingIssueCopyPrompt = $state(false);
	let pickerOpen = $state(false);
	let pickerQuery = $state('');
	let pickerTab = $state<'icons' | 'emoji'>('icons');
	let visibleIconCount = $state(96);
	let visibleEmojiCount = $state(144);
	let lastPickerQuery = '';
	let emojiGroup = $state(0);
	let emojiResults = $state<Emoji[]>([]);
	let emojiLoading = $state(false);
	let emojiDatabase: Database | null = null;
	let emojiRequestId = 0;
	let developmentRepositories = $state<GitHubRepo[]>([]);
	let developmentEnvironments = $state<DevMachineEnvironment[]>([]);
	let developmentRepositoryId = $state('inherit');
	let developmentEnvironmentId = $state('inherit');
	let developmentLoading = $state(true);
	let developmentReady = $state(false);
	let savingDevelopment = $state(false);
	let canManageDevelopment = $state(false);
	let privacyEnabled = $state(false);
	let developmentRequestVersion = 0;
	let developmentSaveVersion = 0;

	const PRESET_COLORS = ['#ef4444', '#f97316', '#eab308', '#22c55e', '#06b6d4', '#3b82f6', '#8b5cf6', '#ec4899'];
	const ICON_PAGE_SIZE = 96;
	const EMOJI_PAGE_SIZE = 144;
	const excludedLucideExports = new Set(['Icon', 'LucideIcon', 'default']);
	const lucideExports = LucideIcons as unknown as Record<string, Component>;
	const EMOJI_GROUPS = [
		{ id: 0, labelKey: 'team_settings.emoji_group.smileys' },
		{ id: 1, labelKey: 'team_settings.emoji_group.people' },
		{ id: 3, labelKey: 'team_settings.emoji_group.nature' },
		{ id: 4, labelKey: 'team_settings.emoji_group.food' },
		{ id: 6, labelKey: 'team_settings.emoji_group.travel' },
		{ id: 5, labelKey: 'team_settings.emoji_group.activities' },
		{ id: 7, labelKey: 'team_settings.emoji_group.objects' },
		{ id: 8, labelKey: 'team_settings.emoji_group.symbols' },
		{ id: 9, labelKey: 'team_settings.emoji_group.flags' }
	];
	const dynamicMessage = m as unknown as Record<string, () => string>;

	function formatIconLabel(name: string): string {
		return name.replace(/([a-z0-9])([A-Z])/g, '$1 $2').replace(/([A-Z])([A-Z][a-z])/g, '$1 $2');
	}

	const TEAM_ICON_OPTIONS = Object.entries(lucideExports)
		.filter(([name, value]) => /^[A-Z]/.test(name) && !excludedLucideExports.has(name) && typeof value === 'function')
		.map(([name, icon]) => ({ value: name, label: formatIconLabel(name), icon }))
		.sort((a, b) => a.label.localeCompare(b.label));

	const filteredIcons = $derived(
		pickerOpen && pickerTab === 'icons'
			? TEAM_ICON_OPTIONS.filter((option) => {
					const query = pickerQuery.trim().toLowerCase();
					return !query || option.label.toLowerCase().includes(query) || option.value.toLowerCase().includes(query);
				})
			: []
	);
	const visibleIcons = $derived(filteredIcons.slice(0, visibleIconCount));
	const visibleEmojis = $derived(emojiResults.slice(0, visibleEmojiCount));
	const selectedEmoji = $derived(team?.icon?.startsWith('emoji:') ? team.icon.slice(6) : '');
	const selectedColor = $derived(team?.color ?? PRESET_COLORS[5]);
	const LEGACY_ICON_NAMES: Record<string, string> = {
		box: 'Box',
		'circle-dot': 'CircleDot',
		layers: 'Layers',
		settings: 'Settings',
		shield: 'ShieldCheck',
		'square-user': 'SquareUser',
		users: 'Users'
	};
	const selectedIconName = $derived(
		team?.icon && !team.icon.startsWith('emoji:') ? (LEGACY_ICON_NAMES[team.icon] ?? team.icon) : 'SquareUser'
	);

	function selectIcon(icon: string) {
		updateTeamVisual({ icon });
		pickerOpen = false;
		pickerQuery = '';
	}

	function emojiUnicode(emoji: Emoji): string {
		return 'unicode' in emoji ? emoji.unicode : emoji.name;
	}

	function emojiLabel(emoji: Emoji): string {
		if ('annotation' in emoji) return emoji.annotation;
		return emoji.name;
	}

	function selectEmoji(emoji: Emoji) {
		selectIcon(`emoji:${emojiUnicode(emoji)}`);
		if ('unicode' in emoji) {
			emojiDatabase?.incrementFavoriteEmojiCount(emoji.unicode);
		}
	}

	function showPickerTab(tab: 'icons' | 'emoji') {
		pickerTab = tab;
		pickerQuery = '';
		visibleIconCount = ICON_PAGE_SIZE;
		visibleEmojiCount = EMOJI_PAGE_SIZE;
	}

	function handlePickerScroll(e: Event) {
		const el = e.currentTarget as HTMLElement;
		if (el.scrollTop + el.clientHeight < el.scrollHeight - 80) return;
		if (pickerTab === 'icons' && visibleIconCount < filteredIcons.length) {
			visibleIconCount += ICON_PAGE_SIZE;
		}
		if (pickerTab === 'emoji' && visibleEmojiCount < emojiResults.length) {
			visibleEmojiCount += EMOJI_PAGE_SIZE;
		}
	}

	function handleHorizontalWheel(e: WheelEvent) {
		const el = e.currentTarget as HTMLElement;
		if (Math.abs(e.deltaY) <= Math.abs(e.deltaX)) return;
		e.preventDefault();
		el.scrollLeft += e.deltaY;
	}

	async function loadEmojiResults() {
		if (!emojiDatabase || !pickerOpen || pickerTab !== 'emoji') return;
		const requestId = ++emojiRequestId;
		emojiLoading = true;
		try {
			const query = pickerQuery.trim();
			const results = query
				? await emojiDatabase.getEmojiBySearchQuery(query)
				: await emojiDatabase.getEmojiByGroup(emojiGroup);
			if (requestId === emojiRequestId) {
				emojiResults = results;
				visibleEmojiCount = EMOJI_PAGE_SIZE;
			}
		} catch {
			if (requestId === emojiRequestId) emojiResults = [];
		} finally {
			if (requestId === emojiRequestId) emojiLoading = false;
		}
	}

	$effect(() => {
		if (pickerOpen) {
			visibleIconCount = ICON_PAGE_SIZE;
			visibleEmojiCount = EMOJI_PAGE_SIZE;
		} else {
			pickerQuery = '';
		}
	});

	function isCurrentDevelopmentScope(s: string, t: string, version: number) {
		return slug === s && teamId === t && developmentRequestVersion === version;
	}

	async function loadDevelopmentSettings(s: string, t: string, version: number) {
		try {
			const workspace = await getWorkspace(s);
			if (!isCurrentDevelopmentScope(s, t, version)) return;
			privacyEnabled = !!workspace.privacy_enabled;
			canManageDevelopment = !workspace.privacy_enabled && (workspace.current_user_role === 'owner' || workspace.current_user_role === 'admin');
			if (!canManageDevelopment) return;
			const [github, setting, environments] = await Promise.all([
				getGitHubStatus(s), getDevMachineScopeSetting(s, 'team', t), listDevMachineEnvironments(s)
			]);
			if (!isCurrentDevelopmentScope(s, t, version)) return;
			developmentRepositories = github.repos ?? [];
			developmentEnvironments = (environments ?? []).filter((item) => item.status === 'ready');
			developmentRepositoryId = setting.github_repo_id ?? 'inherit';
			developmentEnvironmentId = setting.environment_id ?? 'inherit';
			developmentReady = true;
		} catch (error) {
			if (!isCurrentDevelopmentScope(s, t, version)) return;
			appToast.apiError(error, m['team_settings.toast.dev_settings_load_failed']());
		} finally {
			if (isCurrentDevelopmentScope(s, t, version)) developmentLoading = false;
		}
	}

	$effect(() => {
		const s = slug;
		const t = teamId;
		const version = ++developmentRequestVersion;
		developmentSaveVersion++;
		developmentRepositories = [];
		developmentEnvironments = [];
		developmentRepositoryId = 'inherit';
		developmentEnvironmentId = 'inherit';
		developmentLoading = true;
		developmentReady = false;
		savingDevelopment = false;
		canManageDevelopment = false;
		if (!s || !t) return;
		void loadDevelopmentSettings(s, t, version);
		return () => {
			if (developmentRequestVersion === version) developmentRequestVersion++;
			developmentSaveVersion++;
		};
	});

	$effect(() => {
		if (pickerQuery === lastPickerQuery) return;
		lastPickerQuery = pickerQuery;
		visibleIconCount = ICON_PAGE_SIZE;
		visibleEmojiCount = EMOJI_PAGE_SIZE;
	});

	$effect(() => {
		pickerOpen;
		pickerTab;
		pickerQuery;
		emojiGroup;
		loadEmojiResults();
	});

	onMount(() => {
		emojiDatabase = new Database();
		loadEmojiResults();
	});

	let teamRefresh = $state(0);
	onMount(() => {
		const refresh = (event: Event) => {
			const detail = (event as CustomEvent).detail;
			if ((!detail?.slug || detail.slug === slug) && (detail?.resources ?? []).some((r: string) => ['teams', 'workspace', 'members'].includes(r))) teamRefresh++;
		};
		window.addEventListener('app:refresh', refresh);
		return () => window.removeEventListener('app:refresh', refresh);
	});
	$effect(() => {
		const s = slug, t = teamId;
		teamRefresh;
		let active = true;
		team = null; loading = true; editingDetails = false; issueCopyPrompt = '';
		if (!s || !t) return;
		void listTeams(s).then((teams) => {
			if (!active) return;
			team = teams.find(tm => tm.id === t) ?? null;
			issueCopyPrompt = team?.issue_copy_prompt ?? '';
		}).catch(() => { if (active) appToast.error(m['privacy.unavailable']()); })
		.finally(() => { if (active) loading = false; });
		return () => { active = false; };
	});

	function startEditDetails() {
		if (!team) return;
		editName = team.name;
		editDescription = team.description ?? '';
		editingDetails = true;
	}

	async function saveDetails() {
		if (!team || !editName.trim()) return;
		const previous = team;
		team = { ...team, name: editName.trim(), description: editDescription.trim() || null };
		try {
			team = await updateTeam(slug, teamId, {
				name: editName.trim(),
				description: editDescription.trim() || null
			});
			editingDetails = false;
			appToast.success(m['team_settings.toast.details_updated']());
		} catch (err: any) {
			team = previous;
			appToast.apiError(err, m['team_settings.toast.details_update_failed']());
		}
	}

	async function updateTeamVisual(data: { color?: string; icon?: string }) {
		if (!team) return;
		const previous = team;
		team = { ...team, ...data };
		try {
			team = await updateTeam(slug, teamId, data);
			appToast.success(m['team_settings.toast.appearance_updated']());
		} catch (err: any) {
			team = previous;
			appToast.apiError(err, m['team_settings.toast.appearance_update_failed']());
		}
	}

	async function updateSubIssueAutomation(
		field: 'parent_auto_close_enabled' | 'sub_issue_auto_close_enabled',
		value: boolean
	) {
		if (!team) return;
		const previous = team;
		team = { ...team, [field]: value };
		try {
			team = await updateTeam(slug, teamId, { [field]: value });
			appToast.success(m['team_settings.toast.automation_updated']());
		} catch (err: any) {
			team = previous;
			appToast.apiError(err, m['team_settings.toast.automation_update_failed']());
		}
	}

	async function saveIssueCopyPrompt() {
		if (!team) return;
		const previous = team;
		savingIssueCopyPrompt = true;
		const prompt = issueCopyPrompt.trim();
		team = { ...team, issue_copy_prompt: prompt || null };
		try {
			team = await updateTeam(slug, teamId, { issue_copy_prompt: prompt });
			issueCopyPrompt = team.issue_copy_prompt ?? '';
			appToast.success(m['team_settings.toast.copy_prompt_updated']());
		} catch (err: any) {
			team = previous;
			appToast.apiError(err, m['team_settings.toast.copy_prompt_update_failed']());
		} finally {
			savingIssueCopyPrompt = false;
		}
	}

	async function saveDevelopmentSettings() {
		if (!canManageDevelopment || developmentLoading || !developmentReady || savingDevelopment) return;
		const s = slug;
		const t = teamId;
		const requestVersion = developmentRequestVersion;
		const saveVersion = ++developmentSaveVersion;
		const repositoryId = developmentRepositoryId;
		const environmentId = developmentEnvironmentId;
		const repository = developmentRepositories.find((item) => item.id === repositoryId);
		savingDevelopment = true;
		try {
			if (repositoryId === 'inherit' && environmentId === 'inherit') {
				await deleteDevMachineScopeSetting(s, 'team', t);
			} else {
				await updateDevMachineScopeSetting(s, {
					scope_type: 'team', scope_id: t,
					github_repo_id: repository?.id, base_branch: repository?.default_branch,
					environment_id: environmentId === 'inherit' ? undefined : environmentId
				});
			}
			if (!isCurrentDevelopmentScope(s, t, requestVersion) || developmentSaveVersion !== saveVersion) return;
			appToast.success(m['team_settings.toast.dev_settings_saved']());
		} catch (error) {
			if (!isCurrentDevelopmentScope(s, t, requestVersion) || developmentSaveVersion !== saveVersion) return;
			appToast.apiError(error, m['team_settings.toast.dev_settings_save_failed']());
		} finally {
			if (isCurrentDevelopmentScope(s, t, requestVersion) && developmentSaveVersion === saveVersion) savingDevelopment = false;
		}
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{m['team_settings.title']()}</h1>
	<p class="mt-2 text-sm text-[var(--color-text-tertiary)]">
		{m['team_settings.description']()}
	</p>

	{#if loading}
		<div class="flex justify-center py-8">
			<div
				class="h-5 w-5 animate-spin rounded-full border-2 border-[var(--color-text-tertiary)] border-t-transparent"
			></div>
		</div>
	{:else if team}
		<TeamPrivacySettings {slug} {team} onupdated={(updated) => team = updated} />
		<div class="mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<div class="flex items-center justify-between gap-4 border-b border-[var(--app-border)] px-5 py-4">
				<div class="flex items-center gap-3">
					<div class="flex h-9 w-9 items-center justify-center rounded-lg bg-[var(--color-bg)]">
						<TeamIcon {team} size={18} />
					</div>
					<div>
						<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['team_settings.general']()}</p>
						<p class="text-xs text-[var(--color-text-tertiary)]">{m['team_settings.general_desc']()}</p>
					</div>
				</div>
				{#if !editingDetails}
					<Button variant="ghost" size="sm" onclick={startEditDetails}>{m['team_settings.edit']()}</Button>
				{/if}
			</div>
			<div class="space-y-4 px-5 py-4">
				{#if editingDetails}
					<div class="space-y-1.5">
						<Label class="text-xs text-[var(--color-text-secondary)]">{m['team_settings.name']()}</Label>
						<Input
							bind:value={editName}
							class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
						/>
					</div>
					<div class="space-y-1.5">
						<Label class="text-xs text-[var(--color-text-secondary)]">{m['team_settings.description_label']()}</Label>
						<Input
							bind:value={editDescription}
							placeholder={m['team_settings.description_placeholder']()}
							class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
						/>
					</div>
					<div class="flex justify-end gap-2">
						<Button variant="outline" size="sm" onclick={() => (editingDetails = false)}><X size={14} />{m['team_settings.cancel']()}</Button>
						<Button size="sm" onclick={saveDetails} disabled={!editName.trim()}><Check size={14} />{m['team_settings.save']()}</Button>
					</div>
				{:else}
					<div class="grid gap-3 text-sm">
						<div>
							<p class="text-xs text-[var(--color-text-tertiary)]">{m['team_settings.name']()}</p>
							<p class="text-[var(--color-text-primary)]">{team.name}</p>
						</div>
						<div>
							<p class="text-xs text-[var(--color-text-tertiary)]">{m['team_settings.identifier']()}</p>
							<p class="font-mono text-[var(--color-text-primary)]">{team.key}</p>
						</div>
						<div>
							<p class="text-xs text-[var(--color-text-tertiary)]">{m['team_settings.description_label']()}</p>
							<p class="text-[var(--color-text-primary)]">{team.description || m['team_settings.no_description']()}</p>
						</div>
					</div>
				{/if}
			</div>
		</div>

		<div class="mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<div class="border-b border-[var(--app-border)] px-5 py-4"><p class="text-sm font-medium text-[var(--color-text-primary)]">{m['team_settings.development_defaults']()}</p><p class="text-xs text-[var(--color-text-tertiary)]">{m['team_settings.development_defaults_desc']()}</p></div>
			<div class="grid gap-4 px-5 py-4 sm:grid-cols-2">
				{#if !canManageDevelopment}<p class="rounded-md border border-[var(--app-border)] p-3 text-xs text-[var(--color-text-tertiary)] sm:col-span-2">{privacyEnabled ? m['privacy.disabled_feature']() : m['team_settings.development_admin_only']()}</p>{/if}
				<div class="space-y-1"><Label>{m['team_settings.repository']()}</Label><Select.Root type="single" value={developmentRepositoryId} disabled={developmentLoading || !developmentReady || !canManageDevelopment} onValueChange={(value) => value && (developmentRepositoryId = value)}><Select.Trigger class="w-full">{developmentLoading ? m['team_settings.loading']() : developmentRepositories.find((item) => item.id === developmentRepositoryId)?.full_name ?? m['team_settings.use_workspace_default']()}</Select.Trigger><Select.Content><Select.Item value="inherit" label={m['team_settings.use_workspace_default']()}>{m['team_settings.use_workspace_default']()}</Select.Item>{#each developmentRepositories as repository}<Select.Item value={repository.id} label={repository.full_name}>{repository.full_name}</Select.Item>{/each}</Select.Content></Select.Root></div>
				<div class="space-y-1"><Label>{m['team_settings.environment']()}</Label><Select.Root type="single" value={developmentEnvironmentId} disabled={developmentLoading || !developmentReady || !canManageDevelopment} onValueChange={(value) => value && (developmentEnvironmentId = value)}><Select.Trigger class="w-full">{developmentLoading ? m['team_settings.loading']() : developmentEnvironments.find((item) => item.id === developmentEnvironmentId)?.name ?? m['team_settings.use_workspace_default']()}</Select.Trigger><Select.Content><Select.Item value="inherit" label={m['team_settings.use_workspace_default']()}>{m['team_settings.use_workspace_default']()}</Select.Item>{#each developmentEnvironments as environment}<Select.Item value={environment.id} label={environment.name}>{environment.name}</Select.Item>{/each}</Select.Content></Select.Root></div>
				<div class="flex justify-end sm:col-span-2"><Button size="sm" onclick={saveDevelopmentSettings} disabled={developmentLoading || !developmentReady || savingDevelopment || !canManageDevelopment}>{savingDevelopment ? m['team_settings.saving']() : m['team_settings.save_development_defaults']()}</Button></div>
			</div>
		</div>

		<div class="mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<div class="border-b border-[var(--app-border)] px-5 py-4">
				<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['team_settings.appearance']()}</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">{m['team_settings.appearance_desc']()}</p>
			</div>
			<div class="space-y-4 px-5 py-4">
				<div>
					<p class="mb-2 text-xs font-medium text-[var(--color-text-secondary)]">{m['team_settings.icon']()}</p>
					<Popover.Root bind:open={pickerOpen}>
						<Popover.Trigger>
							<button
								type="button"
								class="flex items-center gap-3 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-left transition-colors hover:bg-[var(--color-bg-hover)]"
							>
								<span class="flex h-9 w-9 items-center justify-center rounded-md bg-[var(--color-bg-secondary)]">
									<TeamIcon {team} size={20} />
								</span>
								<span>
									<span class="block text-sm text-[var(--color-text-primary)]">{m['team_settings.choose_icon_or_emoji']()}</span>
									<span class="block text-xs text-[var(--color-text-tertiary)]"
										>{m['team_settings.icons_preview_desc']()}</span
									>
								</span>
							</button>
						</Popover.Trigger>
						<Popover.Content class="w-72 p-0" align="start">
							<div class="border-b border-[var(--app-border)] p-2">
								<div class="relative">
									<Search
										size={14}
										class="absolute left-2 top-1/2 -translate-y-1/2 text-[var(--color-text-tertiary)]"
									/>
									<input
										bind:value={pickerQuery}
										placeholder={pickerTab === 'icons' ? m['team_settings.search_lucide_icons']() : m['team_settings.search_emoji']()}
										class="h-8 w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] pl-7 pr-2 text-sm text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)] focus:border-[var(--app-accent)]"
									/>
								</div>
								<div class="mt-2 grid grid-cols-2 gap-1 rounded-md bg-[var(--color-bg)] p-0.5">
									<button
										type="button"
										onclick={() => showPickerTab('icons')}
										class="rounded px-2 py-1 text-xs {pickerTab === 'icons'
											? 'bg-[var(--color-bg-secondary)] text-[var(--color-text-primary)]'
											: 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]'}"
									>
										{m['team_settings.icons_tab']()}
									</button>
									<button
										type="button"
										onclick={() => showPickerTab('emoji')}
										class="rounded px-2 py-1 text-xs {pickerTab === 'emoji'
											? 'bg-[var(--color-bg-secondary)] text-[var(--color-text-primary)]'
											: 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]'}"
									>
										{m['team_settings.emoji_tab']()}
									</button>
								</div>
							</div>
							<div class="max-h-72 overflow-y-auto p-2" onscroll={handlePickerScroll}>
								{#if pickerTab === 'icons' && filteredIcons.length > 0}
									<p
										class="px-1 pb-1 text-[10px] font-semibold uppercase tracking-wide text-[var(--color-text-tertiary)]"
									>
										{m['team_settings.icons_tab']()} · {filteredIcons.length}
									</p>
									<div class="grid grid-cols-8 gap-1">
										{#each visibleIcons as option}
											{@const Icon = option.icon}
											<button
												type="button"
												onclick={() => selectIcon(option.value)}
												class="flex h-7 items-center justify-center rounded border transition-colors {selectedIconName ===
												option.value
													? 'border-[var(--app-accent)] bg-[var(--app-accent)]/10'
													: 'border-transparent hover:border-[var(--app-border)] hover:bg-[var(--color-bg-hover)]'}"
												aria-label={m['team_settings.use_icon_aria']({ label: option.label })}
											>
												<Icon size={15} style="color: {selectedColor}" />
											</button>
										{/each}
									</div>
									{#if visibleIcons.length < filteredIcons.length}
										<p class="py-2 text-center text-[10px] text-[var(--color-text-tertiary)]">{m['team_settings.scroll_for_more_icons']()}</p>
									{/if}
								{/if}
								{#if pickerTab === 'emoji'}
									{#if !pickerQuery.trim()}
										<div class="mb-2 flex gap-1 overflow-x-auto pb-3" onwheel={handleHorizontalWheel}>
											{#each EMOJI_GROUPS as group}
												<button
													type="button"
													onclick={() => {
														emojiGroup = group.id;
														visibleEmojiCount = EMOJI_PAGE_SIZE;
													}}
													class="shrink-0 rounded-full px-2 py-0.5 text-[10px] {emojiGroup === group.id
														? 'bg-[var(--app-accent)]/10 text-[var(--app-accent-light)]'
														: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
													>
														{dynamicMessage[group.labelKey]()}
												</button>
											{/each}
										</div>
									{/if}
									<p
										class="px-1 pb-1 text-[10px] font-semibold uppercase tracking-wide text-[var(--color-text-tertiary)]"
									>
										{pickerQuery.trim()
											? m['team_settings.emoji_search_label']()
											: dynamicMessage[EMOJI_GROUPS.find((group) => group.id === emojiGroup)?.labelKey ?? 'team_settings.emoji_default_label']()} · {emojiResults.length}
									</p>
									{#if emojiLoading}
										<div class="flex justify-center py-6">
											<div
												class="h-4 w-4 animate-spin rounded-full border-2 border-[var(--color-text-tertiary)] border-t-transparent"
											></div>
										</div>
									{:else}
										<div class="grid grid-cols-9 gap-1">
											{#each visibleEmojis as emoji}
												{@const unicode = emojiUnicode(emoji)}
												<button
													type="button"
													onclick={() => selectEmoji(emoji)}
													class="flex h-8 items-center justify-center rounded-md border text-lg transition-colors {selectedEmoji ===
													unicode
														? 'border-[var(--app-accent)] bg-[var(--app-accent)]/10'
														: 'border-transparent hover:border-[var(--app-border)] hover:bg-[var(--color-bg-hover)]'}"
													aria-label={m['team_settings.use_emoji_aria']({ label: emojiLabel(emoji) })}
												>
													{unicode}
												</button>
											{/each}
										</div>
										{#if visibleEmojis.length < emojiResults.length}
											<p class="py-2 text-center text-[10px] text-[var(--color-text-tertiary)]">
												{m['team_settings.scroll_for_more_emoji']()}
											</p>
										{/if}
									{/if}
								{/if}
							</div>
						</Popover.Content>
					</Popover.Root>
				</div>
				<div>
					<p class="mb-2 text-xs font-medium text-[var(--color-text-secondary)]">{m['team_settings.color']()}</p>
					<div class="flex flex-wrap gap-1.5">
						{#each PRESET_COLORS as c}
							<button
								type="button"
								onclick={() => updateTeamVisual({ color: c })}
								class="h-6 w-6 rounded-full transition-transform hover:scale-110 {team.color === c
									? 'ring-2 ring-[var(--app-accent)] ring-offset-2 ring-offset-[var(--color-bg-secondary)]'
									: ''}"
								style="background-color: {c}"
								aria-label={m['team_settings.use_color_aria']({ color: c })}
							></button>
						{/each}
					</div>
				</div>
			</div>
		</div>

		<div class="mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<div class="border-b border-[var(--app-border)] px-5 py-4">
				<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['team_settings.sub_issue_automation']()}</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">
					{m['team_settings.sub_issue_automation_desc']()}
				</p>
			</div>
			<label class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm text-[var(--color-text-primary)]">{m['team_settings.parent_auto_close']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">
						{m['team_settings.parent_auto_close_desc']()}
					</p>
				</div>
				<Switch
					checked={team.parent_auto_close_enabled}
					onCheckedChange={(value) => updateSubIssueAutomation('parent_auto_close_enabled', value)}
				/>
			</label>
			<label class="flex items-center justify-between gap-4 border-t border-[var(--app-border)] px-5 py-4">
				<div>
					<p class="text-sm text-[var(--color-text-primary)]">{m['team_settings.sub_issue_auto_close']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">
						{m['team_settings.sub_issue_auto_close_desc']()}
					</p>
				</div>
				<Switch
					checked={team.sub_issue_auto_close_enabled}
					onCheckedChange={(value) => updateSubIssueAutomation('sub_issue_auto_close_enabled', value)}
				/>
			</label>
		</div>

		<div class="mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<div class="border-b border-[var(--app-border)] px-5 py-4">
				<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['team_settings.issue_copy_prompt']()}</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">
					{m['team_settings.issue_copy_prompt_desc']()}
				</p>
			</div>
			<div class="space-y-3 px-5 py-4">
				<textarea
					bind:value={issueCopyPrompt}
					rows="8"
					placeholder={m['team_settings.issue_copy_prompt_placeholder']()}
					class="w-full rounded-lg border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 font-mono text-sm text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)] focus:border-[var(--app-accent)]"
				></textarea>
				<p class="text-xs text-[var(--color-text-tertiary)]">
					{m['team_settings.available_placeholders']()} {'{{issue_identifier}}'}, {'{{issue_title}}'}, {'{{team_key}}'}, {'{{team_name}}'}, {'{{issue_xml}}'}.
				</p>
				<div class="flex justify-end gap-2">
					<Button variant="outline" size="sm" onclick={() => (issueCopyPrompt = '')}>{m['team_settings.use_workspace_default']()}</Button>
					<Button size="sm" onclick={saveIssueCopyPrompt} disabled={savingIssueCopyPrompt}>
						{savingIssueCopyPrompt ? m['team_settings.saving']() : m['team_settings.save_prompt']()}
					</Button>
				</div>
			</div>
		</div>
	{/if}
</div>
