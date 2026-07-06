<script lang="ts">
	import type { Issue } from '$lib/types/issue';
	import type { PaginatedResponse } from '$lib/types/common';
	import { listIssues } from '$lib/api/issues';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import { LoaderCircle } from 'lucide-svelte';

	let {
		open = $bindable(false),
		slug,
		title = 'Select issue',
		description = 'Search for an issue.',
		actionLabel = 'Select',
		excludeIds = [],
		onselect
	}: {
		open: boolean;
		slug: string;
		title?: string;
		description?: string;
		actionLabel?: string;
		excludeIds?: string[];
		onselect: (issue: Issue) => void | Promise<void>;
	} = $props();

	interface HighlightSegment {
		text: string;
		match: boolean;
	}

	const ANIM_DURATION = 100;
	const hanRegex = /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/;
	let searchQuery = $state('');
	let searchResults = $state<Issue[]>([]);
	let searching = $state(false);
	let showingRecent = $state(false);
	let selectedIndex = $state(0);
	let visible = $state(false);
	let closing = false;
	let searchTimer: ReturnType<typeof setTimeout> | undefined;

	$effect(() => {
		if (open) {
			closing = false;
			visible = false;
			searchQuery = '';
			searchResults = [];
			searching = false;
			showingRecent = false;
			selectedIndex = 0;
			loadRecentIssues();
			requestAnimationFrame(() => {
				requestAnimationFrame(() => {
					visible = true;
					document.getElementById('issue-picker-search')?.focus();
				});
			});
		} else {
			visible = false;
			closing = false;
			clearTimeout(searchTimer);
		}
	});

	$effect(() => {
		if (!open) return;

		clearTimeout(searchTimer);
		const query = searchQuery;
		if (!canSearchIssues(query)) {
			if (!showingRecent) loadRecentIssues();
			selectedIndex = 0;
			return;
		}

		searching = true;
		showingRecent = false;
		selectedIndex = 0;
		searchTimer = setTimeout(async () => {
			try {
				const response: PaginatedResponse<Issue> = await listIssues(slug, { search: query, per_page: '12' });
				const excluded = new Set(excludeIds);
				searchResults = response.data.filter((issue) => !excluded.has(issue.id));
			} catch {
				searchResults = [];
			} finally {
				searching = false;
			}
		}, 250);

		return () => clearTimeout(searchTimer);
	});

	$effect(() => {
		if (searchResults.length === 0) {
			selectedIndex = 0;
		} else if (selectedIndex >= searchResults.length) {
			selectedIndex = searchResults.length - 1;
		}
	});

	function canSearchIssues(value: string) {
		const query = value.trim();
		return query.length >= 2 || hanRegex.test(query);
	}

	async function loadRecentIssues() {
		searching = true;
		showingRecent = true;
		try {
			const response: PaginatedResponse<Issue> = await listIssues(slug, { sort: 'updated_at', order: 'desc', per_page: '12' });
			const excluded = new Set(excludeIds);
			searchResults = response.data.filter((issue) => !excluded.has(issue.id));
		} catch {
			searchResults = [];
		} finally {
			searching = false;
		}
	}

	function close() {
		if (closing) return;
		closing = true;
		visible = false;
		clearTimeout(searchTimer);
		setTimeout(() => {
			open = false;
			closing = false;
		}, ANIM_DURATION);
	}

	async function selectIssue(issue: Issue) {
		await onselect(issue);
		close();
	}

	function activateIndex(index: number) {
		const issue = searchResults[index];
		if (issue) selectIssue(issue);
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			close();
		} else if (e.key === 'ArrowDown') {
			e.preventDefault();
			if (searchResults.length > 0) selectedIndex = Math.min(selectedIndex + 1, searchResults.length - 1);
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			if (searchResults.length > 0) selectedIndex = Math.max(selectedIndex - 1, 0);
		} else if (e.key === 'Enter') {
			e.preventDefault();
			activateIndex(selectedIndex);
		}
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
</script>

{#if open}
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="fixed inset-0 z-50 flex items-start justify-center px-3 pt-[6vh]" onkeydown={handleKeydown}>
		<button
			class="fixed inset-0 cursor-default"
			style="background: rgba(0,0,0,{visible ? 0.5 : 0}); transition: background {ANIM_DURATION}ms ease;"
			onclick={close}
			tabindex={-1}
			aria-label="Close"
		></button>

		<div
			class="relative z-10 w-full max-w-2xl overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] shadow-2xl"
			style="opacity: {visible ? 1 : 0}; transform: scale({visible ? 1 : 0.95}); transition: opacity {ANIM_DURATION}ms ease, transform {ANIM_DURATION}ms ease;"
		>
			<div class="min-w-0">
				<div class="sr-only">
					<h2>{title}</h2>
					<p>{description}</p>
				</div>
					<!-- svelte-ignore a11y_autofocus -->
					<input
						id="issue-picker-search"
						type="text"
						aria-label={title}
						bind:value={searchQuery}
						placeholder="Search issues..."
						autofocus
						class="w-full border-b border-[var(--app-border)] bg-transparent px-4 py-4 text-sm text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
					/>

					<div class="max-h-[68vh] min-h-[22rem] overflow-y-auto py-2">
						{#if canSearchIssues(searchQuery) || showingRecent || searchResults.length > 0}
							<div class="px-3 py-1">
								<span class="text-[10px] font-medium uppercase text-[var(--color-text-tertiary)]">{showingRecent && !canSearchIssues(searchQuery) ? 'Recent issues' : 'Issues'}</span>
							</div>
							{#if searching}
								<div class="flex items-center justify-center py-8">
									<LoaderCircle size={16} class="animate-spin text-[var(--color-text-tertiary)]" />
								</div>
							{:else if searchResults.length > 0}
								{#each searchResults as result, i (result.id)}
									{@const snippet = descriptionSnippet(result.description, searchQuery)}
									<button
										class="flex w-full items-start gap-2 px-4 py-2 text-left text-sm {i === selectedIndex ? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]' : 'text-[var(--color-text-secondary)]'}"
										onmouseenter={() => (selectedIndex = i)}
										onclick={() => selectIssue(result)}
									>
										<div class="mt-0.5 flex shrink-0 items-center gap-2">
											<IssuePriorityIcon priority={result.priority} size={14} />
											<IssueStatusIcon status={result.status} category={result.status_info?.category} color={result.status_info?.color} size={14} />
										</div>
										<div class="min-w-0 flex-1">
											<div class="flex min-w-0 items-center gap-2">
												<span class="shrink-0 text-xs text-[var(--color-text-tertiary)]">
													{#each highlightedSegments(result.identifier, searchQuery) as segment}
														<span class={highlightClass(segment.match)}>{segment.text}</span>
													{/each}
												</span>
												<span class="min-w-0 flex-1 truncate">
													{#each highlightedSegments(result.title, searchQuery) as segment}
														<span class={highlightClass(segment.match)}>{segment.text}</span>
													{/each}
												</span>
											</div>
											{#if snippet}
												<p class="mt-1 line-clamp-2 pr-8 text-xs leading-5 text-[var(--color-text-tertiary)]">
													{#each highlightedSegments(snippet, searchQuery) as segment}
														<span class={highlightClass(segment.match)}>{segment.text}</span>
													{/each}
												</p>
											{/if}
										</div>
										<span class="mt-0.5 hidden shrink-0 text-xs text-[var(--color-text-tertiary)] sm:inline">{actionLabel}</span>
									</button>
								{/each}
							{:else}
								<p class="px-4 py-2 text-sm text-[var(--color-text-tertiary)]">No issues found</p>
							{/if}
						{:else if searchQuery.trim()}
							<p class="px-4 py-2 text-sm text-[var(--color-text-tertiary)]">Keep typing to search issues</p>
						{:else}
							<p class="px-4 py-2 text-sm text-[var(--color-text-tertiary)]">Start typing to search issues</p>
						{/if}
					</div>
			</div>
		</div>
	</div>
{/if}
