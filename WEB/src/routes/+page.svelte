<script lang="ts">
	import Nav from '$lib/components/Nav.svelte';
	import Hero from '$lib/components/Hero.svelte';
	import ProductTour from '$lib/components/ProductTour.svelte';
	import DevMachines from '$lib/components/DevMachines.svelte';
	import Pricing from '$lib/components/Pricing.svelte';
	import Deploy from '$lib/components/Deploy.svelte';
	import Footer from '$lib/components/Footer.svelte';
	import Seo from '$lib/components/Seo.svelte';
	import CtaSection from '$lib/components/CtaSection.svelte';
	import AlternativesStrip from '$lib/components/home/AlternativesStrip.svelte';
	import Chapter from '$lib/components/home/Chapter.svelte';
	import IssueMock from '$lib/components/home/IssueMock.svelte';
	import TriageMock from '$lib/components/home/TriageMock.svelte';
	import GithubMock from '$lib/components/home/GithubMock.svelte';
	import AccessMock from '$lib/components/home/AccessMock.svelte';
	import UseCases from '$lib/components/home/UseCases.svelte';
	import FeatureGrid from '$lib/components/home/FeatureGrid.svelte';
	import RecentReleases from '$lib/components/home/RecentReleases.svelte';
	import { url, SITE } from '$lib/config/site';
	import { ORGANIZATION_LD, WEBSITE_LD, SOFTWARE_APP_LD, webPageLd } from '$lib/data/routes';

	let { data } = $props();

	const title = 'Kuayle: Open Source, Self-Hosted Issue Tracker With No Paid Tier';
	const description = SITE.description;
	const canonical = url('/');

	const jsonLd = [ORGANIZATION_LD, WEBSITE_LD, SOFTWARE_APP_LD, webPageLd(title, description, canonical)];
</script>

<Seo meta={{ title, description, canonical, jsonLd }} />

<Nav />

<main id="main" tabindex="-1" class="outline-none">
	<Hero stars={data.stars} />
	<AlternativesStrip />

	<Chapter
		id="track"
		number="01"
		eyebrow="Track"
		title="Issues that fit how teams really work"
		body="Put several people on one issue, break it into sub-issues, and link what blocks what. Every title and description change is kept, so you can see who changed the scope and when."
		points={['Multiple assignees, sub-issues and relations', 'Team-specific statuses, labels, priorities and templates', 'Title and description version history']}
		link={{ label: 'Issue tracking', href: '/features/issue-tracking' }}
	>
		{#snippet visual()}<IssueMock />{/snippet}
	</Chapter>

	<ProductTour />

	<Chapter
		id="collaborate"
		number="03"
		eyebrow="Collaborate"
		title="Fast enough to keep up with the conversation"
		body="New work lands in a triage inbox to accept or decline. Threaded comments with @mentions, saved views and real-time sync keep everyone on the same page, and the keyboard does the rest."
		points={['Triage inbox and saved views', 'Threaded comments with mentions and resolve', 'C to create, ⌘K to search, S P A L for properties']}
		link={{ label: 'Keyboard shortcuts', href: '/features/keyboard-shortcuts' }}
		reverse
	>
		{#snippet visual()}<TriageMock />{/snippet}
	</Chapter>

	<Chapter
		id="integrate"
		number="04"
		eyebrow="Integrate"
		title="Let pull requests move the work"
		body="Install the GitHub App and branches, commits and pull requests attach to issues by their key. Choose which status an issue moves to when a branch is created, a PR opens or a PR merges."
		points={['GitHub App with status automation and PR comments', 'HMAC-signed webhooks for your own services', 'Scoped, expiring personal access tokens for scripts']}
		link={{ label: 'GitHub integration', href: '/features/github-integration' }}
	>
		{#snippet visual()}<GithubMock />{/snippet}
	</Chapter>

	<DevMachines />

	<Chapter
		id="control"
		number="06"
		eyebrow="Control"
		title="Private teams stay private"
		body="Make a team private and its issues, projects, cycles, comments and attachments are visible only to its members, including real-time updates. Owners and admins always keep access. Run several workspaces on one instance, each with its own members and roles."
		points={['Public and private teams; owner, admin, member and guest roles', 'Removing a member revokes access on the next request, tokens included', 'Trade-off: once a workspace uses private teams, saved views, public links, workspace export, webhooks and Dev Machines are turned off in it']}
		link={{ label: 'Teams and access control', href: '/features/teams-and-access-control' }}
		reverse
	>
		{#snippet visual()}<AccessMock />{/snippet}
	</Chapter>

	<Deploy />
	<UseCases />
	<Pricing />
	<FeatureGrid />
	<RecentReleases />

	<div class="mx-auto max-w-4xl px-6 pb-24">
		<CtaSection
			heading="Run your issue tracker on your own terms"
			description="Apache 2.0 · no per-seat fees · no paid tier. Read the source, then deploy with Docker Compose."
		/>
	</div>
</main>

<Footer />
