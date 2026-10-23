<script lang="ts">
	import { reveal } from '$lib/actions/reveal';
	import { Button } from '$lib/components/ui/button/index.js';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import FolderKanban from '@lucide/svelte/icons/folder-kanban';
	import History from '@lucide/svelte/icons/history';

	const tabs = [
		{
			id: 'cycles',
			icon: RefreshCw,
			label: 'Cycles',
			title: 'Every cycle works like your issue list',
			description:
				'Group, filter and bulk-edit cycle issues in list or board layouts. Add issues from the whole team with a searchable picker, or create one already assigned to the cycle.',
			href: '/features/cycles',
			image: 'tour-cycle',
			alt: 'Kuayle cycle detail page with progress, dates and the cycle issues grouped by status'
		},
		{
			id: 'projects',
			icon: FolderKanban,
			label: 'Projects',
			title: 'Projects across teams, with a Gantt view',
			description:
				'Track status, lead, dates and progress, then work the same issues in list, board or Gantt layouts. Remove an issue from a project without deleting it.',
			href: '/features/projects',
			image: 'tour-project',
			alt: 'Kuayle project detail page with project status, progress and issues from several teams'
		},
		{
			id: 'history',
			icon: History,
			label: 'Version history',
			title: 'See who changed what, and when',
			description:
				'Browse recorded title and description edits with the author, time and complete before and after values, rendered or as HTML source.',
			href: '/features/issue-tracking',
			image: 'tour-history',
			alt: 'Kuayle issue version history dialog comparing an earlier and a later issue description'
		}
	];

	let active = $state(0);
	const current = $derived(tabs[active]);

	function onKeydown(event: KeyboardEvent) {
		const last = tabs.length - 1;
		let next = active;
		if (event.key === 'ArrowRight') next = active === last ? 0 : active + 1;
		else if (event.key === 'ArrowLeft') next = active === 0 ? last : active - 1;
		else if (event.key === 'Home') next = 0;
		else if (event.key === 'End') next = last;
		else return;
		event.preventDefault();
		active = next;
		document.getElementById(`tour-tab-${tabs[next].id}`)?.focus();
	}
</script>

<section id="tour" class="relative py-24 sm:py-32">
	<div class="mx-auto max-w-6xl px-6">
		<div class="max-w-2xl" use:reveal>
			<p class="text-sm font-semibold tracking-widest text-brand-300 uppercase">A closer look</p>
			<h2 class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">Planning and history, from the current release</h2>
			<p class="mt-4 text-lg text-muted-foreground">
				Screenshots from a local instance with the repository’s seed data.
			</p>
		</div>

		<div use:reveal={{ delay: 100 }} class="mt-10">
			<div
				role="tablist"
				aria-label="Product screenshots"
				tabindex="-1"
				class="flex flex-wrap gap-2"
				onkeydown={onKeydown}
			>
				{#each tabs as tab, i (tab.id)}
					<Button
						variant="plain"
						type="button"
						role="tab"
						id="tour-tab-{tab.id}"
						aria-selected={active === i}
						aria-controls="tour-panel"
						tabindex={active === i ? 0 : -1}
						onclick={() => (active = i)}
						class="inline-flex items-center gap-2 rounded-full border px-4 py-2 text-sm font-medium transition-colors {active === i
							? 'border-brand-400/50 bg-brand-400/15 text-brand-100'
							: 'border-border bg-card/60 text-muted-foreground hover:border-brand-400/30 hover:text-foreground'}"
					>
						<tab.icon class="size-4 {active === i ? 'text-brand-300' : ''}" />
						{tab.label}
					</Button>
				{/each}
			</div>

			<div
				id="tour-panel"
				role="tabpanel"
				aria-labelledby="tour-tab-{current.id}"
				class="mt-6 grid items-start gap-8 lg:grid-cols-[minmax(0,1fr)_280px]"
			>
				<div class="relative min-w-0">
					<div
						class="animate-glow absolute -inset-4 -z-10 rounded-3xl bg-gradient-to-tr from-brand-600/30 via-brand-400/15 to-transparent blur-2xl"
					></div>
					<div
						class="ring-gradient overflow-hidden rounded-2xl border border-white/10 bg-card shadow-2xl shadow-black/60"
					>
						<div class="flex items-center gap-1.5 border-b border-white/5 bg-black/40 px-4 py-3">
							<span class="size-3 rounded-full bg-[#ff5f57]/80"></span>
							<span class="size-3 rounded-full bg-[#febc2e]/80"></span>
							<span class="size-3 rounded-full bg-[#28c840]/80"></span>
						</div>
						{#key current.id}
							<img
								src="/{current.image}-1440.webp"
								srcset="/{current.image}-720.webp 720w, /{current.image}-1440.webp 1440w, /{current.image}-2160.webp 2160w"
								sizes="(max-width: 1024px) 92vw, 840px"
								alt={current.alt}
								width="1440"
								height="900"
								loading="lazy"
								decoding="async"
								class="h-auto w-full"
							/>
						{/key}
					</div>
				</div>

				<div class="min-w-0">
					<h3 class="text-xl font-semibold">{current.title}</h3>
					<p class="mt-3 leading-relaxed text-muted-foreground">{current.description}</p>
					<a
						href={current.href}
						class="mt-5 inline-flex items-center gap-1.5 text-sm font-medium text-brand-300 transition-colors hover:text-brand-200"
					>
						Read about {current.label.toLowerCase()} <ArrowRight class="size-3.5" />
					</a>
				</div>
			</div>
		</div>
	</div>
</section>
