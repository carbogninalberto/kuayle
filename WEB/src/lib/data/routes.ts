/**
 * Route and content registry for the Kuayle marketing site.
 * All routes are statically prerenderable.
 *
 * This file serves as the single source of truth for:
 *  - which routes exist (used for sitemap generation and SEO validation)
 *  - content for shared dynamic-route pages (features, self-hosting, compare, alternatives)
 *  - breadcrumb data
 *  - hub/silo relationships
 */

import { url, type PageMeta } from '$lib/config/site';
import { FALLBACK_VERSION } from '$lib/release.svelte';

// Re-export url for convenience
export { url };

// ---- Types ----

export interface ContentPage {
	slug: string;
	title: string;
	description: string;
	heading: string;
	intro: string;
	sections: ContentSection[];
	/** Optional extra markup slots rendered after sections */
	footnotes?: string;
}

export interface ContentSection {
	heading: string;
	body: string;
	list?: string[];
	links?: { label: string; href: string }[];
}

export interface ContentRegistry {
	/** Route path within parent (e.g. 'issue-tracking') */
	[key: string]: ContentPage;
}

// ---- Breadcrumb utilities ----

export interface Crumb {
	label: string;
	href: string;
}

/**
 * Build a breadcrumb trail for any page.
 * `parentSlug` is the hub root (e.g. 'features' or 'self-hosting').
 * `parentLabel` is the display label for that hub.
 */
export function breadcrumbsFrom(parentSlug: string, parentLabel: string, pageTitle?: string, pageSlug?: string): Crumb[] {
	const crumbs: Crumb[] = [
		{ label: 'Home', href: '/' }
	];
	if (parentSlug) {
		crumbs.push({ label: parentLabel, href: `/${parentSlug}` });
	}
	if (pageTitle && pageSlug) {
		crumbs.push({ label: pageTitle, href: `/${parentSlug}/${pageSlug}` });
	}
	return crumbs;
}

// ---- Hub definitions ----

export interface Hub {
	slug: string;
	label: string;
	href: string;
	children: { slug: string; label: string; href: string; description?: string }[];
}

export const HUBS: Record<string, Hub> = {
	features: {
		slug: 'features',
		label: 'Features',
		href: '/features',
		children: [
			{ slug: 'issue-tracking', label: 'Issue Tracking', href: '/features/issue-tracking', description: 'Multiple assignees, sub-issues, relations, comments, labels and history.' },
			{ slug: 'cycles', label: 'Cycles', href: '/features/cycles', description: 'Date-bound work with burndown, velocity and optional carry-over.' },
			{ slug: 'projects', label: 'Projects', href: '/features/projects', description: 'Cross-team issue grouping, progress and a due-date Gantt view.' },
			{ slug: 'github-integration', label: 'GitHub Integration', href: '/features/github-integration', description: 'Match issue IDs in branches, commits and pull requests; configure status changes.' },
			{ slug: 'real-time-sync', label: 'Real-Time Sync', href: '/features/real-time-sync', description: 'Workspace WebSocket events for issues, comments, cycles, views and GitHub activity.' },
			{ slug: 'keyboard-shortcuts', label: 'Keyboard Shortcuts', href: '/features/keyboard-shortcuts', description: 'Global navigation, issue search, property controls and triage shortcuts.' },
			{ slug: 'views-and-triage', label: 'Views & Triage', href: '/features/views-and-triage', description: 'Saved filters, triage inbox, hierarchical labels, and custom workflows.' },
			{ slug: 'teams-and-access-control', label: 'Teams & Access Control', href: '/features/teams-and-access-control', description: 'Multiple teams per workspace with role-based access control.' },
			{ slug: 'analytics-insights', label: 'Analytics & Insights', href: '/features/analytics-insights', description: 'Workspace and team overviews, burn-up trends, and configurable issue insights.' },
			{ slug: 'dev-machines', label: 'Dev Machines', href: '/features/dev-machines', description: 'Opt-in multi-container coding environments with agent providers and issue worktrees.' }
		]
	},
	selfHosting: {
		slug: 'self-hosting',
		label: 'Self-Hosting',
		href: '/self-hosting',
		children: [
			{ slug: 'docker-compose', label: 'Docker Compose', href: '/self-hosting/docker-compose', description: 'Reference stack with Caddy, PostgreSQL, Redis, API and frontend.' },
			{ slug: 'requirements', label: 'Requirements', href: '/self-hosting/requirements', description: 'Server prerequisites and deployment dependencies.' },
			{ slug: 'configuration', label: 'Configuration', href: '/self-hosting/configuration', description: 'Environment variables, secrets, and deployment settings.' },
			{ slug: 'updating', label: 'Updating', href: '/self-hosting/updating', description: 'Rebuild containers and apply migrations with the supplied update script.' },
			{ slug: 'storage', label: 'Storage', href: '/self-hosting/storage', description: 'Local filesystem and S3-compatible storage backends.' },
			{ slug: 'github-app', label: 'GitHub App', href: '/self-hosting/github-app', description: 'Set up and configure the GitHub integration.' },
			{ slug: 'dev-machines', label: 'Dev Machines', href: '/self-hosting/dev-machines', description: 'Enable the opt-in agentic coding subsystem with wildcard TLS.' }
		]
	},
	compare: {
		slug: 'compare',
		label: 'Compare',
		href: '/compare',
		children: [
			{ slug: 'kuayle-vs-linear', label: 'Kuayle vs Linear', href: '/compare/kuayle-vs-linear', description: 'Self-hosted source access versus a mature hosted service.' },
			{ slug: 'kuayle-vs-plane', label: 'Kuayle vs Plane', href: '/compare/kuayle-vs-plane', description: 'A focused ungated tracker versus a broader suite with commercial plans.' }
		]
	},
	alternatives: {
		slug: 'alternatives',
		label: 'Alternatives',
		href: '/alternatives',
		children: [
			{ slug: 'open-source-issue-trackers', label: 'Open Source Issue Trackers', href: '/alternatives/open-source-issue-trackers', description: 'Compare licenses, product scope and commercial edition models.' },
			{ slug: 'self-hosted-issue-trackers', label: 'Self-Hosted Issue Trackers', href: '/alternatives/self-hosted-issue-trackers', description: 'Compare deployment footprint, updates, backups and operator responsibility.' }
		]
	}
};

// ---- Standalone page definitions ----

export interface StandalonePage {
	slug: string;
	title: string;
	description: string;
	canonical: string;
	inSitemap: boolean;
}

export const STANDALONE_PAGES: StandalonePage[] = [
	{ slug: '', title: '', description: '', canonical: url('/'), inSitemap: true },
	{ slug: 'features', title: 'Kuayle Features — Issues, Cycles, Projects and GitHub', description: `Review the issue tracking, multi-assignee, cycle, project, GitHub, keyboard, real-time, analytics and Dev Machine features implemented in Kuayle ${FALLBACK_VERSION}.`, canonical: url('/features'), inSitemap: true },
	{ slug: 'self-hosting', title: 'Self-Host Kuayle With Docker Compose', description: 'Deploy Kuayle with Caddy, PostgreSQL, Redis, the Go API and SvelteKit frontend; configure storage, updates and GitHub webhooks.', canonical: url('/self-hosting'), inSitemap: true },
	{ slug: 'compare', title: 'Compare Kuayle — Issue Tracker Comparisons', description: 'Compare Kuayle with Linear and Plane by hosting model, license, workflow coverage, product maturity and operating cost.', canonical: url('/compare'), inSitemap: true },
	{ slug: 'alternatives', title: 'Issue Tracker Alternatives — Kuayle', description: 'Compare open-source and self-hosted issue trackers by license, edition model, product scope, deployment footprint and operator responsibility.', canonical: url('/alternatives'), inSitemap: true },
	{ slug: 'open-source', title: 'Kuayle Open-Source Model — Apache 2.0, One Edition', description: 'Understand Kuayle’s Apache 2.0 license, single public repository, ungated feature model and self-hosting costs.', canonical: url('/open-source'), inSitemap: true },
	{ slug: 'license', title: 'Kuayle License — Apache 2.0', description: 'A plain-language summary of Kuayle’s Apache 2.0 permissions, redistribution conditions, patent grant and warranty limits.', canonical: url('/license'), inSitemap: true },
	{ slug: 'security', title: 'Kuayle Security and Self-Hosting Responsibilities', description: 'Review Kuayle authentication, network exposure, credential storage, update flow and operator security responsibilities.', canonical: url('/security'), inSitemap: true },
	{ slug: 'about', title: 'About Kuayle — Product, Maintainer and Current State', description: `Why Kuayle exists, who maintains it, how it is built and what the current ${FALLBACK_VERSION} release implements.`, canonical: url('/about'), inSitemap: true },
	{ slug: 'roadmap', title: 'Kuayle Roadmap and Development Status', description: `See what Kuayle ${FALLBACK_VERSION} implements, which product gaps remain and where proposed work is tracked.`, canonical: url('/roadmap'), inSitemap: true },
	{ slug: 'privacy', title: 'Privacy Policy — Kuayle', description: 'Kuayle privacy policy: what we collect, the cookies we use, your GDPR rights, and how to contact us.', canonical: url('/privacy'), inSitemap: true }
];

// ---- All indexable routes (for sitemap + validation) ----

export function allRoutes(): { path: string; priority: number; changefreq: string }[] {
	const routes = new Map<string, { path: string; priority: number; changefreq: string }>();
	const add = (path: string, priority: number, changefreq: string) => {
		routes.set(path, { path, priority, changefreq });
	};

	add('/', 1.0, 'weekly');

	// Standalone
	for (const page of STANDALONE_PAGES) {
		if (!page.slug || !page.inSitemap) continue;
		add(`/${page.slug}`, page.slug === 'privacy' ? 0.3 : 0.9, 'monthly');
	}

	// Hub children
	for (const hub of Object.values(HUBS)) {
		for (const child of hub.children) {
			add(child.href, 0.7, 'monthly');
		}
	}

	return [...routes.values()];
}

// ---- Static metadata for the homepage JSON-LD ----

export const ORGANIZATION_LD = {
	'@context': 'https://schema.org',
	'@type': 'Organization',
	name: 'Bakney srl',
	url: 'https://kuayle.com',
	logo: url('/logo_primary.svg'),
	sameAs: ['https://github.com/carbogninalberto/kuayle'],
	contactPoint: {
		'@type': 'ContactPoint',
		email: 'support@bakney.com',
		contactType: 'customer support'
	}
};

export const WEBSITE_LD = {
	'@context': 'https://schema.org',
	'@type': 'WebSite',
	name: 'Kuayle',
	url: url('/')
};

export const SOFTWARE_APP_LD = {
	'@context': 'https://schema.org',
	'@type': 'SoftwareApplication',
	name: 'Kuayle',
	url: url('/'),
	description: 'A keyboard-driven, self-hosted issue tracker licensed under Apache 2.0.',
	applicationCategory: 'BusinessApplication',
	operatingSystem: 'Linux, Docker',
	offers: {
		'@type': 'Offer',
		price: '0',
		priceCurrency: 'USD'
	},
	license: 'https://www.apache.org/licenses/LICENSE-2.0'
};

export function webPageLd(name: string, description: string, pageUrl: string, breadcrumbs?: Crumb[]) {
	const ld: Record<string, unknown> = {
		'@context': 'https://schema.org',
		'@type': 'WebPage',
		name,
		description,
		url: pageUrl
	};
	if (breadcrumbs && breadcrumbs.length > 0) {
		ld.breadcrumb = {
			'@type': 'BreadcrumbList',
			itemListElement: breadcrumbs.map((crumb, i) => ({
				'@type': 'ListItem',
				position: i + 1,
				name: crumb.label,
				item: crumb.href.startsWith('http') ? crumb.href : url(crumb.href)
			}))
		};
	}
	return ld;
}

// ---- Metadata for dynamic/standalone pages ----

export function metaForStandalone(slug: string): PageMeta | undefined {
	const page = STANDALONE_PAGES.find(p => p.slug === slug);
	if (!page) return undefined;
	return {
		title: page.title,
		description: page.description,
		canonical: page.canonical
	};
}
