<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import ExternalLink from '@lucide/svelte/icons/external-link';
	import Breadcrumbs from '$lib/components/Breadcrumbs.svelte';
	import CtaSection from '$lib/components/CtaSection.svelte';
	import Footer from '$lib/components/Footer.svelte';
	import Nav from '$lib/components/Nav.svelte';
	import Seo from '$lib/components/Seo.svelte';
	import { reveal } from '$lib/actions/reveal';
	import { contentModifiedAt, url } from '$lib/config/site';
	import { competitorBySlug } from '$lib/data/alternative-pages';
	import { alternativePath, hostingLabel } from '$lib/data/competitor-types';
	import { faqLd, webPageLd, type Crumb } from '$lib/data/routes';

	const path = '/jira-data-center-end-of-life';
	const title = 'Jira Data Center End of Life (EOL): Dates and Self-Hosted Options | Kuayle';
	const description =
		'Jira Data Center reaches end of life on 28 March 2029. Key Atlassian dates, what happens to your instance, Data Center vs Cloud, and self-hosted Jira alternatives.';
	const modifiedAt = contentModifiedAt(path);
	const crumbs: Crumb[] = [
		{ label: 'Home', href: '/' },
		{ label: 'Alternatives', href: '/alternatives' },
		{ label: 'Jira Data Center end of life', href: path }
	];

	const EOL_URL = 'https://www.atlassian.com/licensing/data-center-end-of-life';
	const BLOG_URL = 'https://www.atlassian.com/blog/company-news/atlassian-ascend';

	const timeline = [
		{ date: '8 September 2025', label: 'Announcement', body: 'Atlassian announces the end of its Data Center products.' },
		{ date: '30 March 2026', label: 'New sales end', body: 'New customers can no longer buy Data Center subscriptions or new Marketplace Data Center apps.' },
		{ date: '30 March 2028', label: 'Last purchases', body: 'Last date for existing customers to buy new subscriptions, Marketplace apps, expansions and upgrades.' },
		{ date: '28 March 2029', label: 'End of life', body: 'Subscriptions expire. Products and apps become read-only, and updates, support and security fixes end. Renewals are prorated to this date.' }
	];

	const options = [
		{
			heading: 'Move to Jira Cloud',
			body: 'Atlassian’s supported path. Your data moves to Atlassian’s cloud; SSO and SCIM need Atlassian Guard below the Enterprise plan.'
		},
		{
			heading: 'Ask Atlassian for an extension',
			body: 'Atlassian says extended maintenance beyond 28 March 2029 is available by exception only. Plan for it as a bridge, not a destination.'
		},
		{
			heading: 'Switch to a self-hosted tracker',
			body: 'If the tracker has to stay on your own infrastructure, choose a product you can run yourself, such as Kuayle, OpenProject, YouTrack Server, GitLab or Plane.'
		}
	];

	const alternatives = ['openproject', 'youtrack', 'gitlab', 'plane', 'itsaplan'].map(competitorBySlug);

	const checklist = [
		'List the projects, workflows, custom fields and Marketplace apps you actually use today.',
		'Decide whether data residency or contracts require the tracker to stay on your infrastructure.',
		'Export a full CSV of work items, and an XML or database backup, before any migration.',
		'Pilot the target tool with one team for at least one cycle before moving everyone.',
		'Keep Data Center read-only for history until the end of life date, then archive the backup.'
	];

	const faq = [
		{
			question: 'When is Jira Data Center end of life?',
			answer: 'Jira Software and Jira Service Management Data Center reach end of life on 28 March 2029 at 23:59 PST, according to Atlassian’s Data Center end-of-life page.'
		},
		{
			question: 'Can I still buy Jira Data Center?',
			answer: 'Not as a new customer: new sales ended on 30 March 2026. Existing customers can buy new subscriptions, expansions and upgrades until 30 March 2028, and renew until the end of life date.'
		},
		{
			question: 'What happens to my Jira Data Center instance after end of life?',
			answer: 'Subscriptions expire and the products and apps become read-only. Atlassian stops updates, support and security fixes.'
		},
		{
			question: 'Jira Data Center vs Jira Cloud: what changes?',
			answer: 'Data Center runs on your own infrastructure; Jira Cloud runs on Atlassian’s. Moving to Cloud means your data leaves your network, and on plans below Enterprise, SSO and SCIM need a separate Atlassian Guard subscription.'
		},
		{
			question: 'What is a self-hosted alternative to Jira Data Center?',
			answer: 'Kuayle is a free, open source, self-hosted issue tracker under Apache 2.0. OpenProject, YouTrack Server, GitLab and Plane can also be self-hosted. Kuayle is narrower than Jira: it has no custom fields, Marketplace apps or service desk.'
		},
		{
			question: 'Does the end of life affect Bitbucket or Bamboo Data Center?',
			answer: 'Atlassian’s announcement excludes Bitbucket Data Center, Bamboo Data Center and Jira Align from this end of life.'
		}
	];

	const meta = {
		title,
		description,
		canonical: url(path),
		ogType: 'article' as const,
		modifiedAt,
		jsonLd: [webPageLd(title, description, url(path), crumbs, modifiedAt), faqLd(faq)]
	};
</script>

<Seo {meta} />
<Nav />

<main id="main" tabindex="-1" class="mx-auto max-w-4xl px-6 pt-28 pb-20 outline-none">
	<Breadcrumbs breadcrumbs={crumbs} />

	<article class="mt-8 min-w-0 [overflow-wrap:anywhere]">
		<header use:reveal>
			<p class="font-mono text-xs tracking-widest text-brand-300 uppercase">Guide · checked 6 November 2026</p>
			<h1 class="mt-3 text-4xl leading-tight font-bold tracking-tight sm:text-5xl">Jira Data Center end of life: dates and self-hosted options</h1>
			<p class="mt-5 text-lg leading-relaxed text-muted-foreground">
				Atlassian is ending its Data Center products. Jira Software and Jira Service Management Data Center become read-only
				on 28 March 2029. Here are the dates, what changes, and the options for teams that need to stay self-hosted.
			</p>
		</header>

		<section class="mt-14" aria-labelledby="timeline-heading">
			<h2 id="timeline-heading" class="text-2xl font-semibold tracking-tight sm:text-3xl">Timeline</h2>
			<ol class="mt-6 space-y-0 border-l border-dashed border-white/15">
				{#each timeline as item}
					<li class="relative pb-8 pl-6 last:pb-0">
						<span class="absolute top-1.5 -left-[5px] size-2.5 rounded-full bg-brand-400" aria-hidden="true"></span>
						<p class="font-mono text-sm text-brand-200"><time>{item.date}</time></p>
						<h3 class="mt-1 font-semibold">{item.label}</h3>
						<p class="mt-1 text-sm leading-relaxed text-muted-foreground">{item.body}</p>
					</li>
				{/each}
			</ol>
			<p class="mt-6 text-sm text-muted-foreground">
				Scope: Jira Software and Jira Service Management Data Center. Bitbucket, Bamboo and Jira Align Data Center are excluded.
				Source: <a href={EOL_URL} target="_blank" rel="noopener noreferrer" class="text-brand-300 underline-offset-4 hover:underline">Atlassian Data Center end of life</a>.
			</p>
		</section>

		<section class="mt-14" aria-labelledby="options-heading">
			<h2 id="options-heading" class="text-2xl font-semibold tracking-tight sm:text-3xl">Your options</h2>
			<div class="mt-6 grid gap-4 md:grid-cols-3">
				{#each options as option}
					<div class="rounded-xl border border-border bg-card/50 p-5">
						<h3 class="font-semibold">{option.heading}</h3>
						<p class="mt-2 text-sm leading-relaxed text-muted-foreground">{option.body}</p>
					</div>
				{/each}
			</div>
		</section>

		<section class="mt-14" aria-labelledby="kuayle-heading">
			<h2 id="kuayle-heading" class="text-2xl font-semibold tracking-tight sm:text-3xl">Staying self-hosted with Kuayle</h2>
			<p class="mt-4 leading-relaxed text-muted-foreground">
				Kuayle is an Apache 2.0 issue tracker you run with Docker Compose, with no licence fee and no paid tier. It covers
				issues with multiple assignees, cycles, projects with a Gantt view, private teams and GitHub automation. It does not
				replace everything in Jira: there are no custom fields, Marketplace apps, service desk or SAML SSO yet, and no built-in
				Jira importer.
			</p>
			<a href="/jira-alternative" class="mt-5 inline-flex items-center gap-1 text-sm font-medium text-brand-300">
				Compare Kuayle and Jira in detail <ArrowRight class="size-4" aria-hidden="true" />
			</a>
		</section>

		<section class="mt-14" aria-labelledby="others-heading">
			<h2 id="others-heading" class="text-2xl font-semibold tracking-tight sm:text-3xl">Other self-hosted Jira alternatives</h2>
			<ul class="mt-6 grid gap-3 sm:grid-cols-2">
				{#each alternatives as other}
					<li>
						<a href={alternativePath(other)} class="block rounded-xl border border-border bg-card/50 p-4 transition-colors hover:border-brand-400/40">
							<span class="font-medium">{other.displayName ?? other.name}</span>
							<span class="mt-1 block text-xs text-muted-foreground">{other.license} · {hostingLabel(other.hosting)}</span>
						</a>
					</li>
				{/each}
			</ul>
		</section>

		<section class="mt-14" aria-labelledby="checklist-heading">
			<h2 id="checklist-heading" class="text-2xl font-semibold tracking-tight sm:text-3xl">Migration checklist</h2>
			<ol class="mt-6 list-decimal space-y-2 pl-5 text-muted-foreground">
				{#each checklist as item}<li>{item}</li>{/each}
			</ol>
		</section>

		<section class="mt-14" aria-labelledby="faq-heading">
			<h2 id="faq-heading" class="text-2xl font-semibold tracking-tight sm:text-3xl">Jira Data Center EOL FAQ</h2>
			<dl class="mt-6 divide-y divide-border rounded-xl border border-border bg-card/40">
				{#each faq as item}
					<div class="p-5">
						<dt><h3 class="font-semibold">{item.question}</h3></dt>
						<dd class="mt-2 text-sm leading-relaxed text-muted-foreground">{item.answer}</dd>
					</div>
				{/each}
			</dl>
		</section>

		<section class="mt-14" aria-labelledby="sources-heading">
			<h2 id="sources-heading" class="text-xl font-semibold tracking-tight">Sources</h2>
			<ul class="mt-4 space-y-2 text-sm">
				<li><a href={EOL_URL} target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1.5 text-brand-300 underline-offset-4 hover:underline">Atlassian: Data Center end of life <ExternalLink class="size-3" aria-hidden="true" /></a></li>
				<li><a href={BLOG_URL} target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1.5 text-brand-300 underline-offset-4 hover:underline">Atlassian blog: announcement, 8 September 2025 <ExternalLink class="size-3" aria-hidden="true" /></a></li>
				<li><a href="https://www.atlassian.com/software/jira/pricing" target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1.5 text-brand-300 underline-offset-4 hover:underline">Jira pricing <ExternalLink class="size-3" aria-hidden="true" /></a></li>
			</ul>
			<p class="mt-6 rounded-lg border border-white/5 bg-white/[0.02] p-4 text-xs leading-relaxed text-muted-foreground">
				Dates checked against Atlassian’s own pages on 6 November 2026. Atlassian’s pages are authoritative if they change.
				Kuayle is developed by Bakney and is not affiliated with Atlassian. Jira is a trademark of Atlassian.
			</p>
		</section>

		<CtaSection heading="Keep your tracker on your own servers" description="Read the Apache 2.0 source, then follow the Docker Compose guide to run Kuayle with no licence fee." />
	</article>
</main>

<Footer />
