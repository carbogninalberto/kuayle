/**
 * Content for the Tier A `/<competitor>-alternative` pages.
 * External facts come from the competitor's `sources` in competitors.ts (checked 2026-11-06);
 * Kuayle facts come from kuayle-facts.ts. Keep both sides honest: state Kuayle gaps plainly.
 */
import type { AlternativePageContent, Competitor } from './competitor-types.ts';
import { competitors } from './competitors.ts';

export function competitorBySlug(slug: string): Competitor {
	const competitor = competitors.find((item) => item.slug === slug);
	if (!competitor) throw new Error(`Unknown competitor ${slug}`);
	return competitor;
}

const SELF_HOSTED_MIGRATION = (from: string, exportHow: string) => [
	{
		heading: 'Export from ' + from,
		body: exportHow
	},
	{
		heading: 'Deploy Kuayle',
		body: 'Run the Docker Compose stack on your server (Caddy, PostgreSQL, Redis, API and frontend), then create a workspace and invite the team.'
	},
	{
		heading: 'Recreate teams and workflows',
		body: 'Set up teams (public or private), team statuses, labels and issue templates before moving work, so issues land in the right place.'
	},
	{
		heading: 'Move open work through the API',
		body: `Kuayle has no built-in ${from} importer yet. Script the move of open issues with a scoped personal access token and the published API reference (kuayle.com/api-reference), or start fresh with active work and keep ${from} read-only for history.`
	}
];

export const alternativePages: AlternativePageContent[] = [
	// ---------------------------------------------------------------- Linear
	{
		competitor: 'linear',
		title: 'Open Source Linear Alternative, Self-Hosted and Free (2026) | Kuayle',
		description:
			'Looking for a self-hosted, open source Linear alternative? Compare Kuayle and Linear on licence, hosting, per-seat pricing, SSO, AI, integrations and migration.',
		h1: 'The open source, self-hosted Linear alternative',
		intro:
			'Kuayle is a keyboard-first issue tracker you run on your own server, under Apache 2.0, with no per-seat price and no paid tier. Linear is a polished hosted product; this page shows where Kuayle matches it, where it does not yet, and what each costs.',
		difference: 'Linear is a mature hosted service billed per user; Kuayle is free software you host, with a smaller feature set.',
		whyLeave: [
			{
				heading: 'The bill grows with every hire',
				body: 'Linear bills per user: $10 on Basic and $16 on Business, billed yearly. The Free plan stops at 2 teams and 250 issues.'
			},
			{
				heading: 'The data has to stay in-house',
				body: 'Linear runs only in its own cloud, with workspace data in the EU or the US. It documents no self-hosted edition.'
			},
			{
				heading: 'SSO and SCIM need Enterprise',
				body: 'Google sign-in is on every plan, but SAML SSO and SCIM provisioning are on the custom-priced Enterprise plan only.'
			},
			{
				heading: 'Agent work is metered',
				body: 'Linear’s cloud coding agents and Loops draw on separately purchased, prepaid AI credits.'
			}
		],
		rows: [
			{ dimension: 'Licence', kuayle: 'license', mark: 'no', text: 'Proprietary. The source is not published.' },
			{ dimension: 'Hosting', kuayle: 'hosting', mark: 'no', text: 'Linear’s cloud only, with an EU or US data region chosen at workspace creation.' },
			{ dimension: 'Pricing', kuayle: 'pricing', mark: 'partial', text: 'Free plan (2 teams, 250 issues), then $10 Basic or $16 Business per user a month, billed yearly; Enterprise by quote.' },
			{ dimension: 'SSO and SCIM', kuayle: 'sso', mark: 'partial', text: 'Google SSO on all plans; SAML and SCIM on Enterprise only.' },
			{ dimension: 'AI and agents', kuayle: 'ai', mark: 'yes', text: 'Linear Agent and the agent platform on every plan; cloud coding agents and Loops use prepaid AI credits.' },
			{ dimension: 'MCP server', kuayle: 'mcp', mark: 'yes', text: 'Official remote MCP server at mcp.linear.app, on all plans.' },
			{ dimension: 'API and webhooks', kuayle: 'api', mark: 'yes', text: 'GraphQL API and admin-created webhooks on all plans.' },
			{ dimension: 'Importers', kuayle: 'importers', mark: 'yes', text: 'Jira, GitHub Issues, Asana and Shortcut, plus a CLI importer.' },
			{ dimension: 'Git integrations', kuayle: 'gitForges', mark: 'yes', text: 'GitHub (PR linking and two-way issue sync) and GitLab; GitHub Enterprise Server on Enterprise.' },
			{ dimension: 'Where data lives', kuayle: 'data', mark: 'partial', text: 'In Linear’s cloud, in the EU or US region you picked.' },
			{ dimension: 'Issue model', kuayle: 'issues', mark: 'yes', text: 'Mature issue workflow with sub-issues, relations, labels, cycles and projects.' }
		],
		costPlans: { entry: 'Basic', business: 'Business' },
		costNote:
			'Linear list prices billed yearly, checked 6 November 2026, before AI credits and tax. Kuayle has no licence fee; you pay for your own server and its operation.',
		migration: SELF_HOSTED_MIGRATION(
			'Linear',
			'Linear exports workspace issues, views and projects to CSV, and its GraphQL API reads everything else. Keep the export as your archive.'
		),
		stayIf: [
			'You want someone else to run, patch and back up the tracker.',
			'You rely on Linear’s importers, initiatives or agent platform today.',
			'You need SAML SSO now (on Linear Enterprise); Kuayle offers OIDC SSO free but not SAML yet.',
			'Per-seat cost is fine and product maturity matters more than control of the code.'
		],
		switchIf: [
			'Issues and code have to stay on your own infrastructure.',
			'Headcount should not change what the tracker costs.',
			'You want to read, audit and change the source under Apache 2.0.',
			'Multiple assignees per issue, private teams and edit history cover your workflow.'
		],
		others: ['plane', 'itsaplan', 'openproject', 'jira', 'youtrack', 'taiga'],
		faq: [
			{
				question: 'Is there an open source alternative to Linear?',
				answer: 'Yes. Kuayle is an open source Linear alternative under Apache 2.0 that you host yourself. Plane, It’s a Plan, OpenProject and Taiga are other open source trackers to compare.'
			},
			{
				question: 'Can you self-host Linear?',
				answer: 'No. Linear runs only in its own cloud, with an EU or US data region. To keep the tracker on your own servers you need a self-hosted alternative such as Kuayle.'
			},
			{
				question: 'Is there a free Linear alternative?',
				answer: 'Kuayle is free: no licence fee, no per-seat price and no paid tier. You pay only for the server it runs on. Linear’s own Free plan stops at 2 teams and 250 issues.'
			},
			{
				question: 'Can I import my Linear workspace into Kuayle?',
				answer: 'Not with a built-in importer yet. Export from Linear to CSV or read it through its API, then create issues in Kuayle through its documented HTTP API with a scoped personal access token.'
			},
			{
				question: 'Does Kuayle have SSO and an MCP server like Linear?',
				answer: 'An MCP server, yes: every Kuayle instance serves one at /mcp, where agents sign in with a scoped personal access token (OAuth sign-in is not available yet). SSO, partly: Kuayle offers OIDC single sign-on free in every workspace, but not SAML or SCIM yet. If SAML is a hard requirement now, Linear covers it.'
			},
			{
				question: 'How does Kuayle feel compared with Linear?',
				answer: 'Both are keyboard-first, with a ⌘K command palette, shortcuts, cycles and projects. Kuayle adds multiple assignees per issue, private teams and title and description history.'
			}
		]
	},

	// ---------------------------------------------------------------- Jira
	{
		competitor: 'jira',
		title: 'Open Source Jira Alternative, Self-Hosted and Free (2026) | Kuayle',
		description:
			'Jira Data Center ends in 2029. Compare Kuayle, a free, open source, self-hosted Jira alternative, with Jira Cloud on hosting, pricing, SSO, AI and migration.',
		h1: 'The open source, self-hosted Jira alternative',
		intro:
			'Atlassian has stopped selling Jira Data Center to new customers and ends it on 28 March 2029, so the self-managed option is closing. Kuayle is an Apache 2.0 issue tracker you host yourself, with no licence fee. It is far smaller than Jira; this page shows the trade-off.',
		difference: 'Jira is moving everyone to its cloud; Kuayle stays on your servers, with a narrower, developer-focused scope.',
		whyLeave: [
			{
				heading: 'Data Center is ending',
				body: 'New customers can no longer buy Jira Data Center since 30 March 2026, and it reaches end of life on 28 March 2029. After that it becomes read-only.'
			},
			{
				heading: 'The cloud is the only path left',
				body: 'For new customers, Jira is cloud-only. Teams with an on-premises requirement need a different product.'
			},
			{
				heading: 'SSO is a separate subscription',
				body: 'On Free, Standard and Premium, SSO, SCIM and AD sync need an Atlassian Guard Standard subscription on top of Jira.'
			},
			{
				heading: 'Per-user prices add up',
				body: 'Atlassian lists Standard at $8.48 and Premium at $15.56 per user a month on monthly billing; Enterprise is by quote.'
			}
		],
		rows: [
			{ dimension: 'Licence', kuayle: 'license', mark: 'no', text: 'Proprietary.' },
			{ dimension: 'Hosting', kuayle: 'hosting', mark: 'partial', text: 'Jira Cloud. Data Center is closed to new customers and ends on 28 March 2029.' },
			{ dimension: 'Pricing', kuayle: 'pricing', mark: 'partial', text: 'Free up to 10 users; Standard $8.48 and Premium $15.56 per user a month (monthly billing); Enterprise by quote.' },
			{ dimension: 'SSO and SCIM', kuayle: 'sso', mark: 'partial', text: 'Needs an Atlassian Guard Standard subscription, except on Enterprise, which includes it.' },
			{ dimension: 'AI and agents', kuayle: 'ai', mark: 'yes', text: 'Rovo Search, Chat and Agents on paid plans, with pooled monthly credits per user.' },
			{ dimension: 'MCP server', kuayle: 'mcp', mark: 'yes', text: 'Official Atlassian Rovo MCP server; some tools consume Rovo credits.' },
			{ dimension: 'API and webhooks', kuayle: 'api', mark: 'yes', text: 'REST API (v2 and v3) and webhooks.' },
			{ dimension: 'Importers', kuayle: 'importers', mark: 'yes', text: 'Asana, monday, ClickUp, Trello, Azure DevOps, GitHub, GitLab, Linear, Notion, CSV and more.' },
			{ dimension: 'Git integrations', kuayle: 'gitForges', mark: 'yes', text: 'Bitbucket, GitHub, GitHub Enterprise Server, GitLab (Marketplace app) and Azure DevOps.' },
			{ dimension: 'Custom fields', kuayle: 'customFields', mark: 'yes', text: 'Extensive custom fields and workflow configuration.' },
			{ dimension: 'Issue model', kuayle: 'issues', mark: 'yes', text: 'Highly configurable issue types, workflows, boards and automation.' }
		],
		costPlans: { entry: 'Standard', business: 'Premium' },
		costNote:
			'Atlassian publishes per-user prices for monthly billing only (Standard $8.48, Premium $15.56, checked 6 November 2026); the yearly figures here are those prices × 12. Annual plans are priced by user tier. Atlassian Guard, Marketplace apps and tax are extra. Kuayle has no licence fee.',
		migration: SELF_HOSTED_MIGRATION(
			'Jira',
			'Export work items to CSV from the list view or search (attachments come out as URLs), or read them with the Jira REST API.'
		),
		stayIf: [
			'You depend on custom fields, complex workflows, Marketplace apps or Jira Service Management.',
			'Moving to Jira Cloud is acceptable for your data and compliance rules.',
			'You need SAML SSO today; Kuayle offers OIDC SSO free but not SAML yet.',
			'Non-engineering teams share the same instance and rely on its breadth.'
		],
		switchIf: [
			'You run Data Center and the tracker must stay on your own infrastructure after 2029.',
			'Your team mainly needs issues, cycles, projects and GitHub linking, not a configurable platform.',
			'Licence and per-user costs should drop to zero.',
			'You want an Apache 2.0 codebase you can audit and change.'
		],
		others: ['openproject', 'youtrack', 'plane', 'gitlab', 'linear', 'itsaplan'],
		faq: [
			{
				question: 'When does Jira Data Center reach end of life?',
				answer: 'Atlassian ends Jira Software and Jira Service Management Data Center on 28 March 2029. New customer sales ended on 30 March 2026, and existing customers can buy or expand until 30 March 2028.'
			},
			{
				question: 'Is there a self-hosted Jira alternative?',
				answer: 'Yes. Kuayle is a free, self-hosted Jira alternative under Apache 2.0. OpenProject, YouTrack Server, Plane and GitLab are other self-hostable options.'
			},
			{
				question: 'Is there a free, open source Jira alternative?',
				answer: 'Kuayle is free and open source, with no licence fee or paid tier. It is narrower than Jira: there are no custom fields, Marketplace apps or service desk.'
			},
			{
				question: 'Can I migrate from Jira to Kuayle?',
				answer: 'There is no built-in Jira importer yet. Export to CSV or read the Jira REST API, then create issues in Kuayle through its documented HTTP API with a scoped personal access token.'
			},
			{
				question: 'Does Kuayle replace Jira Service Management?',
				answer: 'No. Kuayle is an issue tracker for product and engineering teams. It has a triage inbox, but no customer portal, SLAs or service desk features.'
			}
		]
	},

	// ---------------------------------------------------------------- Plane
	{
		competitor: 'plane',
		title: 'Open Source Plane.so Alternative, Self-Hosted and Free (2026) | Kuayle',
		description:
			'Compare Kuayle and Plane.so, two self-hostable open source issue trackers: licence, editions, per-seat pricing, SSO, AI, importers and Git integrations.',
		h1: 'An open source Plane.so alternative with no paid tier',
		intro:
			'Plane and Kuayle can both run on your own server. The difference is the edition model: Plane’s Community Edition is AGPL-3.0, while paid features live in a closed Commercial Edition. Kuayle is one Apache 2.0 codebase with nothing gated.',
		difference: 'Plane is broader and has importers and AI; Kuayle has one ungated edition under a permissive licence.',
		whyLeave: [
			{
				heading: 'Paid features need a closed edition',
				body: 'Self-hosted teams must switch from the AGPL Community Edition to the closed-source Commercial Edition before upgrading to a paid plan.'
			},
			{
				heading: 'Git integrations are paid',
				body: 'Plane’s GitHub and GitLab integrations start on Pro and are not on Free or the Community Edition.'
			},
			{
				heading: 'Per-seat pricing past 12 users',
				body: 'The free plan covers up to 12 users; Pro is $6 and Business $13 per user a month, billed yearly.'
			},
			{
				heading: 'AI is cloud-only',
				body: 'Plane’s AI credits and agents are cloud-only and included in paid plans.'
			}
		],
		rows: [
			{ dimension: 'Licence', kuayle: 'license', mark: 'partial', text: 'Community Edition AGPL-3.0; the Commercial Edition is closed source.' },
			{ dimension: 'Hosting', kuayle: 'hosting', mark: 'yes', text: 'Plane Cloud, or self-hosted Community or Commercial Edition.' },
			{ dimension: 'Pricing', kuayle: 'pricing', mark: 'partial', text: 'Free up to 12 users; Pro $6 and Business $13 per user a month billed yearly; Enterprise custom.' },
			{ dimension: 'SSO and SCIM', kuayle: 'sso', mark: 'partial', text: 'SAML and OIDC from Pro (self-hosted) or Business (cloud); LDAP on Enterprise.' },
			{ dimension: 'AI and agents', kuayle: 'ai', mark: 'partial', text: 'AI credits and agents on paid cloud plans only.' },
			{ dimension: 'MCP server', kuayle: 'mcp', mark: 'yes', text: 'Official MCP server, hosted or local stdio for self-hosted.' },
			{ dimension: 'API and webhooks', kuayle: 'api', mark: 'yes', text: 'REST API and workspace webhooks.' },
			{ dimension: 'Importers', kuayle: 'importers', mark: 'yes', text: 'Jira, Linear, Asana, ClickUp and CSV on all plans.' },
			{ dimension: 'Git integrations', kuayle: 'gitForges', mark: 'partial', text: 'GitHub and GitLab from Pro upward.' },
			{ dimension: 'Issue model', kuayle: 'issues', mark: 'yes', text: 'Work items with cycles, modules, views and pages.' },
			{ dimension: 'Teams and access', kuayle: 'access', mark: 'yes', text: 'Workspaces, projects and roles.' }
		],
		costPlans: { entry: 'Pro', business: 'Business' },
		costNote:
			'Plane list prices billed yearly, checked 6 November 2026, before tax. The first 12 users are free on Plane’s free plan. Kuayle has no licence fee.',
		migration: SELF_HOSTED_MIGRATION(
			'Plane',
			'Workspace admins can export projects as CSV, Excel or JSON; the REST API reads the rest.'
		),
		stayIf: [
			'You use Plane’s importers, pages or AI.',
			'You are on the free tier with 12 users or fewer and need nothing paid.',
			'You need SAML SSO now; Kuayle offers OIDC SSO free but not SAML yet.'
		],
		switchIf: [
			'You want every feature in one open source edition with no licence key.',
			'You prefer Apache 2.0 terms to AGPL-3.0.',
			'You want the GitHub integration without a paid plan.',
			'Multiple assignees per issue and private teams matter to you.'
		],
		others: ['itsaplan', 'linear', 'openproject', 'taiga', 'jira', 'youtrack'],
		faq: [
			{
				question: 'What is the best open source Plane alternative?',
				answer: 'It depends on what you need. Kuayle is an Apache 2.0 tracker with no paid tier; It’s a Plan, OpenProject and Taiga are other open source options with different scopes.'
			},
			{
				question: 'Is Plane fully open source?',
				answer: 'Plane’s Community Edition is AGPL-3.0. Paid plans run on the Commercial Edition, which is closed source.'
			},
			{
				question: 'Can I self-host Kuayle and Plane the same way?',
				answer: 'Both run with Docker. Kuayle ships one Docker Compose stack with Caddy, PostgreSQL, Redis, the API and the frontend, and has no separate commercial build.'
			},
			{
				question: 'Can I move from Plane to Kuayle?',
				answer: 'There is no Plane importer yet. Export to CSV or JSON, or read Plane’s REST API, and create issues in Kuayle through its documented HTTP API with a personal access token.'
			},
			{
				question: 'Does Kuayle have AI features like Plane?',
				answer: 'Kuayle has a built-in assistant that runs on your own OpenAI-compatible model and delegates issues to coding agents such as Claude Code or Codex on opt-in Dev Machines on your own servers. There are no AI credits to buy. External agents reach issues and wiki pages through its built-in MCP server.'
			}
		]
	},

	// ---------------------------------------------------------------- Asana
	{
		competitor: 'asana',
		title: 'Open Source Asana Alternative for Software Teams, Self-Hosted (2026) | Kuayle',
		description:
			'Compare Kuayle, a free, self-hosted, open source Asana alternative for software teams, with Asana on pricing, SSO, AI, GitHub integration and data location.',
		h1: 'The open source, self-hosted Asana alternative for software teams',
		intro:
			'Asana is a general work-management product in the cloud. Kuayle is an issue tracker for product and engineering teams that you host yourself, with GitHub linking, cycles and no per-seat price.',
		difference: 'Asana covers all kinds of work in its cloud; Kuayle focuses on software teams, on your own server.',
		whyLeave: [
			{ heading: 'Per-seat prices are high', body: 'Starter is $10.99 and Advanced $24.99 per user a month billed yearly, and seats are sold in increments.' },
			{ heading: 'The free plan is for two people', body: 'Asana’s Personal plan is free for up to 2 users.' },
			{ heading: 'SSO is Enterprise-only', body: 'SAML SSO and SCIM are on the quote-priced Enterprise tiers only.' },
			{ heading: 'Developer integrations are thin', body: 'There is an official GitHub app; GitLab relies on the third-party Unito integration.' }
		],
		rows: [
			{ dimension: 'Licence', kuayle: 'license', mark: 'no', text: 'Proprietary.' },
			{ dimension: 'Hosting', kuayle: 'hosting', mark: 'no', text: 'Asana’s cloud only; data residency is an Enterprise+ option.' },
			{ dimension: 'Pricing', kuayle: 'pricing', mark: 'partial', text: 'Free for 2 users; Starter $10.99 and Advanced $24.99 per user a month billed yearly.' },
			{ dimension: 'SSO and SCIM', kuayle: 'sso', mark: 'partial', text: 'SAML and SCIM on Enterprise and Enterprise+ only.' },
			{ dimension: 'AI and agents', kuayle: 'ai', mark: 'yes', text: 'AI Teammates and AI Studio, with per-user request limits and paid overage.' },
			{ dimension: 'MCP server', kuayle: 'mcp', mark: 'yes', text: 'Official V2 MCP server.' },
			{ dimension: 'API and webhooks', kuayle: 'api', mark: 'yes', text: 'REST API with webhooks and events.' },
			{ dimension: 'Importers', kuayle: 'importers', mark: 'yes', text: 'CSV importer for Excel, Smartsheet, Wrike, monday.com, Airtable and Trello exports.' },
			{ dimension: 'Git integrations', kuayle: 'gitForges', mark: 'partial', text: 'Official GitHub app; GitLab only via Unito.' },
			{ dimension: 'Planning', kuayle: 'planning', mark: 'yes', text: 'Timelines, goals, portfolios and dashboards on paid plans.' }
		],
		costPlans: { entry: 'Starter', business: 'Advanced' },
		costNote:
			'Asana list prices billed yearly, checked 6 November 2026, before tax. Asana sells seats in increments, so real bills can be higher. Kuayle has no licence fee.',
		migration: SELF_HOSTED_MIGRATION('Asana', 'Export projects to CSV, or read tasks through the Asana REST API.'),
		stayIf: [
			'Marketing, operations and other non-engineering teams plan in the same tool.',
			'You use Asana goals, portfolios, timelines or AI Studio.',
			'You need SAML SSO now; Kuayle offers OIDC SSO free but not SAML yet.'
		],
		switchIf: [
			'Your engineering team needs issues linked to GitHub branches, commits and pull requests.',
			'Data must stay on your own servers.',
			'You want cycles with burndown and velocity without paying per seat.'
		],
		others: ['clickup', 'trello', 'openproject', 'plane', 'linear', 'jira'],
		faq: [
			{ question: 'Is there an open source alternative to Asana?', answer: 'Yes. Kuayle is an open source, self-hosted alternative for software teams. OpenProject and Taiga are open source options for broader project management.' },
			{ question: 'Can Asana be self-hosted?', answer: 'No. Asana runs only in its own cloud. To keep project data on your servers you need a self-hosted tool such as Kuayle.' },
			{ question: 'Is there a free Asana alternative?', answer: 'Kuayle is free with no seat limit and no paid tier; you pay only for hosting. Asana’s own free plan covers 2 users.' },
			{ question: 'Is Kuayle a good fit for non-technical teams?', answer: 'Kuayle is built for product and engineering work. Teams that mostly need task lists, forms and portfolios may be better served by a general tool.' },
			{ question: 'Can I import Asana tasks into Kuayle?', answer: 'Not with a built-in importer yet. Export to CSV or use Asana’s REST API, then create issues through Kuayle’s documented HTTP API.' }
		]
	},

	// ---------------------------------------------------------------- ClickUp
	{
		competitor: 'clickup',
		title: 'Open Source ClickUp Alternative, Self-Hosted and Free (2026) | Kuayle',
		description:
			'Compare Kuayle, a free, self-hosted, open source ClickUp alternative for software teams, with ClickUp on pricing, SSO, AI credits, integrations and data location.',
		h1: 'The open source, self-hosted ClickUp alternative',
		intro:
			'ClickUp packs tasks, docs, chat and AI into one hosted platform. Kuayle does less: it is a focused, keyboard-first issue tracker for software teams, under Apache 2.0, that runs on your own server with no per-seat price.',
		difference: 'ClickUp is an all-in-one hosted platform; Kuayle is a focused issue tracker you host.',
		whyLeave: [
			{ heading: 'Enterprise SSO costs extra', body: 'SAML, Okta and Microsoft SSO and SCIM are on Enterprise and MAX only; Google SSO starts on Business.' },
			{ heading: 'Free limits are tight', body: 'Free Forever allows 60MB of storage and 100 automation actions a month.' },
			{ heading: 'AI is metered in credits', body: 'Brain credits are pooled per user; extra credits cost $10 per 10K.' },
			{ heading: 'Cloud only', body: 'ClickUp is hosted SaaS; data residency choices are on Enterprise and MAX.' }
		],
		rows: [
			{ dimension: 'Licence', kuayle: 'license', mark: 'no', text: 'Proprietary.' },
			{ dimension: 'Hosting', kuayle: 'hosting', mark: 'no', text: 'ClickUp’s cloud only; US, EU or APAC residency on Enterprise and MAX.' },
			{ dimension: 'Pricing', kuayle: 'pricing', mark: 'partial', text: 'Free Forever; Core $9 and Business $17 per user a month billed yearly; Enterprise by quote.' },
			{ dimension: 'SSO and SCIM', kuayle: 'sso', mark: 'partial', text: 'Google SSO from Business; SAML, Okta, Microsoft and SCIM on Enterprise and MAX.' },
			{ dimension: 'AI and agents', kuayle: 'ai', mark: 'yes', text: 'Brain credits per user, with paid top-ups.' },
			{ dimension: 'MCP server', kuayle: 'mcp', mark: 'yes', text: 'Official remote MCP server on all plans, rate-limited on Free.' },
			{ dimension: 'API and webhooks', kuayle: 'api', mark: 'yes', text: 'REST API v2 with webhooks.' },
			{ dimension: 'Importers', kuayle: 'importers', mark: 'yes', text: 'Asana, Basecamp, Confluence, Jira, monday.com, Todoist, Trello and Wrike.' },
			{ dimension: 'Git integrations', kuayle: 'gitForges', mark: 'yes', text: 'GitHub, GitLab and Bitbucket.' },
			{ dimension: 'Views', kuayle: 'views', mark: 'yes', text: 'Many views, including list, board, table, calendar and Gantt.' }
		],
		costPlans: { entry: 'Core', business: 'Business' },
		costNote:
			'ClickUp list prices billed yearly, checked 6 November 2026, before AI add-ons and tax. Kuayle has no licence fee.',
		migration: SELF_HOSTED_MIGRATION('ClickUp', 'Export list and table views (Business and above) or read tasks through the ClickUp REST API.'),
		stayIf: [
			'You want docs, chat, whiteboards and tasks in one product.',
			'You rely on ClickUp’s many views, automations or AI.',
			'You need SAML SSO now; Kuayle offers OIDC SSO free but not SAML yet.'
		],
		switchIf: [
			'Your engineering team wants a fast, focused tracker rather than an all-in-one platform.',
			'Data must stay on your own servers.',
			'Seat count should not change the price.'
		],
		others: ['asana', 'trello', 'linear', 'plane', 'jira', 'openproject'],
		faq: [
			{ question: 'Is there an open source ClickUp alternative?', answer: 'Yes. Kuayle is an open source, self-hosted alternative focused on software teams. OpenProject, Plane and Taiga are other open source options.' },
			{ question: 'Can ClickUp be self-hosted?', answer: 'No. ClickUp is cloud-only. Kuayle runs on your own server with Docker Compose.' },
			{ question: 'Is there a free ClickUp alternative without limits?', answer: 'Kuayle has no paid tier, seat cap or storage quota of its own; limits come only from the server you run it on.' },
			{ question: 'Does Kuayle have docs and whiteboards like ClickUp?', answer: 'No. Kuayle is an issue tracker. It has issues, cycles, projects, views and GitHub linking, but no docs or whiteboards.' },
			{ question: 'Can I import from ClickUp?', answer: 'There is no built-in importer yet. Export from ClickUp or use its API, then create issues in Kuayle through its documented HTTP API.' }
		]
	},

	// ---------------------------------------------------------------- YouTrack
	{
		competitor: 'youtrack',
		title: 'Open Source YouTrack Alternative, Self-Hosted and Free (2026) | Kuayle',
		description:
			'Compare Kuayle, a free, open source, self-hosted YouTrack alternative, with JetBrains YouTrack on licence, Cloud and Server pricing, SSO, AI and integrations.',
		h1: 'The open source YouTrack alternative',
		intro:
			'YouTrack can run in JetBrains’ cloud or on your own server, but it is proprietary and billed per user past 10 users. Kuayle is self-hosted too, under Apache 2.0, with no licence fee at any size.',
		difference: 'Both can run on your server; YouTrack is proprietary and per-user, Kuayle is Apache 2.0 and free.',
		whyLeave: [
			{ heading: 'Closed source', body: 'YouTrack is proprietary software; you cannot read or change its code.' },
			{ heading: 'Billing starts at 11 users', body: 'YouTrack is free for up to 10 users and 3 helpdesk agents; after that every user is billed.' },
			{ heading: 'Pricing changed in 2025', body: 'JetBrains moved YouTrack Cloud to per-user pricing on 1 October 2025; legacy fixed-user plans could not be renewed.' },
			{ heading: 'Server is sold in user packs', body: 'Self-hosted YouTrack Server is licensed in user packs rather than free for any team size.' }
		],
		rows: [
			{ dimension: 'Licence', kuayle: 'license', mark: 'no', text: 'Proprietary.' },
			{ dimension: 'Hosting', kuayle: 'hosting', mark: 'yes', text: 'YouTrack Cloud or self-hosted YouTrack Server.' },
			{ dimension: 'Pricing', kuayle: 'pricing', mark: 'partial', text: 'Free for 10 users; Cloud $4.50 per user a month billed yearly; Server in user packs.' },
			{ dimension: 'SSO and SCIM', kuayle: 'sso', mark: 'yes', text: 'SAML 2.0 with SCIM 2.0 provisioning documented.' },
			{ dimension: 'AI and agents', kuayle: 'ai', mark: 'yes', text: 'AI Assistant on Cloud and Server, currently free with weekly credits.' },
			{ dimension: 'MCP server', kuayle: 'mcp', mark: 'yes', text: 'Official remote MCP server (documented for Cloud).' },
			{ dimension: 'API and webhooks', kuayle: 'api', mark: 'yes', text: 'REST API with permanent tokens.' },
			{ dimension: 'Importers', kuayle: 'importers', mark: 'yes', text: 'Jira, GitHub, GitLab, monday.com, ClickUp, Zendesk and more.' },
			{ dimension: 'Git integrations', kuayle: 'gitForges', mark: 'yes', text: 'GitHub, GitLab, Bitbucket, Azure Repos and Gitea/Gogs.' },
			{ dimension: 'Custom fields', kuayle: 'customFields', mark: 'yes', text: 'Custom fields, workflows and time tracking.' }
		],
		costPlans: { entry: 'Cloud', business: null },
		costNote:
			'YouTrack Cloud list price billed yearly ($4.50 per user a month), checked 6 November 2026; JetBrains discounts larger teams and the first 10 users are free, so real bills can be lower. Kuayle has no licence fee.',
		migration: SELF_HOSTED_MIGRATION('YouTrack', 'Export selected issues to XLSX or CSV (500 by default), or read JSON through the REST API.'),
		stayIf: [
			'You use YouTrack’s custom fields, workflows, time tracking or helpdesk.',
			'You have 10 users or fewer and the free tier covers you.',
			'You need SAML SSO now; Kuayle offers OIDC SSO free but not SAML yet.'
		],
		switchIf: [
			'You want an open source tracker you can audit and change.',
			'Your team is past 10 users and seats should stay free.',
			'Issues, cycles, projects and GitHub linking cover your workflow.'
		],
		others: ['jira', 'openproject', 'linear', 'plane', 'gitlab', 'taiga'],
		faq: [
			{ question: 'Is YouTrack open source?', answer: 'No. YouTrack is proprietary JetBrains software, available as Cloud or self-hosted Server.' },
			{ question: 'What is a good open source YouTrack alternative?', answer: 'Kuayle is an Apache 2.0, self-hosted issue tracker with no licence fee. OpenProject and Taiga are other open source options.' },
			{ question: 'Is YouTrack free?', answer: 'YouTrack is free for up to 10 users and 3 helpdesk agents. Beyond that, JetBrains bills per user.' },
			{ question: 'Can I migrate from YouTrack to Kuayle?', answer: 'There is no built-in importer. Export to CSV or JSON and create issues through Kuayle’s documented HTTP API.' }
		]
	},

	// ---------------------------------------------------------------- OpenProject
	{
		competitor: 'openproject',
		title: 'OpenProject Alternative: Open Source, Self-Hosted Issue Tracker (2026) | Kuayle',
		description:
			'Compare Kuayle and OpenProject, two open source, self-hosted tools: licence, Community vs Enterprise features, per-user pricing, SSO, agile focus and Git integrations.',
		h1: 'An open source OpenProject alternative for software teams',
		intro:
			'OpenProject is a broad, GPL-3.0 project-management suite with Gantt charts and classic planning; several features are Enterprise add-ons. Kuayle is a narrower, keyboard-first issue tracker for software teams, under Apache 2.0, with no paid edition.',
		difference: 'OpenProject is a full PM suite with paid add-ons; Kuayle is a focused issue tracker with nothing gated.',
		whyLeave: [
			{ heading: 'Enterprise features are paid add-ons', body: 'SAML and OIDC SSO are Enterprise add-ons, and SCIM is limited to the Corporate plan.' },
			{ heading: 'Minimum seat counts', body: 'Enterprise plans start at 25 users (100 for Premium), priced per user in EUR.' },
			{ heading: 'AI needs a higher plan', body: 'The MCP server is an Enterprise add-on from Professional upward.' },
			{ heading: 'Built for classic project management', body: 'Work packages, Gantt and BCF suit PMO planning more than a fast engineering workflow.' }
		],
		rows: [
			{ dimension: 'Licence', kuayle: 'license', mark: 'yes', text: 'GPL-3.0 Community edition; Enterprise features are add-ons.' },
			{ dimension: 'Hosting', kuayle: 'hosting', mark: 'yes', text: 'Self-managed Community, or Enterprise on-premises or in an EU cloud.' },
			{ dimension: 'Pricing', kuayle: 'pricing', mark: 'partial', text: 'Community free; Enterprise from €5.95 per user a month, minimum 25 users.' },
			{ dimension: 'SSO and SCIM', kuayle: 'sso', mark: 'partial', text: 'LDAP in Community; SAML and OIDC as Enterprise add-ons; SCIM on Corporate.' },
			{ dimension: 'AI and agents', kuayle: 'ai', mark: 'partial', text: 'AI through the MCP server on Professional and higher.' },
			{ dimension: 'MCP server', kuayle: 'mcp', mark: 'partial', text: 'Official MCP server, an Enterprise add-on.' },
			{ dimension: 'API and webhooks', kuayle: 'api', mark: 'yes', text: 'REST API v3 with an OpenAPI spec, plus BCF and SCIM APIs.' },
			{ dimension: 'Importers', kuayle: 'importers', mark: 'partial', text: 'Jira Server/Data Center via the Jira Migrator (beta; not Jira Cloud).' },
			{ dimension: 'Git integrations', kuayle: 'gitForges', mark: 'partial', text: 'GitHub; GitLab via a community plugin.' },
			{ dimension: 'Planning', kuayle: 'planning', mark: 'yes', text: 'Gantt charts, roadmaps, agile boards, time and cost tracking.' }
		],
		costPlans: { entry: null, business: null },
		costNote:
			'OpenProject publishes Enterprise prices in EUR only (Basic €5.95, Professional €10.95, Premium €15.95 per user a month, minimum 25 users), checked 6 November 2026, so no USD column is shown. Community is free. Kuayle has no licence fee.',
		migration: SELF_HOSTED_MIGRATION('OpenProject', 'Export work packages to CSV, XLS or PDF, or read them through the REST API v3.'),
		stayIf: [
			'You need Gantt-first planning, time and cost tracking or BIM/BCF.',
			'LDAP sign-in in the Community edition covers your identity needs.',
			'Your organisation runs classic, waterfall or hybrid projects.'
		],
		switchIf: [
			'You want a fast, keyboard-first tracker for software teams.',
			'You prefer Apache 2.0 to GPL-3.0 and want no Enterprise add-ons.',
			'GitHub branches, commits and pull requests should move issues automatically.'
		],
		others: ['plane', 'taiga', 'jira', 'youtrack', 'itsaplan', 'gitlab'],
		faq: [
			{ question: 'Is OpenProject free?', answer: 'The Community edition is free and open source under GPL-3.0. SSO, SCIM, the MCP server and other features are paid Enterprise add-ons.' },
			{ question: 'What is a lighter OpenProject alternative?', answer: 'Kuayle is a lighter, issue-first tracker for software teams, self-hosted under Apache 2.0, with cycles, projects and GitHub integration.' },
			{ question: 'Does Kuayle have Gantt charts like OpenProject?', answer: 'Kuayle has a project Gantt view, cycles with burndown and velocity, and analytics. It has no time tracking or cost tracking.' },
			{ question: 'Can I move from OpenProject to Kuayle?', answer: 'There is no built-in importer. Export work packages to CSV or read the REST API, then create issues through Kuayle’s documented HTTP API.' }
		]
	},

	// ---------------------------------------------------------------- Taiga
	{
		competitor: 'taiga',
		title: 'Open Source Taiga Alternative, Self-Hosted (2026) | Kuayle',
		description:
			'Compare Kuayle and Taiga, two open source, self-hostable agile trackers: licence, hosted plans, SSO, integrations, planning features and migration.',
		h1: 'An open source Taiga alternative',
		intro:
			'Taiga is a long-standing MPL-2.0 Scrum and Kanban tool with hosted plans. Kuayle is an Apache 2.0 issue tracker built around cycles, projects and GitHub, which you host yourself.',
		difference: 'Taiga centres on Scrum and Kanban; Kuayle on keyboard-first issues, cycles and GitHub automation.',
		whyLeave: [
			{ heading: 'Tight storage on hosted plans', body: 'Taiga’s hosted plans cap storage (10MB free, 500MB Basic, 3GB Premium).' },
			{ heading: 'No SAML SSO or SCIM documented', body: 'Authentication plugins cover GitHub and GitLab; there is no documented SAML or SCIM.' },
			{ heading: 'Old Jira importer', body: 'The Jira importer supports Jira up to 8.3.5, not Jira Cloud.' },
			{ heading: 'EUR-only pricing', body: 'Hosted prices are published in EUR only.' }
		],
		rows: [
			{ dimension: 'Licence', kuayle: 'license', mark: 'yes', text: 'MPL-2.0.' },
			{ dimension: 'Hosting', kuayle: 'hosting', mark: 'yes', text: 'Hosted SaaS, private cloud, or self-hosted.' },
			{ dimension: 'Pricing', kuayle: 'pricing', mark: 'yes', text: 'Flat per account with unlimited users: €50, €200 or €600 a year.' },
			{ dimension: 'SSO and SCIM', kuayle: 'sso', mark: 'partial', text: 'GitHub and GitLab sign-in plugins; community LDAP plugins; no SAML or SCIM documented.' },
			{ dimension: 'API and webhooks', kuayle: 'api', mark: 'yes', text: 'REST API with webhooks.' },
			{ dimension: 'Importers', kuayle: 'importers', mark: 'yes', text: 'Jira (up to 8.3.5), GitHub and Trello.' },
			{ dimension: 'Git integrations', kuayle: 'gitForges', mark: 'yes', text: 'GitHub, GitLab, Bitbucket and Gogs.' },
			{ dimension: 'Planning', kuayle: 'planning', mark: 'yes', text: 'Scrum backlogs, sprints, burndown and Kanban.' },
			{ dimension: 'Issue model', kuayle: 'issues', mark: 'yes', text: 'User stories, tasks, issues and epics.' }
		],
		costPlans: { entry: null, business: null },
		costNote:
			'Taiga prices hosted plans flat per account in EUR (Enthusiast €50, Basic €200, Premium €600 a year, unlimited users), checked 6 November 2026, so no per-seat columns apply. Self-hosting is free. Kuayle has no licence fee.',
		migration: SELF_HOSTED_MIGRATION('Taiga', 'Export each project as a JSON dump from project settings or through the exporter API.'),
		stayIf: [
			'Your team runs strict Scrum with user stories and story points.',
			'A flat-priced hosted plan suits you better than running a server.',
			'You use Taiga’s Bitbucket or Gogs integrations.'
		],
		switchIf: [
			'You want a keyboard-first tracker with a command palette and shortcuts.',
			'GitHub pull requests should update issue status automatically.',
			'You need private teams, multiple assignees and edit history.'
		],
		others: ['openproject', 'plane', 'itsaplan', 'gitlab', 'trello', 'linear'],
		faq: [
			{ question: 'Is Taiga still maintained?', answer: 'Yes. Taiga’s backend repository received commits in November 2026, and taiga.io still sells hosted plans.' },
			{ question: 'What is a good Taiga alternative?', answer: 'Kuayle is an open source, self-hosted tracker with cycles, projects and GitHub automation. OpenProject and Plane are other open source options.' },
			{ question: 'Can I migrate from Taiga to Kuayle?', answer: 'There is no Taiga importer. Export the project JSON or use Taiga’s REST API, then create issues through Kuayle’s documented HTTP API.' },
			{ question: 'Does Kuayle support Scrum?', answer: 'Kuayle uses cycles (time-boxed iterations) with burndown, velocity and carry-over. It has no story points or estimates.' }
		]
	},

	// ---------------------------------------------------------------- GitLab
	{
		competitor: 'gitlab',
		title: 'GitLab Issues Alternative: Open Source, Self-Hosted Tracker (2026) | Kuayle',
		description:
			'Compare Kuayle with GitLab issues and boards: a focused, free, self-hosted issue tracker versus a full DevOps platform with per-user Premium features.',
		h1: 'A self-hosted GitLab issues alternative for focused teams',
		intro:
			'GitLab puts issues next to Git hosting and CI/CD, and many self-hosting teams already run it. Kuayle is a dedicated issue tracker under Apache 2.0, with cycles, projects and GitHub automation, and no per-user tier for planning features.',
		difference: 'GitLab is a full DevOps platform with planning features in Premium; Kuayle is a focused tracker with every feature free.',
		whyLeave: [
			{ heading: 'Planning features sit in Premium', body: 'Multiple group boards, assignee and iteration lists and WIP limits on boards need Premium or Ultimate.' },
			{ heading: 'Premium is priced per user', body: 'GitLab Premium is $29 per user a month billed yearly; Ultimate is by quote.' },
			{ heading: 'GitLab.com Free is small', body: 'GitLab.com Free allows 5 users per top-level private group.' },
			{ heading: 'A lot to run for a tracker', body: 'Self-managing GitLab means operating a full DevOps platform, not just an issue tracker.' }
		],
		rows: [
			{ dimension: 'Licence', kuayle: 'license', mark: 'partial', text: 'MIT core; Enterprise Edition code under a separate licence.' },
			{ dimension: 'Hosting', kuayle: 'hosting', mark: 'yes', text: 'GitLab.com, Self-Managed or Dedicated.' },
			{ dimension: 'Pricing', kuayle: 'pricing', mark: 'partial', text: 'Free tier; Premium $29 per user a month billed yearly; Ultimate by quote.' },
			{ dimension: 'SSO and SCIM', kuayle: 'sso', mark: 'yes', text: 'Instance-wide SAML on Self-Managed Free; group SAML on GitLab.com needs Premium.' },
			{ dimension: 'AI and agents', kuayle: 'ai', mark: 'yes', text: 'GitLab Duo, billed through GitLab Credits.' },
			{ dimension: 'MCP server', kuayle: 'mcp', mark: 'yes', text: 'Official MCP server on all tiers, including Self-Managed.' },
			{ dimension: 'API and webhooks', kuayle: 'api', mark: 'yes', text: 'REST and GraphQL APIs with webhooks.' },
			{ dimension: 'Importers', kuayle: 'importers', mark: 'yes', text: 'GitHub, Bitbucket, Gitea, Jira (issues only) and other GitLab instances.' },
			{ dimension: 'Git integrations', kuayle: 'gitForges', mark: 'yes', text: 'Built-in Git hosting, merge requests and CI/CD.' },
			{ dimension: 'Planning', kuayle: 'planning', mark: 'partial', text: 'Issues, boards and milestones on Free; richer boards, iterations and epics in paid tiers.' }
		],
		costPlans: { entry: 'Premium', business: null },
		costNote:
			'GitLab Premium list price billed yearly ($29 per user a month), checked 6 November 2026, before tax. Premium covers the whole DevOps platform, not only issues. Kuayle has no licence fee.',
		migration: SELF_HOSTED_MIGRATION('GitLab', 'Export issues to CSV, export a whole project as .tar.gz (NDJSON), or read issues through the REST or GraphQL API.'),
		stayIf: [
			'Your code, merge requests and CI/CD already live in GitLab and the built-in issues are enough.',
			'You need epics, iterations or SAML across groups and already pay for Premium.',
			'You use GitLab Duo, or its MCP server for merge requests.'
		],
		switchIf: [
			'You want a dedicated, keyboard-first tracker rather than issues inside a DevOps platform.',
			'Your code is on GitHub and you want pull requests to move issues.',
			'Cycles with burndown and velocity, private teams and multiple assignees should be free.'
		],
		others: ['jira', 'youtrack', 'openproject', 'plane', 'taiga', 'itsaplan'],
		faq: [
			{ question: 'Can GitLab be used just as an issue tracker?', answer: 'Yes, but it is a full DevOps platform, and several board and planning features need Premium. Kuayle is a dedicated tracker with no paid features.' },
			{ question: 'Does Kuayle integrate with GitLab?', answer: 'Not yet. Kuayle integrates with GitHub through a GitHub App. GitLab, Bitbucket and Gitea are not supported today.' },
			{ question: 'Is GitLab open source?', answer: 'GitLab’s core is MIT-licensed; Enterprise Edition code is under a separate licence.' },
			{ question: 'Can I move GitLab issues to Kuayle?', answer: 'There is no GitLab importer. Export issues to CSV or read the GitLab API, then create issues through Kuayle’s documented HTTP API.' }
		]
	},

	// ---------------------------------------------------------------- Trello
	{
		competitor: 'trello',
		title: 'Open Source Trello Alternative for Software Teams, Self-Hosted (2026) | Kuayle',
		description:
			'Outgrowing Trello boards? Compare Kuayle, a free, self-hosted, open source issue tracker, with Trello on pricing, limits, SSO, exports and developer workflow.',
		h1: 'The open source, self-hosted Trello alternative for software teams',
		intro:
			'Trello is a simple, hosted board tool. When a software team needs real issues, cycles, projects and GitHub linking, it often outgrows cards. Kuayle is a self-hosted tracker under Apache 2.0 with board and list layouts, and no per-seat price.',
		difference: 'Trello is hosted boards and cards; Kuayle is a full issue tracker with boards, on your own server.',
		whyLeave: [
			{ heading: 'Free plan limits', body: 'Trello Free allows 10 collaborators and 10 boards per Workspace.' },
			{ heading: 'Exports need Premium', body: 'Board CSV export and full Workspace export require Premium.' },
			{ heading: 'SSO is extra', body: 'SAML SSO needs an Atlassian Guard subscription from $4 per user a month, unless you are on Enterprise.' },
			{ heading: 'Cloud only', body: 'Trello is offered only as Atlassian-hosted software.' }
		],
		rows: [
			{ dimension: 'Licence', kuayle: 'license', mark: 'no', text: 'Proprietary.' },
			{ dimension: 'Hosting', kuayle: 'hosting', mark: 'no', text: 'Atlassian’s cloud only.' },
			{ dimension: 'Pricing', kuayle: 'pricing', mark: 'partial', text: 'Free for 10 collaborators; Standard $5 and Premium $10 per user a month billed yearly.' },
			{ dimension: 'SSO and SCIM', kuayle: 'sso', mark: 'partial', text: 'SAML through Atlassian Guard (extra), included on Enterprise.' },
			{ dimension: 'AI and agents', kuayle: 'ai', mark: 'partial', text: 'AI quick capture and summaries from Standard; AI on boards from Premium.' },
			{ dimension: 'MCP server', kuayle: 'mcp', mark: 'yes', text: 'Official Trello MCP server on all plans.' },
			{ dimension: 'API and webhooks', kuayle: 'api', mark: 'yes', text: 'REST API with webhooks.' },
			{ dimension: 'Git integrations', kuayle: 'gitForges', mark: 'partial', text: 'GitHub Power-Up links branches, commits and pull requests to cards.' },
			{ dimension: 'Issue model', kuayle: 'issues', mark: 'partial', text: 'Cards with checklists, labels, due dates and members.' },
			{ dimension: 'Planning', kuayle: 'planning', mark: 'partial', text: 'Boards, plus timeline, calendar and dashboard views on Premium.' }
		],
		costPlans: { entry: 'Standard', business: 'Premium' },
		costNote:
			'Trello list prices billed yearly, checked 6 November 2026, before Atlassian Guard and tax. Kuayle has no licence fee.',
		migration: SELF_HOSTED_MIGRATION('Trello', 'Export each board as JSON on any plan (CSV needs Premium), or read cards through the Trello REST API.'),
		stayIf: [
			'Simple boards and cards are all your team needs.',
			'You want a hosted tool with nothing to operate.',
			'Non-technical teams share the same boards.'
		],
		switchIf: [
			'You need issues with sub-issues, relations, cycles and projects, not only cards.',
			'GitHub pull requests should move issues automatically.',
			'Data must stay on your own servers, with no collaborator cap.'
		],
		others: ['asana', 'clickup', 'taiga', 'plane', 'linear', 'jira'],
		faq: [
			{ question: 'Is there an open source Trello alternative?', answer: 'Yes. Kuayle is an open source, self-hosted issue tracker with board and list layouts. WeKan and Kanboard are open source board tools closer to Trello itself.' },
			{ question: 'Can Trello be self-hosted?', answer: 'No. Trello is Atlassian-hosted only. Kuayle runs on your own server with Docker Compose.' },
			{ question: 'Is Kuayle as simple as Trello?', answer: 'Kuayle has board and list layouts, but it is an issue tracker with teams, cycles and projects, so it has more structure than Trello.' },
			{ question: 'Can I import Trello boards into Kuayle?', answer: 'There is no Trello importer yet. Export boards to JSON and create issues through Kuayle’s documented HTTP API.' }
		]
	},

	// ---------------------------------------------------------------- It's a Plan
	{
		competitor: 'itsaplan',
		title: 'It’s a Plan Alternative: Open Source, Apache 2.0, No Paid Tier (2026) | Kuayle',
		description:
			'Compare Kuayle and It’s a Plan, two self-hosted open source issue trackers: AGPL vs Apache 2.0, Community vs Pro, SSO, AI agents, MCP and integrations.',
		h1: 'An It’s a Plan alternative with no paid tier',
		intro:
			'It’s a Plan and Kuayle are both self-hosted issue trackers with no per-seat price. It’s a Plan is AGPL-3.0 with a $240-a-year Pro edition. Kuayle is Apache 2.0, with no Pro edition and a smaller feature set. Both let you assign issues to AI agents.',
		difference: 'It’s a Plan has more features, including SSO and more importers, with some behind Pro; Kuayle has no paid edition and a permissive licence.',
		whyLeave: [
			{ heading: 'Some features need Pro', body: 'Multiple workspaces, per-workspace SSO and SCIM, the audit log and event-triggered agents are in the $240-a-year Pro edition.' },
			{ heading: 'AGPL-3.0 terms', body: 'It’s a Plan is AGPL-3.0, with a commercial licence also offered; some organisations prefer permissive licences.' },
			{ heading: 'No documented full export', body: 'Neither its site nor its repository documents a full data export.' }
		],
		rows: [
			{ dimension: 'Licence', kuayle: 'license', mark: 'yes', text: 'AGPL-3.0 (the runner package is Apache 2.0); a commercial licence is offered.' },
			{ dimension: 'Hosting', kuayle: 'hosting', mark: 'yes', text: 'Self-hosted: Railway, Docker Compose, Coolify or Kubernetes.' },
			{ dimension: 'Pricing', kuayle: 'pricing', mark: 'yes', text: 'Community free; Pro $240 a year flat per domain, no seat price.' },
			{ dimension: 'SSO and SCIM', kuayle: 'sso', mark: 'yes', text: 'Instance-wide OIDC and SCIM, Google sign-in and passkeys in Community; per-workspace SSO in Pro.' },
			{ dimension: 'AI and agents', kuayle: 'ai', mark: 'yes', text: 'AI agents as project members on your own model key or coding CLI; workflows and budgets in Pro.' },
			{ dimension: 'MCP server', kuayle: 'mcp', mark: 'yes', text: 'Built-in MCP server.' },
			{ dimension: 'API and webhooks', kuayle: 'api', mark: 'yes', text: 'REST API with OpenAPI reference and signed webhooks.' },
			{ dimension: 'Importers', kuayle: 'importers', mark: 'yes', text: 'Linear, Plane and Trello, plus an import API.' },
			{ dimension: 'Git integrations', kuayle: 'gitForges', mark: 'yes', text: 'GitHub, GitLab, Gitea, Forgejo and Bitbucket pull requests.' },
			{ dimension: 'Teams and access', kuayle: 'access', mark: 'partial', text: 'Project roles in Community; multiple workspaces need Pro.' }
		],
		costPlans: { entry: null, business: null },
		costNote:
			'It’s a Plan Community is free; Pro is $240 a year flat per domain with any number of people, checked 6 November 2026, so per-seat columns do not apply. Kuayle has no licence fee and no paid edition.',
		migration: SELF_HOSTED_MIGRATION('It’s a Plan', 'Read your projects and issues through its REST API; a full export is not documented.'),
		stayIf: [
			'You want OIDC sign-in or SCIM today.',
			'You need importers from Linear, Plane or Trello.',
			'Pull-request linking with GitLab, Gitea, Forgejo or Bitbucket matters to you.'
		],
		switchIf: [
			'You want every feature in one edition, with no Pro key.',
			'You prefer Apache 2.0 to AGPL-3.0.',
			'Multiple workspaces, multiple assignees and private teams should be free.',
			'You want self-hosted coding environments (Dev Machines) linked to issues.'
		],
		others: ['plane', 'linear', 'openproject', 'taiga', 'jira', 'gitlab'],
		faq: [
			{ question: 'What is the difference between Kuayle and It’s a Plan?', answer: 'Both are self-hosted with no per-seat price. It’s a Plan is AGPL-3.0 with a $240-a-year Pro edition and more features such as SSO and importers. Kuayle is Apache 2.0 with no paid edition.' },
			{ question: 'Is It’s a Plan free?', answer: 'Its Community edition is free for any number of people. Pro costs $240 a year per domain.' },
			{ question: 'Does Kuayle have AI agents like It’s a Plan?', answer: 'Yes. In Kuayle an agent can be assigned an issue, alone or beside people, and @mentioned in a comment; assigning starts a coding task on an opt-in, self-hosted Dev Machine and the issue shows its status. Admins choose the teams each agent works in, and an agent acts with the permissions of the person who assigned it. Automations and workflows can also start agents on a label or status, and external agents reach issues through Kuayle’s MCP server.' },
			{ question: 'Which licence is more permissive?', answer: 'Apache 2.0, which Kuayle uses, is permissive. AGPL-3.0, which It’s a Plan uses, requires sharing source for modified versions offered over a network.' }
		]
	}
];
