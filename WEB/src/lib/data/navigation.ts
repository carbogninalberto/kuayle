/**
 * Navigation menus (navbar dropdowns, mobile sheet and footer columns) built from the route registry,
 * so new pages appear in navigation without a second list to maintain.
 */
import { HUBS } from '$lib/data/routes';

export interface NavLink {
	label: string;
	href: string;
	description?: string;
	external?: boolean;
}

export interface NavGroup {
	label: string;
	/** Overview page for the group. */
	href: string;
	links: NavLink[];
	footer?: NavLink;
}

const featureLinks: NavLink[] = HUBS.features.children.map((child) => ({
	label: child.label,
	href: child.href,
	description: child.description
}));

const alternativeLinks: NavLink[] = HUBS.alternatives.children
	.filter((child) => child.href.endsWith('-alternative'))
	.map((child) => ({ label: child.label.replace(/ alternative$/, ''), href: child.href, description: child.description }));

export const NAV_GROUPS: NavGroup[] = [
	{
		label: 'Features',
		href: '/features',
		links: featureLinks,
		footer: { label: 'All features', href: '/features' }
	},
	{
		label: 'Alternatives',
		href: '/alternatives',
		links: alternativeLinks,
		footer: { label: 'Compare all 28 tools', href: '/compare' }
	},
	{
		label: 'Docs',
		href: '/self-hosting',
		links: [
			{ label: 'Self-hosting guide', href: '/self-hosting', description: 'Deploy the Docker Compose stack on your server.' },
			...HUBS.selfHosting.children.map((child) => ({
				label: child.label,
				href: child.href,
				description: child.description
			})),
			{ label: 'Security', href: '/security', description: 'Authentication, network exposure and operator responsibilities.' },
			{ label: 'Open source and licence', href: '/open-source', description: 'Apache 2.0, one edition, nothing behind a key.' },
			{ label: 'Roadmap', href: '/roadmap', description: 'What ships today, known gaps and proposed work.' }
		],
		footer: { label: 'Source and README on GitHub', href: 'https://github.com/carbogninalberto/kuayle', external: true }
	}
];

export const FOOTER_COLUMNS: { title: string; links: NavLink[] }[] = [
	{
		title: 'Product',
		links: [
			{ label: 'Features', href: '/features' },
			...featureLinks.slice(0, 6).map(({ label, href }) => ({ label, href })),
			{ label: 'Roadmap', href: '/roadmap' },
			{ label: 'Live demo', href: 'https://demo.kuayle.com', external: true }
		]
	},
	{
		title: 'Alternatives',
		links: [
			...alternativeLinks.map(({ label, href }) => ({ label: `${label} alternative`, href })),
			{ label: 'Jira Data Center end of life', href: '/jira-data-center-end-of-life' },
			{ label: 'Comparison matrix', href: '/compare' }
		]
	},
	{
		title: 'Self-hosting',
		links: [
			{ label: 'Overview', href: '/self-hosting' },
			...HUBS.selfHosting.children.map(({ label, href }) => ({ label, href })),
			{ label: 'Security', href: '/security' }
		]
	},
	{
		title: 'Open source',
		links: [
			{ label: 'Open-source model', href: '/open-source' },
			{ label: 'Licence (Apache 2.0)', href: '/license' },
			{ label: 'Open source issue trackers', href: '/alternatives/open-source-issue-trackers' },
			{ label: 'Self-hosted issue trackers', href: '/alternatives/self-hosted-issue-trackers' },
			{ label: 'GitHub repository', href: 'https://github.com/carbogninalberto/kuayle', external: true },
			{ label: 'About', href: '/about' },
			{ label: 'Privacy', href: '/privacy' }
		]
	}
];
