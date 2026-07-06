<script lang="ts">
	import { goto } from '$app/navigation';
	import type { Team } from '$lib/types/team';
	import type { Issue } from '$lib/types/issue';
	import { listIssues } from '$lib/api/issues';
	import { Kbd } from '$lib/components/ui/kbd';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
	import IssuePriorityIcon from '$lib/features/issues/IssuePriorityIcon.svelte';
	import { LoaderCircle } from 'lucide-svelte';
	import { onMount } from 'svelte';

	let { slug, teams, onclose }: { slug: string; teams: Team[]; onclose: () => void } = $props();
	let search = $state('');
	let selectedIndex = $state(0);
	let issueResults = $state<Issue[]>([]);
	let issueLoading = $state(false);
	let debounceTimer: ReturnType<typeof setTimeout> | undefined;

	const ANIM_DURATION = 100;
	let visible = $state(false);
	let closing = false;

	onMount(() => {
		requestAnimationFrame(() => {
			requestAnimationFrame(() => {
				visible = true;
			});
		});
	});

	function close() {
		if (closing) return;
		closing = true;
		visible = false;
		setTimeout(onclose, ANIM_DURATION);
	}

	interface CommandItem {
		label: string;
		description?: string;
		action: () => void;
	}

	interface HighlightSegment {
		text: string;
		match: boolean;
	}

	const commands: CommandItem[] = $derived.by(() => {
		const items: CommandItem[] = [
			{ label: 'Go to Inbox', action: () => navigate(`/${slug}/inbox`) },
			{ label: 'Go to My Issues', action: () => navigate(`/${slug}/my-issues`) },
			{ label: 'Go to Projects', action: () => navigate(`/${slug}/projects`) },
			{ label: 'Go to Settings', action: () => navigate(`/${slug}/settings`) },
			...teams.map((t) => ({
				label: `Go to ${t.name}`,
				description: t.key,
				action: () => navigate(`/${slug}/teams/${t.id}`)
			}))
		];

		if (!search) return items;
		return items.filter((i) => i.label.toLowerCase().includes(search.toLowerCase()));
	});

	const totalItems = $derived(commands.length + issueResults.length);
	const shortcuts = [
		{ keys: ['↑', '↓'], label: 'Move selection' },
		{ keys: ['Enter'], label: 'Open selected' },
		{ keys: ['⌥', '1-9'], label: 'Quick open' },
		{ keys: ['Esc'], label: 'Close' }
	];
	const hanRegex = /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/;

	function canSearchIssues(value: string) {
		const query = value.trim();
		return query.length >= 2 || hanRegex.test(query);
	}

	$effect(() => {
		if (canSearchIssues(search)) {
			clearTimeout(debounceTimer);
			debounceTimer = setTimeout(async () => {
				issueLoading = true;
				try {
					const res = await listIssues(slug, { search, per_page: '12' });
					issueResults = res.data;
				} catch {
					issueResults = [];
				} finally {
					issueLoading = false;
				}
			}, 300);
		} else {
			issueResults = [];
			issueLoading = false;
		}
		selectedIndex = 0;
	});

	function navigate(path: string) {
		goto(path);
		close();
	}

	function plainText(html: string | null | undefined) {
		if (!html) return '';

		if (typeof document !== 'undefined') {
			const el = document.createElement('div');
			el.innerHTML = html;
			return (el.textContent ?? '').replace(/\s+/g, ' ').trim();
		}

		return html
			.replace(/<[^>]*>/g, ' ')
			.replace(/&nbsp;/g, ' ')
			.replace(/&amp;/g, '&')
			.replace(/&lt;/g, '<')
			.replace(/&gt;/g, '>')
			.replace(/&quot;/g, '"')
			.replace(/&#39;/g, "'")
			.replace(/\s+/g, ' ')
			.trim();
	}

	function highlightedSegments(value: string | null | undefined, query: string): HighlightSegment[] {
		const text = value ?? '';
		const term = query.trim();
		if (!text || !term) return [{ text, match: false }];

		const lowerText = text.toLowerCase();
		const lowerTerm = term.toLowerCase();
		const segments: HighlightSegment[] = [];
		let cursor = 0;
		let index = lowerText.indexOf(lowerTerm);

		while (index !== -1) {
			if (index > cursor) {
				segments.push({ text: text.slice(cursor, index), match: false });
			}
			segments.push({ text: text.slice(index, index + term.length), match: true });
			cursor = index + term.length;
			index = lowerText.indexOf(lowerTerm, cursor);
		}

		if (cursor < text.length) {
			segments.push({ text: text.slice(cursor), match: false });
		}

		return segments;
	}

	function highlightClass(match: boolean) {
		return match ? 'rounded-sm bg-yellow-300 px-0.5 text-slate-950' : '';
	}

	function descriptionSnippet(description: string | null, query: string) {
		const text = plainText(description);
		const term = query.trim();
		if (!text || !term) return '';

		const matchIndex = text.toLowerCase().indexOf(term.toLowerCase());
		if (matchIndex === -1) return '';

		const context = 56;
		const start = Math.max(0, matchIndex - context);
		const end = Math.min(text.length, matchIndex + term.length + context);
		const prefix = start > 0 ? '...' : '';
		const suffix = end < text.length ? '...' : '';

		return `${prefix}${text.slice(start, end).trim()}${suffix}`;
	}

	function activateIndex(index: number) {
		if (index < 0 || index >= totalItems) return;

		if (index < commands.length) {
			commands[index]?.action();
			return;
		}

		const issue = issueResults[index - commands.length];
		if (issue) navigate(`/${slug}/issue/${issue.identifier}`);
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.altKey && /^[1-9]$/.test(e.key)) {
			e.preventDefault();
			activateIndex(Number(e.key) - 1);
		} else if (e.key === 'Escape') {
			close();
		} else if (e.key === 'ArrowDown') {
			e.preventDefault();
			if (totalItems > 0) selectedIndex = Math.min(selectedIndex + 1, totalItems - 1);
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			if (totalItems > 0) selectedIndex = Math.max(selectedIndex - 1, 0);
		} else if (e.key === 'Enter') {
			e.preventDefault();
			activateIndex(selectedIndex);
		}
	}
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="fixed inset-0 z-50 flex items-start justify-center px-3 pt-[6vh]" onkeydown={handleKeydown}>
	<!-- Backdrop -->
	<button
		class="fixed inset-0 cursor-default"
		style="background: rgba(0,0,0,{visible ? 0.5 : 0}); transition: background {ANIM_DURATION}ms ease;"
		onclick={close}
		tabindex={-1}
		aria-label="Close"
	></button>

	<!-- Dialog -->
	<div
		class="relative z-10 w-full max-w-4xl overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] shadow-2xl"
		style="opacity: {visible ? 1 : 0}; transform: scale({visible
			? 1
			: 0.95}); transition: opacity {ANIM_DURATION}ms ease, transform {ANIM_DURATION}ms ease;"
	>
		<div class="grid md:grid-cols-[minmax(0,1fr)_15rem]">
			<div class="min-w-0">
				<!-- svelte-ignore a11y_autofocus -->
				<input
					type="text"
					bind:value={search}
					placeholder="Type a command or search..."
					autofocus
					class="w-full border-b border-[var(--app-border)] bg-transparent px-4 py-4 text-sm text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
				/>
				<div class="max-h-[68vh] min-h-[28rem] overflow-y-auto py-2">
					{#if commands.length > 0}
						<div class="px-3 py-1">
							<span class="text-[10px] font-medium uppercase text-[var(--color-text-tertiary)]">Commands</span>
						</div>
						{#each commands as cmd, i}
							<button
								class="flex w-full items-center gap-3 px-4 py-2 text-left text-sm {i === selectedIndex
									? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-secondary)]'}"
								onmouseenter={() => (selectedIndex = i)}
								onclick={() => cmd.action()}
							>
								<span class="min-w-0 flex-1 truncate">
									{#each highlightedSegments(cmd.label, search) as segment}
										<span class={highlightClass(segment.match)}>{segment.text}</span>
									{/each}
								</span>
								{#if cmd.description}
									<span class="text-xs text-[var(--color-text-tertiary)]">{cmd.description}</span>
								{/if}
								{#if i < 9}
									<Kbd
										class="ml-auto shrink-0 border border-[var(--app-border)] bg-[var(--color-bg-tertiary)] text-[var(--color-text-tertiary)]"
										>⌥{i + 1}</Kbd
									>
								{/if}
							</button>
						{/each}
					{/if}

					{#if canSearchIssues(search) && (issueLoading || issueResults.length > 0 || commands.length > 0)}
						<div class="px-3 py-1 {commands.length > 0 ? 'mt-1 border-t border-[var(--app-border)] pt-2' : ''}">
							<span class="text-[10px] font-medium uppercase text-[var(--color-text-tertiary)]">Issues</span>
						</div>
						{#if issueLoading}
							<div class="flex items-center justify-center py-4">
								<LoaderCircle size={16} class="animate-spin text-[var(--color-text-tertiary)]" />
							</div>
						{:else if issueResults.length > 0}
							{#each issueResults as issue, i}
								{@const idx = commands.length + i}
								{@const snippet = descriptionSnippet(issue.description, search)}
								<button
									class="flex w-full items-start gap-2 px-4 py-2 text-left text-sm {idx === selectedIndex
										? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
										: 'text-[var(--color-text-secondary)]'}"
									onmouseenter={() => (selectedIndex = idx)}
									onclick={() => navigate(`/${slug}/issue/${issue.identifier}`)}
								>
									<div class="mt-0.5 flex shrink-0 items-center gap-2">
										<IssuePriorityIcon priority={issue.priority} size={14} />
										<IssueStatusIcon status={issue.status} size={14} />
									</div>
									<div class="min-w-0 flex-1">
										<div class="flex min-w-0 items-center gap-2">
											<span class="shrink-0 text-xs text-[var(--color-text-tertiary)]">
												{#each highlightedSegments(issue.identifier, search) as segment}
													<span class={highlightClass(segment.match)}>{segment.text}</span>
												{/each}
											</span>
											<span class="min-w-0 flex-1 truncate">
												{#each highlightedSegments(issue.title, search) as segment}
													<span class={highlightClass(segment.match)}>{segment.text}</span>
												{/each}
											</span>
											{#if idx < 9}
												<Kbd
													class="shrink-0 border border-[var(--app-border)] bg-[var(--color-bg-tertiary)] text-[var(--color-text-tertiary)]"
													>⌥{idx + 1}</Kbd
												>
											{/if}
										</div>
										{#if snippet}
											<p class="mt-1 line-clamp-2 pr-8 text-xs leading-5 text-[var(--color-text-tertiary)]">
												{#each highlightedSegments(snippet, search) as segment}
													<span class={highlightClass(segment.match)}>{segment.text}</span>
												{/each}
											</p>
										{/if}
									</div>
								</button>
							{/each}
						{:else}
							<p class="px-4 py-2 text-sm text-[var(--color-text-tertiary)]">No issues found</p>
						{/if}
					{/if}

					{#if commands.length === 0 && issueResults.length === 0 && !issueLoading}
						<p class="px-4 py-2 text-sm text-[var(--color-text-tertiary)]">No results found</p>
					{/if}
				</div>
			</div>

			<aside class="hidden border-l border-[var(--app-border)] bg-[var(--color-bg-tertiary)]/35 p-4 md:block">
				<div class="mb-3 text-[10px] font-medium uppercase tracking-wide text-[var(--color-text-tertiary)]">
					Keyboard
				</div>
				<div class="space-y-3">
					{#each shortcuts as shortcut}
						<div class="flex items-center justify-between gap-3 text-xs text-[var(--color-text-secondary)]">
							<span>{shortcut.label}</span>
							<div class="flex shrink-0 items-center gap-1">
								{#each shortcut.keys as key}
									<Kbd
										class="border border-[var(--app-border)] bg-[var(--color-bg-secondary)] text-[var(--color-text-secondary)]"
										>{key}</Kbd
									>
								{/each}
							</div>
						</div>
					{/each}
				</div>

				<div class="mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]/70 p-3">
					<div class="text-xs font-medium text-[var(--color-text-primary)]">Search matches</div>
					<p class="mt-1 text-xs leading-5 text-[var(--color-text-tertiary)]">
						Issue search looks across the details you usually scan: title, description, status, project, assignees,
						labels, cycle, due date, team, and priority. Description matches include a short highlighted snippet.
					</p>
				</div>
			</aside>
		</div>
	</div>
</div>
