/**
 * Content registry for alternatives overview pages.
 * Each entry maps to /alternatives/[slug].
 */
import type { ContentRegistry } from './routes';

export const alternatives: ContentRegistry = {
	'open-source-issue-trackers': {
		slug: 'open-source-issue-trackers',
		title: 'Open Source Issue Trackers — Alternatives to Kuayle',
		description:
			'Compare open-source issue tracker categories by license, deployment model, product scope, and commercial edition structure.',
		heading: 'Open Source Issue Trackers',
		intro:
			'“Open source issue tracker” describes several different products: focused trackers, project-management suites and issue systems attached to source hosting. License and edition model matter as much as the feature list.',
		sections: [
			{
				heading: 'The landscape',
				body: 'Start by deciding whether issues are the product’s center or one module in a larger suite.',
				list: [
					'Focused and agile tools: Kuayle, Plane, and Taiga — issue workflows and sprint or cycle planning',
					'All-in-one PM: Redmine, OpenProject, Leantime — broader scope with Gantt, wikis, time tracking',
					'Platform-embedded: GitLab Issues, Gitea, Gogs — tracking alongside source code hosting'
				]
			},
			{
				heading: 'Licensing differences',
				body: 'Source availability does not make licenses interchangeable. Permissive licenses and copyleft licenses impose different redistribution obligations, while some vendors maintain paid editions or features alongside a public repository.',
				list: [
					'Apache 2.0: Kuayle — permits commercial use and modification subject to license conditions',
					'Copyleft projects use licenses such as AGPL or GPL; review each project’s current license and obligations',
					'Open core: some tools offer basic features as open source with paid enterprise add-ons'
				]
			},
			{
				heading: 'Where Kuayle fits',
				body: 'Kuayle is a focused, self-hosted issue tracker rather than a general project-management suite. Its current application and deployment code live in one Apache 2.0 repository, with no enterprise edition or license-key path.',
				list: [
					'Multiple assignees on one issue',
					'Keyboard shortcuts and issue search',
					'Cycles, projects and GitHub automation',
					'Reference Docker Compose deployment'
				]
			},
			{
				heading: 'Sources',
				body: 'Licenses and editions change. Verify them in each project’s first-party repository or documentation.',
				links: [
					{ label: 'Kuayle repository', href: 'https://github.com/carbogninalberto/kuayle' },
					{ label: 'Plane repository', href: 'https://github.com/makeplane/plane' },
					{ label: 'Taiga repository', href: 'https://github.com/taigaio/taiga-back' },
					{ label: 'OpenProject repository', href: 'https://github.com/opf/openproject' }
				]
			}
		],
		footnotes: 'Last reviewed: July 11, 2026. This is an informational overview, not an endorsement. Tool availability, features, and licensing can change. Always review the latest project documentation directly.'
	},

	'self-hosted-issue-trackers': {
		slug: 'self-hosted-issue-trackers',
		title: 'Self-Hosted Issue Trackers — Alternatives to Kuayle',
		description:
			'Compare self-hosted issue trackers by deployment footprint, license, update path, storage, backup responsibilities, and product scope.',
		heading: 'Self-Hosted Issue Trackers',
		intro:
			'Self-hosting moves control—and operational responsibility—to your team. Compare the application and the work required to keep it available, secure and recoverable.',
		sections: [
			{
				heading: 'What self-hosting changes',
				body: 'You choose where the service and primary data run, which network can reach them, and how backups are stored. You also take responsibility for patching, monitoring, capacity, incident response and restore testing.',
				list: [
					'Data location and network policy under operator control',
					'Infrastructure and staff time replace managed-service operations',
					'Backups and disaster recovery are your responsibility',
					'Self-hosting alone does not establish regulatory compliance'
				]
			},
			{
				heading: 'Different product scopes',
				body: 'The main alternatives do not target exactly the same workflow. Verify current editions and deployment support in first-party documentation.',
				list: [
					'Kuayle — focused issue tracking, cycles, projects and GitHub automation; Apache 2.0',
					'Plane — project-management suite with pages, modules, dashboards and commercial plans; AGPL-3.0 public repository',
					'Taiga — Scrum and Kanban-oriented project management',
					'OpenProject — broader project management including Gantt and time-related workflows',
					'GitLab and Gitea — issue tracking integrated with source-code hosting'
				]
			},
			{
				heading: 'What to consider',
				body: 'Evaluate the deployment you will actually operate, not only a product screenshot. Kuayle’s reference stack uses five services in one Compose file; other products may support additional deployment methods or require more services.',
				list: [
					'Deployment and update procedure',
					'Database, upload and certificate backups',
					'Authentication and integration requirements',
					'Product scope: focused issue tracking or broader project management'
				]
			},
			{
				heading: 'Sources',
				body: 'Use first-party deployment and licensing documentation before selecting a tool.',
				links: [
					{ label: 'Kuayle self-hosting guide', href: 'https://kuayle.com/self-hosting' },
					{ label: 'Plane self-hosting documentation', href: 'https://developers.plane.so/self-hosting/overview' },
					{ label: 'Taiga installation guide', href: 'https://docs.taiga.io/setup-production.html' },
					{ label: 'OpenProject installation guide', href: 'https://www.openproject.org/docs/installation-and-operations/' }
				]
			}
		],
		footnotes: 'Last reviewed: July 11, 2026. This overview reflects publicly available information and is not a guarantee of current accuracy. Features, deployment options, and licensing change. Always consult each project\'s official documentation.'
	}
};
