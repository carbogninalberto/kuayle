<script lang="ts">
	import type { Component } from 'svelte';
	import * as LucideIcons from '@lucide/svelte';
	import { Search } from '@lucide/svelte';
	import Database from 'emoji-picker-element/database';
	import type { Emoji } from 'emoji-picker-element/shared';
	import type { Project } from '$lib/types/project';
	import ProjectIcon from '$lib/components/shared/ProjectIcon.svelte';
	import TeamIconGlyph from '$lib/components/shared/TeamIconGlyph.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import * as Popover from '$lib/components/ui/popover';
	import { m } from '$lib/paraglide/messages.js';

	let {
		project,
		disabled = false,
		onchange
	}: {
		project: Pick<Project, 'icon' | 'color'>;
		disabled?: boolean;
		/** An empty string clears the value. */
		onchange: (change: { icon?: string; color?: string }) => void;
	} = $props();

	const PRESET_COLORS = ['#ef4444', '#f97316', '#eab308', '#22c55e', '#06b6d4', '#3b82f6', '#8b5cf6', '#ec4899'];
	const ICON_PAGE_SIZE = 96;
	const EMOJI_PAGE_SIZE = 144;
	const EMOJI_GROUPS: { id: number; label: () => string }[] = [
		{ id: 0, label: () => m['team_settings.emoji_group.smileys']() },
		{ id: 1, label: () => m['team_settings.emoji_group.people']() },
		{ id: 3, label: () => m['team_settings.emoji_group.nature']() },
		{ id: 4, label: () => m['team_settings.emoji_group.food']() },
		{ id: 6, label: () => m['team_settings.emoji_group.travel']() },
		{ id: 5, label: () => m['team_settings.emoji_group.activities']() },
		{ id: 7, label: () => m['team_settings.emoji_group.objects']() },
		{ id: 8, label: () => m['team_settings.emoji_group.symbols']() },
		{ id: 9, label: () => m['team_settings.emoji_group.flags']() }
	];
	const excluded = new Set(['Icon', 'LucideIcon', 'default']);
	const lucideExports = LucideIcons as unknown as Record<string, Component>;
	const ICON_OPTIONS = Object.entries(lucideExports)
		.filter(([name, value]) => /^[A-Z]/.test(name) && !excluded.has(name) && typeof value === 'function')
		.map(([name]) => ({ value: name, label: name.replace(/([a-z0-9])([A-Z])/g, '$1 $2') }))
		.sort((a, b) => a.label.localeCompare(b.label));

	let open = $state(false);
	let tab = $state<'icons' | 'emoji'>('icons');
	let query = $state('');
	let visibleIcons = $state(ICON_PAGE_SIZE);
	let emojiGroup = $state(0);
	let emojiResults = $state<Emoji[]>([]);
	let emojiDatabase: Database | null = null;
	let emojiRequest = 0;

	const selectedEmoji = $derived(project.icon?.startsWith('emoji:') ? project.icon.slice(6) : '');
	const selectedIcon = $derived(project.icon && !selectedEmoji ? project.icon : 'FolderKanban');
	const filteredIcons = $derived(
		open && tab === 'icons'
			? ICON_OPTIONS.filter((o) => {
					const q = query.trim().toLowerCase();
					return !q || o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q);
				})
			: []
	);

	function emojiUnicode(emoji: Emoji): string {
		return 'unicode' in emoji ? emoji.unicode : emoji.name;
	}

	function emojiLabel(emoji: Emoji): string {
		return 'annotation' in emoji ? emoji.annotation : emoji.name;
	}

	function pickIcon(icon: string) {
		onchange({ icon });
		open = false;
	}

	function pickEmoji(emoji: Emoji) {
		pickIcon(`emoji:${emojiUnicode(emoji)}`);
		if ('unicode' in emoji) emojiDatabase?.incrementFavoriteEmojiCount(emoji.unicode);
	}

	function showTab(next: 'icons' | 'emoji') {
		tab = next;
		query = '';
		visibleIcons = ICON_PAGE_SIZE;
	}

	function onScroll(e: Event) {
		const el = e.currentTarget as HTMLElement;
		if (el.scrollTop + el.clientHeight < el.scrollHeight - 80) return;
		if (tab === 'icons' && visibleIcons < filteredIcons.length) visibleIcons += ICON_PAGE_SIZE;
	}

	async function loadEmoji() {
		if (!open || tab !== 'emoji') return;
		// Created on first use: the database opens IndexedDB and may download emoji data.
		emojiDatabase ??= new Database();
		const request = ++emojiRequest;
		try {
			const q = query.trim();
			const results = q ? await emojiDatabase.getEmojiBySearchQuery(q) : await emojiDatabase.getEmojiByGroup(emojiGroup);
			if (request === emojiRequest) emojiResults = results.slice(0, EMOJI_PAGE_SIZE);
		} catch {
			if (request === emojiRequest) emojiResults = [];
		}
	}

	$effect(() => {
		open;
		tab;
		query;
		emojiGroup;
		void loadEmoji();
	});

	$effect(() => {
		query;
		visibleIcons = ICON_PAGE_SIZE;
	});

</script>

<Popover.Root bind:open>
	<Popover.Trigger {disabled}>
		{#snippet child({ props })}
			<Button
				{...props}
				variant="plain"
				type="button"
				{disabled}
				aria-label={m['projects.appearance']()}
				data-testid="project-appearance-trigger"
				class="flex size-7 shrink-0 items-center justify-center rounded-md hover:bg-[var(--color-bg-hover)] disabled:cursor-default disabled:hover:bg-transparent"
			>
				<ProjectIcon {project} size={16} />
			</Button>
		{/snippet}
	</Popover.Trigger>
	<Popover.Content class="w-72 p-0" align="start">
		<div class="border-b border-[var(--app-border)] p-2">
			<div class="relative">
				<Search size={14} class="absolute left-2 top-1/2 -translate-y-1/2 text-[var(--color-text-tertiary)]" />
				<Input
					variant="plain"
					bind:value={query}
					placeholder={tab === 'icons' ? m['team_settings.search_lucide_icons']() : m['team_settings.search_emoji']()}
					class="h-8 w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] pl-7 pr-2 text-sm text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)] focus:border-[var(--app-accent)]"
				/>
			</div>
			<div class="mt-2 grid grid-cols-2 gap-1 rounded-md bg-[var(--color-bg)] p-0.5">
				{#each [['icons', m['team_settings.icons_tab']()], ['emoji', m['team_settings.emoji_tab']()]] as [id, label]}
					<Button
						variant="plain"
						type="button"
						onclick={() => showTab(id as 'icons' | 'emoji')}
						class="rounded px-2 py-1 text-xs {tab === id
							? 'bg-[var(--color-bg-secondary)] text-[var(--color-text-primary)]'
							: 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]'}">{label}</Button
					>
				{/each}
			</div>
		</div>
		<div class="max-h-60 overflow-y-auto p-2" onscroll={onScroll}>
			{#if tab === 'icons'}
				<div class="grid grid-cols-8 gap-1">
					{#each filteredIcons.slice(0, visibleIcons) as option (option.value)}
						<Button
							variant="plain"
							type="button"
							onclick={() => pickIcon(option.value)}
							aria-label={m['team_settings.use_icon_aria']({ label: option.label })}
							class="flex h-7 items-center justify-center rounded border transition-colors {selectedIcon === option.value
								? 'border-[var(--app-accent)] bg-[var(--app-accent)]/10'
								: 'border-transparent hover:border-[var(--app-border)] hover:bg-[var(--color-bg-hover)]'}"
						>
							<TeamIconGlyph name={option.value} size={15} style="color: {project.color ?? 'var(--color-text-secondary)'}" />
						</Button>
					{/each}
				</div>
			{:else}
				{#if !query.trim()}
					<div class="mb-2 flex gap-1 overflow-x-auto pb-2">
						{#each EMOJI_GROUPS as group}
							<Button
								variant="plain"
								type="button"
								onclick={() => (emojiGroup = group.id)}
								class="shrink-0 rounded-full px-2 py-0.5 text-[10px] {emojiGroup === group.id
									? 'bg-[var(--app-accent)]/10 text-[var(--app-accent-light)]'
									: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]'}"
								>{group.label()}</Button
							>
						{/each}
					</div>
				{/if}
				<div class="grid grid-cols-9 gap-1">
					{#each emojiResults as emoji (emojiUnicode(emoji))}
						{@const unicode = emojiUnicode(emoji)}
						<Button
							variant="plain"
							type="button"
							onclick={() => pickEmoji(emoji)}
							aria-label={m['team_settings.use_emoji_aria']({ label: emojiLabel(emoji) })}
							class="flex h-8 items-center justify-center rounded-md border text-lg transition-colors {selectedEmoji === unicode
								? 'border-[var(--app-accent)] bg-[var(--app-accent)]/10'
								: 'border-transparent hover:border-[var(--app-border)] hover:bg-[var(--color-bg-hover)]'}">{unicode}</Button
						>
					{/each}
				</div>
			{/if}
		</div>
		<div class="flex flex-wrap items-center gap-1.5 border-t border-[var(--app-border)] p-2">
			{#each PRESET_COLORS as c}
				<Button
					variant="plain"
					type="button"
					onclick={() => onchange({ color: c })}
					aria-label={m['team_settings.use_color_aria']({ color: c })}
					class="size-5 rounded-full transition-transform hover:scale-110 {project.color === c
						? 'ring-2 ring-[var(--app-accent)] ring-offset-2 ring-offset-[var(--color-bg)]'
						: ''}"
					style="background-color: {c}"
				></Button>
			{/each}
			<Button
				variant="plain"
				type="button"
				onclick={() => {
					onchange({ icon: '', color: '' });
					open = false;
				}}
				class="ml-auto rounded px-1.5 py-0.5 text-[11px] text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
				>{m['projects.appearance_reset']()}</Button
			>
		</div>
	</Popover.Content>
</Popover.Root>
