<script lang="ts">
	import { Bot, BotMessageSquare, PanelLeftClose, Search, Server, Settings, SquarePen } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Select from '$lib/components/ui/select/index.js';
	import type { Conversation } from '$lib/api/conversations';
	import type { Project } from '$lib/types/project';
	import ConversationNav from './ConversationNav.svelte';
	import type { ProjectFilter } from './project-filter';
	import type { TaskIndicator } from './shell-rules';
	import { m } from '$lib/features/i18n/messages';

	// The assistant's own left column: new chat and the other assistant places,
	// conversation search and project filter, the collapsible Recents list with
	// folders. It scrolls on its own; the page never does.
	let {
		slug,
		conversationId,
		history = $bindable([]),
		projects,
		projectFilter,
		projectOptions,
		loading = false,
		indicators,
		onproject,
		onrenamed,
		oncollapse,
		collapseLabel
	}: {
		slug: string;
		conversationId: string | null;
		history: Conversation[];
		projects: Project[];
		projectFilter: ProjectFilter;
		projectOptions: { value: string; label: string }[];
		loading?: boolean;
		indicators: Record<string, TaskIndicator | undefined>;
		onproject: (value: string) => void;
		onrenamed: (conversation: Conversation) => void;
		oncollapse: () => void;
		collapseLabel: string;
	} = $props();
	let search = $state('');
	const places = $derived([
		{ href: `/${slug}/settings/assistant`, label: m['assistant.place_skills'](), icon: BotMessageSquare },
		{ href: `/${slug}/settings/agents`, label: m['settings.nav.agents'](), icon: Bot },
		{ href: `/${slug}/machines`, label: m['machines.title'](), icon: Server }
	]);
</script>

<!-- relative: visually hidden (absolute) labels inside the list are clipped by
     this column instead of stretching the page. -->
<aside class="relative flex h-full min-h-0 w-full min-w-0 flex-col bg-muted/20" aria-label={m['assistant.sidebar']()}>
	<div class="flex h-12 shrink-0 items-center gap-1 px-3">
		<BotMessageSquare size={16} class="shrink-0 text-muted-foreground" aria-hidden="true" />
		<p class="min-w-0 flex-1 truncate text-sm font-semibold">{m['assistant.title']()}</p>
		<Button
			variant="ghost"
			size="icon-sm"
			href="/{slug}/assistant"
			aria-label={m['assistant.new']()}
			title={m['assistant.new']()}><SquarePen /></Button
		>
		<Button
			variant="ghost"
			size="icon-sm"
			href="/{slug}/settings/assistant"
			aria-label={m['settings.nav.assistant']()}
			title={m['settings.nav.assistant']()}><Settings /></Button
		>
		<Button variant="ghost" size="icon-sm" aria-label={collapseLabel} title={collapseLabel} onclick={oncollapse}
			><PanelLeftClose /></Button
		>
	</div>
	<div class="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
		<nav class="flex items-center gap-0.5 pb-2" aria-label={m['assistant.places']()}>
			{#each places as place (place.href)}
				<Button variant="ghost" size="icon-sm" href={place.href} aria-label={place.label} title={place.label}
					><place.icon /></Button
				>
			{/each}
		</nav>
		<div class="relative mb-2">
			<Search
				size={14}
				class="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-muted-foreground"
				aria-hidden="true"
			/>
			<Input
				bind:value={search}
				type="search"
				aria-label={m['assistant.search']()}
				placeholder={m['assistant.search']()}
				class="h-8 pl-8 text-sm"
				onkeydown={(event) => {
					if (event.key === 'Escape' && search) {
						event.preventDefault();
						search = '';
					}
				}}
			/>
		</div>
		{#if projects.length > 0 || projectFilter !== 'all'}
			<div class="mb-2">
				<Select.Root type="single" value={projectFilter} onValueChange={onproject} items={projectOptions}>
					<Select.Trigger class="h-8 w-full" aria-label={m['assistant.project_filter']()}>
						<span class="truncate">{projectOptions.find((o) => o.value === projectFilter)?.label ?? ''}</span>
					</Select.Trigger>
					<Select.Content>
						{#each projectOptions as option (option.value)}
							<Select.Item value={option.value} label={option.label}>{option.label}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>
		{/if}
		<ConversationNav
			{slug}
			{conversationId}
			bind:history
			{search}
			{projects}
			{projectFilter}
			{loading}
			{indicators}
			{onrenamed}
		/>
	</div>
</aside>
