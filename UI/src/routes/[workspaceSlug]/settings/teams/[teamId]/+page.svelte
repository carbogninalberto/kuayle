<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import type { Team } from '$lib/types/team';
	import { listTeams, updateTeam } from '$lib/api/teams';
	import TeamIcon from '$lib/components/shared/TeamIcon.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import * as Popover from '$lib/components/ui/popover';
	import { appToast } from '$lib/features/toast/toast';
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

	const PRESET_COLORS = ['#ef4444', '#f97316', '#eab308', '#22c55e', '#06b6d4', '#3b82f6', '#8b5cf6', '#ec4899'];
	const ICON_PAGE_SIZE = 96;
	const EMOJI_PAGE_SIZE = 144;
	const excludedLucideExports = new Set(['Icon', 'LucideIcon', 'default']);
	const lucideExports = LucideIcons as unknown as Record<string, Component>;
	const EMOJI_GROUPS = [
		{ id: 0, label: 'Smileys' },
		{ id: 1, label: 'People' },
		{ id: 3, label: 'Nature' },
		{ id: 4, label: 'Food' },
		{ id: 6, label: 'Travel' },
		{ id: 5, label: 'Activities' },
		{ id: 7, label: 'Objects' },
		{ id: 8, label: 'Symbols' },
		{ id: 9, label: 'Flags' }
	];

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

	$effect(() => {
		const s = slug;
		const t = teamId;
		if (!s || !t) return;
		loading = true;
		editingDetails = false;
		listTeams(s)
			.then((teams) => {
				team = teams.find((tm) => tm.id === t) ?? null;
				issueCopyPrompt = team?.issue_copy_prompt ?? '';
			})
			.catch(() => {
				appToast.error('Failed to load team');
			})
			.finally(() => {
				loading = false;
			});
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
			appToast.success('Team details updated');
		} catch (err: any) {
			team = previous;
			appToast.apiError(err, 'Failed to update team details');
		}
	}

	async function updateTeamVisual(data: { color?: string; icon?: string }) {
		if (!team) return;
		const previous = team;
		team = { ...team, ...data };
		try {
			team = await updateTeam(slug, teamId, data);
			appToast.success('Team appearance updated');
		} catch (err: any) {
			team = previous;
			appToast.apiError(err, 'Failed to update team appearance');
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
			appToast.success('Sub-issue automation updated');
		} catch (err: any) {
			team = previous;
			appToast.apiError(err, 'Failed to update automation');
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
			appToast.success('Issue copy prompt updated');
		} catch (err: any) {
			team = previous;
			appToast.apiError(err, 'Failed to update issue copy prompt');
		} finally {
			savingIssueCopyPrompt = false;
		}
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">Team settings</h1>
	<p class="mt-2 text-sm text-[var(--color-text-tertiary)]">
		Manage the team's basic details, sidebar appearance, and automation.
	</p>

	{#if loading}
		<div class="flex justify-center py-8">
			<div
				class="h-5 w-5 animate-spin rounded-full border-2 border-[var(--color-text-tertiary)] border-t-transparent"
			></div>
		</div>
	{:else if team}
		<div class="mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<div class="flex items-center justify-between gap-4 border-b border-[var(--app-border)] px-5 py-4">
				<div class="flex items-center gap-3">
					<div class="flex h-9 w-9 items-center justify-center rounded-lg bg-[var(--color-bg)]">
						<TeamIcon {team} size={18} />
					</div>
					<div>
						<p class="text-sm font-medium text-[var(--color-text-primary)]">General</p>
						<p class="text-xs text-[var(--color-text-tertiary)]">Name, identifier, and description.</p>
					</div>
				</div>
				{#if !editingDetails}
					<Button variant="ghost" size="sm" onclick={startEditDetails}>Edit</Button>
				{/if}
			</div>
			<div class="space-y-4 px-5 py-4">
				{#if editingDetails}
					<div class="space-y-1.5">
						<Label class="text-xs text-[var(--color-text-secondary)]">Name</Label>
						<Input
							bind:value={editName}
							class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
						/>
					</div>
					<div class="space-y-1.5">
						<Label class="text-xs text-[var(--color-text-secondary)]">Description</Label>
						<Input
							bind:value={editDescription}
							placeholder="What does this team work on?"
							class="bg-[var(--color-bg)] border-[var(--app-border)] text-[var(--color-text-primary)]"
						/>
					</div>
					<div class="flex justify-end gap-2">
						<Button variant="outline" size="sm" onclick={() => (editingDetails = false)}><X size={14} />Cancel</Button>
						<Button size="sm" onclick={saveDetails} disabled={!editName.trim()}><Check size={14} />Save</Button>
					</div>
				{:else}
					<div class="grid gap-3 text-sm">
						<div>
							<p class="text-xs text-[var(--color-text-tertiary)]">Name</p>
							<p class="text-[var(--color-text-primary)]">{team.name}</p>
						</div>
						<div>
							<p class="text-xs text-[var(--color-text-tertiary)]">Identifier</p>
							<p class="font-mono text-[var(--color-text-primary)]">{team.key}</p>
						</div>
						<div>
							<p class="text-xs text-[var(--color-text-tertiary)]">Description</p>
							<p class="text-[var(--color-text-primary)]">{team.description || 'No description'}</p>
						</div>
					</div>
				{/if}
			</div>
		</div>

		<div class="mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<div class="border-b border-[var(--app-border)] px-5 py-4">
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Appearance</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Customize the team icon and sidebar color.</p>
			</div>
			<div class="space-y-4 px-5 py-4">
				<div>
					<p class="mb-2 text-xs font-medium text-[var(--color-text-secondary)]">Icon</p>
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
									<span class="block text-sm text-[var(--color-text-primary)]">Choose icon or emoji</span>
									<span class="block text-xs text-[var(--color-text-tertiary)]"
										>Icons preview with the selected team color.</span
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
										placeholder={pickerTab === 'icons' ? 'Search Lucide icons' : 'Search emoji'}
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
										Icons
									</button>
									<button
										type="button"
										onclick={() => showPickerTab('emoji')}
										class="rounded px-2 py-1 text-xs {pickerTab === 'emoji'
											? 'bg-[var(--color-bg-secondary)] text-[var(--color-text-primary)]'
											: 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]'}"
									>
										Emoji
									</button>
								</div>
							</div>
							<div class="max-h-72 overflow-y-auto p-2" onscroll={handlePickerScroll}>
								{#if pickerTab === 'icons' && filteredIcons.length > 0}
									<p
										class="px-1 pb-1 text-[10px] font-semibold uppercase tracking-wide text-[var(--color-text-tertiary)]"
									>
										Icons · {filteredIcons.length}
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
												aria-label="Use {option.label} icon"
											>
												<Icon size={15} style="color: {selectedColor}" />
											</button>
										{/each}
									</div>
									{#if visibleIcons.length < filteredIcons.length}
										<p class="py-2 text-center text-[10px] text-[var(--color-text-tertiary)]">Scroll for more icons</p>
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
													{group.label}
												</button>
											{/each}
										</div>
									{/if}
									<p
										class="px-1 pb-1 text-[10px] font-semibold uppercase tracking-wide text-[var(--color-text-tertiary)]"
									>
										{pickerQuery.trim()
											? 'Search'
											: (EMOJI_GROUPS.find((group) => group.id === emojiGroup)?.label ?? 'Emoji')} · {emojiResults.length}
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
													aria-label="Use {emojiLabel(emoji)} emoji"
												>
													{unicode}
												</button>
											{/each}
										</div>
										{#if visibleEmojis.length < emojiResults.length}
											<p class="py-2 text-center text-[10px] text-[var(--color-text-tertiary)]">
												Scroll for more emoji
											</p>
										{/if}
									{/if}
								{/if}
							</div>
						</Popover.Content>
					</Popover.Root>
				</div>
				<div>
					<p class="mb-2 text-xs font-medium text-[var(--color-text-secondary)]">Color</p>
					<div class="flex flex-wrap gap-1.5">
						{#each PRESET_COLORS as c}
							<button
								type="button"
								onclick={() => updateTeamVisual({ color: c })}
								class="h-6 w-6 rounded-full transition-transform hover:scale-110 {team.color === c
									? 'ring-2 ring-[var(--app-accent)] ring-offset-2 ring-offset-[var(--color-bg-secondary)]'
									: ''}"
								style="background-color: {c}"
								aria-label="Use color {c}"
							></button>
						{/each}
					</div>
				</div>
			</div>
		</div>

		<div class="mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<div class="border-b border-[var(--app-border)] px-5 py-4">
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Sub-issue automation</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">
					Automatically keep parent and sub-issue statuses in sync for this team.
				</p>
			</div>
			<label class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm text-[var(--color-text-primary)]">Parent auto-close</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">
						Move a parent issue to done when all direct sub-issues are completed or cancelled.
					</p>
				</div>
				<Switch
					checked={team.parent_auto_close_enabled}
					onCheckedChange={(value) => updateSubIssueAutomation('parent_auto_close_enabled', value)}
				/>
			</label>
			<label class="flex items-center justify-between gap-4 border-t border-[var(--app-border)] px-5 py-4">
				<div>
					<p class="text-sm text-[var(--color-text-primary)]">Sub-issue auto-close</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">
						Move remaining open direct sub-issues to done when their parent is completed.
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
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Issue copy prompt</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">
					Override the workspace AI prompt template for this team. Leave empty to use the workspace default.
				</p>
			</div>
			<div class="space-y-3 px-5 py-4">
				<textarea
					bind:value={issueCopyPrompt}
					rows="8"
					placeholder="Use workspace issue copy prompt"
					class="w-full rounded-lg border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 font-mono text-sm text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)] focus:border-[var(--app-accent)]"
				></textarea>
				<p class="text-xs text-[var(--color-text-tertiary)]">
					Available placeholders: {'{{issue_identifier}}'}, {'{{issue_title}}'}, {'{{team_key}}'}, {'{{team_name}}'}, {'{{issue_xml}}'}.
				</p>
				<div class="flex justify-end gap-2">
					<Button variant="outline" size="sm" onclick={() => (issueCopyPrompt = '')}>Use workspace default</Button>
					<Button size="sm" onclick={saveIssueCopyPrompt} disabled={savingIssueCopyPrompt}>
						{savingIssueCopyPrompt ? 'Saving...' : 'Save prompt'}
					</Button>
				</div>
			</div>
		</div>
	{/if}
</div>
