<script lang="ts">
	import { Input } from '$lib/components/ui/input/index.js';
	import { untrack } from 'svelte';
	import type { Issue } from '$lib/types/issue';
	import { listIssues } from '$lib/api/issues';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import { m } from '$lib/paraglide/messages.js';

	let {
		open = $bindable(false),
		slug,
		title = m['issuePicker.title'](),
		description = m['issuePicker.description'](),
		actionLabel = m['issuePicker.select'](),
		excludeIds = [],
		params = {},
		filterIssue,
		multiple = false,
		onselect,
		onsubmit
	}: {
		open: boolean;
		slug: string;
		title?: string;
		description?: string;
		actionLabel?: string;
		excludeIds?: string[];
		params?: Record<string, string>;
		filterIssue?: (issue: Issue) => boolean;
		multiple?: boolean;
		onselect?: (issue: Issue) => void | Promise<void>;
		onsubmit?: (issues: Issue[]) => Promise<Issue[] | void>;
	} = $props();

	interface HighlightSegment {
		text: string;
		match: boolean;
	}
	const uid = $props.id();
	let searchQuery = $state('');
	let searchResults = $state<Issue[]>([]);
	let searching = $state(false);
	let selectedIndex = $state(0);
	let selected = $state<Issue[]>([]);
	let hasMore = $state(false);
	let currentPage = 0;
	let requestVersion = 0;
	let submitting = $state(false);
	let error = $state('');
	let loadFailed = $state(false);

	$effect(() => {
		if (open)
			untrack(() => {
				searchQuery = '';
				selected = [];
				error = '';
			});
	});

	$effect(() => {
		const isOpen = open;
		const query = searchQuery.trim();
		const workspace = slug;
		const scopeParams = { ...params };
		const version = ++requestVersion;
		if (!isOpen) return;
		searchResults = [];
		selectedIndex = 0;
		hasMore = false;
		currentPage = 0;
		searching = true;
		const timer = setTimeout(() => void loadPage(1, version, workspace, query, scopeParams), query ? 250 : 0);
		return () => {
			clearTimeout(timer);
			requestVersion++;
		};
	});

	async function loadPage(
		pageNumber: number,
		version = requestVersion,
		workspace = slug,
		query = searchQuery.trim(),
		scopeParams = params
	) {
		searching = true;
		error = '';
		loadFailed = false;
		try {
			const response = await listIssues(workspace, {
				...scopeParams,
				search: query,
				sort: 'updated_at',
				order: 'desc',
				per_page: '20',
				page: String(pageNumber)
			});
			if (!open || version !== requestVersion) return;
			const excluded = new Set(excludeIds);
			const results = response.data.filter((issue) => !excluded.has(issue.id) && (!filterIssue || filterIssue(issue)));
			searchResults = [
				...new Map([...(pageNumber === 1 ? [] : searchResults), ...results].map((issue) => [issue.id, issue])).values()
			];
			hasMore = response.has_more;
			currentPage = pageNumber;
		} catch {
			if (version === requestVersion) {
				error = m['issuePicker.failed_load']();
				loadFailed = true;
			}
		} finally {
			if (version === requestVersion) searching = false;
		}
	}

	async function selectIssue(issue: Issue) {
		if (submitting) return;
		if (multiple) {
			selected = selected.some((item) => item.id === issue.id)
				? selected.filter((item) => item.id !== issue.id)
				: [...selected, issue];
			return;
		}
		submitting = true;
		try {
			await onselect?.(issue);
			open = false;
		} catch {
			error = m['issuePicker.failed_save']();
		} finally {
			submitting = false;
		}
	}

	async function submit() {
		if (!onsubmit || submitting || !selected.length) return;
		submitting = true;
		error = '';
		try {
			const failed = await onsubmit(selected);
			if (!failed?.length) open = false;
			else {
				const successfulIds = new Set(
					selected.filter((issue) => !failed.some((item) => item.id === issue.id)).map((issue) => issue.id)
				);
				searchResults = searchResults.filter((issue) => !successfulIds.has(issue.id));
				selected = failed;
				error = m['issuePicker.partial_failure']({ count: failed.length });
			}
		} catch {
			error = m['issuePicker.failed_save']();
		} finally {
			submitting = false;
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
			e.preventDefault();
			selectedIndex = Math.max(0, Math.min(searchResults.length - 1, selectedIndex + (e.key === 'ArrowDown' ? 1 : -1)));
			document.getElementById(`${uid}-${selectedIndex}`)?.scrollIntoView({ block: 'nearest' });
		} else if (e.key === 'Enter') {
			e.preventDefault();
			const issue = searchResults[selectedIndex];
			if (issue) void selectIssue(issue);
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

<Dialog.Root bind:open>
	<Dialog.Content
		class="sm:max-w-2xl"
		showCloseButton={!submitting}
		onEscapeKeydown={(event) => {
			if (submitting) event.preventDefault();
		}}
		onInteractOutside={(event) => {
			if (submitting) event.preventDefault();
		}}
	>
		<Dialog.Header>
			<Dialog.Title>{title}</Dialog.Title>
			<Dialog.Description>{description}</Dialog.Description>
		</Dialog.Header>
		<Input
			type="search"
			aria-label={m['issuePicker.search']()}
			placeholder={m['issuePicker.search']()}
			bind:value={searchQuery}
			disabled={submitting}
			role="combobox"
			aria-expanded="true"
			aria-controls={`${uid}-results`}
			aria-activedescendant={searchResults.length ? `${uid}-${selectedIndex}` : undefined}
			aria-autocomplete="list"
			onkeydown={handleKeydown}
			class="w-full bg-transparent"
		/>
		<div
			id={`${uid}-results`}
			role="listbox"
			aria-label={title}
			aria-multiselectable={multiple}
			aria-busy={searching}
			class="max-h-[50vh] overflow-y-auto"
		>
			{#each searchResults as result, i (result.id)}
				{@const snippet = descriptionSnippet(result.description, searchQuery)}
				<Button variant="ghost" size="sm"
					id={`${uid}-${i}`}
					type="button"
					role="option"
					aria-selected={multiple ? selected.some((issue) => issue.id === result.id) : i === selectedIndex}
					disabled={submitting}
					class="h-auto justify-start whitespace-normal flex w-full items-start gap-2 rounded-md px-3 py-2 text-left text-sm hover:bg-[var(--color-bg-hover)] {i ===
					selectedIndex
						? 'bg-[var(--color-bg-hover)]'
						: ''}"
					onmouseenter={() => (selectedIndex = i)}
					onclick={() => selectIssue(result)}
				>
					{#if multiple}<span aria-hidden="true">{selected.some((issue) => issue.id === result.id) ? '☑' : '☐'}</span
						>{/if}
					<IssuePriorityIcon priority={result.priority} size={14} />
					<IssueStatusIcon
						status={result.status}
						category={result.status_info?.category}
						color={result.status_info?.color}
						size={14}
					/>
					<span class="min-w-0 flex-1">
						<span class="mr-2 text-xs text-[var(--color-text-tertiary)]">{result.identifier}</span>
						{#each highlightedSegments(result.title, searchQuery) as segment}<span class={highlightClass(segment.match)}
								>{segment.text}</span
							>{/each}
						{#if snippet}<span class="mt-1 block line-clamp-2 text-xs text-[var(--color-text-tertiary)]">{snippet}</span
							>{/if}
					</span>
					{#if !multiple}<span class="hidden text-xs sm:inline">{actionLabel}</span>{/if}
				</Button>
			{/each}
		</div>
		{#if searching}<p role="status" class="text-sm text-[var(--color-text-tertiary)]">{m['common.loading']()}</p>
		{:else if !searchResults.length}<p class="text-sm text-[var(--color-text-tertiary)]">
				{m['issuePicker.empty']()}
			</p>{/if}
		{#if error}<p role="alert" class="text-sm text-[var(--color-error)]">{error}</p>{/if}
		{#if hasMore || loadFailed}<Button
				variant="outline"
				disabled={searching || submitting}
				onclick={() => loadPage(currentPage + 1)}
				>{loadFailed ? m['scopedIssues.retry']() : m['issuePicker.load_more']()}</Button
			>{/if}
		<Dialog.Footer>
			<Button variant="outline" disabled={submitting} onclick={() => (open = false)}>{m['common.cancel']()}</Button>
			{#if multiple}<Button disabled={!selected.length || submitting} onclick={submit}
					>{actionLabel} ({selected.length})</Button
				>{/if}
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
