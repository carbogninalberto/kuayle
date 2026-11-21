<script lang="ts">
	import { Bot, PanelLeftClose, Search, Server, Settings, SquarePen } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import type { Conversation } from '$lib/api/conversations';
	import ConversationNav from './ConversationNav.svelte';
	import type { TaskIndicator } from './shell-rules';
	import { m } from '$lib/features/i18n/messages';

	let {
		slug,
		conversationId,
		history,
		onchange,
		search = $bindable(''),
		folder = $bindable(null),
		hasMore,
		failed,
		onmore,
		loading = false,
		indicators,
		onrenamed,
		oncollapse,
		collapseLabel
	}: {
		slug: string;
		conversationId: string | null;
		history: Conversation[];
		onchange: (update: (items: Conversation[]) => Conversation[]) => void;
		search: string;
		folder: string | null;
		hasMore: boolean;
		failed: boolean;
		onmore: () => void;
		loading?: boolean;
		indicators: Record<string, TaskIndicator | undefined>;
		onrenamed: (conversation: Conversation) => void;
		oncollapse: () => void;
		collapseLabel: string;
	} = $props();
	const places = $derived([
		{ href: `/${slug}/settings/agents`, label: m['settings.nav.agents'](), icon: Bot },
		{ href: `/${slug}/machines`, label: m['machines.title'](), icon: Server }
	]);
</script>

<!-- relative: visually hidden (absolute) labels inside the list are clipped by
     this column instead of stretching the page. -->
<aside class="relative flex h-full min-h-0 w-full min-w-0 flex-col bg-muted/20" aria-label={m['assistant.sidebar']()}>
	<nav class="flex h-12 shrink-0 items-center gap-1 px-2" aria-label={m['assistant.places']()}>
		<Button
			variant="ghost"
			size="icon-sm"
			href="/{slug}/assistant"
			aria-label={m['assistant.new']()}
			title={m['assistant.new']()}><SquarePen /></Button
		>
		{#each places as place (place.href)}
			<Button variant="ghost" size="icon-sm" href={place.href} aria-label={place.label} title={place.label}
				><place.icon /></Button
			>
		{/each}
		<Button
			variant="ghost"
			size="icon-sm"
			class="ml-auto"
			href="/{slug}/settings/assistant"
			aria-label={m['settings.nav.assistant']()}
			title={m['settings.nav.assistant']()}><Settings /></Button
		>
		<Button variant="ghost" size="icon-sm" aria-label={collapseLabel} title={collapseLabel} onclick={oncollapse}
			><PanelLeftClose /></Button
		>
	</nav>
	<div class="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
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
		<ConversationNav
			{slug}
			{conversationId}
			{history}
			{onchange}
			bind:folder
			{hasMore}
			{failed}
			{onmore}
			{loading}
			{indicators}
			{onrenamed}
		/>
	</div>
</aside>
