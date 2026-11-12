<script lang="ts">
	import { BotMessageSquare, ChevronRight, Circle, CircleCheck, NotebookText } from '@lucide/svelte';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import type { NotebookFinding, TaskNotebook } from '$lib/api/agent-tasks';
	import ClampedText from './ClampedText.svelte';
	import Markdown from './Markdown.svelte';
	import { notebookDocument, type NotebookHistoryEntry } from './task-notebook';
	import { getLocale, m } from '$lib/features/i18n/messages';

	// The task's notebook as a short working doc, one quiet line above
	// the timeline until it is opened: the plan as a checklist, what is done
	// and what remains, how to verify, decisions and findings. What people
	// asked is set apart from what agents wrote; who wrote what, at which
	// step, is the history at the end. Everything in it is untrusted text and
	// only ever escaped (the plan's text goes through the sanitizing Markdown
	// view, as it does in its step).
	let {
		view,
		request,
		name = () => '',
		open = $bindable(false),
		onopened
	}: {
		view: TaskNotebook;
		// The task's request, which the view shows above: it is not repeated.
		request: string;
		// A person's name by id; empty when not known.
		name?: (id: string | undefined) => string;
		open?: boolean;
		// The document was opened and is laid out: the view may place it.
		onopened?: () => void;
	} = $props();

	let section = $state<HTMLElement | null>(null);
	// Told once the opened document is in the page, and once more if it only
	// reaches its height a moment later; what the reader opens inside it
	// afterwards is not an opening.
	$effect(() => {
		const node = section;
		if (!open || !node || !onopened) return;
		const opened = onopened;
		let height = node.offsetHeight;
		opened();
		const observer = new ResizeObserver(() => {
			if (node.offsetHeight > height) opened();
			height = node.offsetHeight;
		});
		observer.observe(node);
		const settled = setTimeout(() => observer.disconnect(), 400);
		return () => {
			clearTimeout(settled);
			observer.disconnect();
		};
	});

	const doc = $derived(notebookDocument(view, request));
	const when = (at: string) => new Date(at).toLocaleString(getLocale(), { dateStyle: 'medium', timeStyle: 'short' });
	const sources: Record<NotebookFinding['source'], () => string> = {
		review: m['notebook.source_review'],
		ci: m['notebook.source_ci'],
		check: m['workflows.type.check'],
		comment: m['agent_messages.channel_comment_short'],
		agent: m['tasks.live_from_agent']
	};
	const sections: Record<string, () => string> = {
		plan: m['tasks.plan'],
		decisions: m['notebook.decisions'],
		done: m['notebook.done'],
		remaining: m['notebook.remaining'],
		verify: m['notebook.verify'],
		findings: m['notebook.findings'],
		instructions: m['notebook.people']
	};
	function author(entry: Extract<NotebookHistoryEntry, { kind: 'change' }>) {
		if (entry.author === 'agent') return m['tasks.live_from_agent']();
		if (entry.author === 'kuayle') return 'Kuayle';
		return name(entry.userId) || m['notebook.author_person']();
	}
	const changed = (entry: Extract<NotebookHistoryEntry, { kind: 'change' }>) =>
		Object.keys(sections)
			.filter((section) => entry.sections.includes(section))
			.map((section) => sections[section]())
			.join(', ');
	const heading = 'text-xs font-medium text-muted-foreground';
</script>

{#snippet chevron()}
	<ChevronRight
		size={14}
		aria-hidden="true"
		class="shrink-0 text-muted-foreground/60 transition-transform group-aria-expanded:rotate-90 motion-reduce:transition-none"
	/>
{/snippet}

{#snippet mark(done: boolean)}
	<span
		role="img"
		aria-label={done ? m['workflows.task.status_done']() : m['notebook.to_do']()}
		class="mt-0.5 inline-flex shrink-0 {done ? 'text-[var(--status-success)]' : 'text-muted-foreground/60'}"
		>{#if done}<CircleCheck size={15} />{:else}<Circle size={15} />{/if}</span
	>
{/snippet}

<section bind:this={section} aria-label={m['notebook.title']()} class="min-w-0" data-testid="task-notebook">
	<Collapsible.Root bind:open>
		<Collapsible.Trigger
			class="group flex w-full min-w-0 cursor-pointer items-center gap-2.5 rounded-md py-1.5 text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
		>
			<span class="inline-flex size-5 shrink-0 items-center justify-center text-muted-foreground"
				><NotebookText size={14} aria-hidden="true" /></span
			>
			<span class="min-w-0 flex-1 truncate text-sm font-medium">{m['notebook.title']()}</span>
			{#if doc.progress}<span class="shrink-0 text-xs text-muted-foreground tabular-nums"
					>{m['notebook.progress']({ done: doc.progress.done, total: doc.progress.total })}</span
				>{/if}
			{@render chevron()}
		</Collapsible.Trigger>
		<Collapsible.Content>
			{#if open}<div class="ml-2.5 flex min-w-0 flex-col gap-5 border-l py-3 pl-4 text-sm sm:pl-5">
					{#if doc.empty}<p class="text-muted-foreground">{m['notebook.empty']()}</p>{/if}

					{#if doc.steps.length || doc.planText}<section aria-labelledby="notebook-plan" class="min-w-0">
							<h3 id="notebook-plan" class={heading}>{m['tasks.plan']()}</h3>
							{#if doc.steps.length}<ol class="mt-2 flex min-w-0 flex-col gap-1.5">
									{#each doc.steps as step, index (index)}
										{@const detailed = !!(step.acceptance || step.files?.length || step.commands?.length)}
										<li class="flex min-w-0 items-start gap-2" data-done={step.done ? 'true' : 'false'}>
											{@render mark(!!step.done)}
											{#if detailed}<Collapsible.Root class="min-w-0 flex-1">
													<Collapsible.Trigger
														class="group flex min-w-0 max-w-full cursor-pointer items-start gap-1.5 rounded-sm text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50 {step.done
															? 'text-muted-foreground'
															: ''}"
														><span class="min-w-0 break-words">{step.title}</span><span class="mt-1 inline-flex"
															>{@render chevron()}</span
														></Collapsible.Trigger
													>
													<Collapsible.Content>
														<div class="flex min-w-0 flex-col gap-1 pt-1 pb-1.5 text-xs text-muted-foreground">
															{#if step.acceptance}<p class="break-words">{step.acceptance}</p>{/if}
															{#each step.commands ?? [] as command, i (i)}<code
																	class="block min-w-0 rounded bg-muted px-1.5 py-0.5 font-mono break-all">{command}</code
																>{/each}
															{#if step.files?.length}<p class="font-mono break-all">{step.files.join(' · ')}</p>{/if}
														</div>
													</Collapsible.Content>
												</Collapsible.Root>{:else}<span
													class="min-w-0 break-words {step.done ? 'text-muted-foreground' : ''}">{step.title}</span
												>{/if}
										</li>
									{/each}
								</ol>{:else}<div class="mt-2 min-w-0"><Markdown source={doc.planText} /></div>{/if}
						</section>{/if}

					{#if doc.done.length}<section aria-labelledby="notebook-done" class="min-w-0">
							<h3 id="notebook-done" class={heading}>{m['notebook.done']()}</h3>
							<ul class="mt-2 flex min-w-0 flex-col gap-1.5">
								{#each doc.done as entry, index (index)}<li class="flex min-w-0 items-start gap-2">
										{@render mark(true)}
										<span class="min-w-0 break-words"
											>{entry.text}{#if entry.recorded}<span class="ml-1.5 text-xs text-muted-foreground"
													>{m['notebook.recorded']()}</span
												>{/if}</span
										>
									</li>{/each}
							</ul>
						</section>{/if}

					{#if doc.remaining.length}<section aria-labelledby="notebook-remaining" class="min-w-0">
							<h3 id="notebook-remaining" class={heading}>{m['notebook.remaining']()}</h3>
							<ul class="mt-2 flex min-w-0 flex-col gap-1.5">
								{#each doc.remaining as entry, index (index)}<li class="flex min-w-0 items-start gap-2">
										{@render mark(false)}<span class="min-w-0 break-words">{entry}</span>
									</li>{/each}
							</ul>
						</section>{/if}

					{#if doc.verify.length}<section aria-labelledby="notebook-verify" class="min-w-0">
							<h3 id="notebook-verify" class={heading}>{m['notebook.verify']()}</h3>
							<ul class="mt-2 flex min-w-0 list-disc flex-col gap-1 pl-5 marker:text-muted-foreground/60">
								{#each doc.verify as entry, index (index)}<li class="min-w-0 break-words">{entry}</li>{/each}
							</ul>
						</section>{/if}

					{#if doc.decisions.length}<section aria-labelledby="notebook-decisions" class="min-w-0">
							<h3 id="notebook-decisions" class={heading}>{m['notebook.decisions']()}</h3>
							<ul class="mt-2 flex min-w-0 list-disc flex-col gap-1.5 pl-5 marker:text-muted-foreground/60">
								{#each doc.decisions as entry, index (index)}<li class="min-w-0 break-words">
										{entry.decision}{#if entry.why}<span class="block text-xs text-muted-foreground">{entry.why}</span
											>{/if}
									</li>{/each}
							</ul>
						</section>{/if}

					{#if doc.findings.length}<section aria-labelledby="notebook-findings" class="min-w-0">
							<h3 id="notebook-findings" class={heading}>{m['notebook.findings']()}</h3>
							<ul class="mt-2 flex min-w-0 flex-col gap-1.5">
								{#each doc.findings as finding (finding.id)}<li
										class="flex min-w-0 items-start gap-2 {finding.resolved ? 'text-muted-foreground' : ''}"
										data-resolved={finding.resolved ? 'true' : 'false'}
									>
										<Badge variant="outline" class="mt-px h-5 shrink-0 px-1.5 text-[11px] font-normal text-muted-foreground"
											>{sources[finding.source]?.() ?? finding.source}</Badge
										>
										<span class="min-w-0 flex-1 break-words">{finding.text}</span>
										{#if finding.resolved}<span
												role="img"
												aria-label={m['issue.resolved']()}
												title={m['issue.resolved']()}
												class="mt-0.5 inline-flex shrink-0 text-[var(--status-success)]"><CircleCheck size={15} /></span
											>{/if}
									</li>{/each}
							</ul>
						</section>{/if}

					{#if doc.people.length}<section
							aria-labelledby="notebook-people"
							class="min-w-0 rounded-lg border bg-muted/40 px-3 py-2.5"
							data-testid="notebook-people"
						>
							<h3 id="notebook-people" class={heading}>{m['notebook.people']()}</h3>
							<ul class="mt-2 flex min-w-0 flex-col gap-2.5">
								{#each doc.people as entry, index (index)}
									<!-- What the assistant wrote and the person approved is not
									     the person's own words: it is signed by the assistant. -->
									<li
										class="min-w-0 {entry.assistant ? 'border-l-2 border-dashed pl-3 text-muted-foreground' : ''}"
										data-author={entry.assistant ? 'assistant' : 'person'}
									>
										<ClampedText text={entry.text} />
										<p class="mt-0.5 flex items-center gap-1 text-xs text-muted-foreground">
											{#if entry.assistant}<BotMessageSquare size={12} aria-hidden="true" class="shrink-0" />{/if}
											{[entry.assistant ? m['assistant.title']() : name(entry.author_id), when(entry.at)]
												.filter(Boolean)
												.join(' · ')}
										</p>
									</li>
								{/each}
							</ul>
						</section>{/if}

					{#if doc.history.length}<Collapsible.Root>
							<Collapsible.Trigger
								class="group flex cursor-pointer items-center gap-1.5 rounded-sm text-xs text-muted-foreground outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
								>{m['notebook.history']()}{@render chevron()}</Collapsible.Trigger
							>
							<Collapsible.Content>
								<ol class="mt-2 flex min-w-0 flex-col gap-1.5 text-xs" aria-label={m['notebook.history']()}>
									{#each doc.history as entry (entry.key)}<li class="flex min-w-0 items-baseline gap-3">
											<span class="min-w-0 flex-1 break-words">
												{#if entry.kind === 'event'}<span class="text-muted-foreground">{entry.text}</span
													>{:else}{[author(entry), entry.step].filter(Boolean).join(' · ')}{#if changed(entry)}<span
															class="text-muted-foreground">{` — ${changed(entry)}`}</span
														>{/if}{/if}
											</span>
											<time datetime={entry.at} class="shrink-0 text-muted-foreground tabular-nums">{when(entry.at)}</time>
										</li>{/each}
								</ol>
							</Collapsible.Content>
						</Collapsible.Root>{/if}
				</div>{/if}
		</Collapsible.Content>
	</Collapsible.Root>
</section>
