/**
 * Competitor inventory. Generated from first-party research checked on 2026-11-06,
 * then reviewed by hand. Every external fact must be backed by an entry in `sources`;
 * unverifiable fields stay null. Re-check sources before changing reviewed dates.
 */
import type { Competitor } from './competitor-types.ts';

export { alternativeSlug, alternativePath, annualLicenseCost, COMPETITORS_CHECKED_AT } from './competitor-types.ts';

export const competitors: Competitor[] = [
	{
		"slug": "linear",
		"name": "Linear",
		"website": "https://linear.app",
		"status": "active",
		"statusNote": null,
		"category": "hosted",
		"tier": "A",
		"tierJustification": "The most common hosted tracker teams compare a fast, keyboard-first tool against.",
		"license": "Proprietary",
		"hosting": "cloud",
		"hostingNote": "Hosted SaaS; workspace data stored in the EU or the US, chosen when the workspace is created; no self-hosting option is documented.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Free: unlimited members, 2 teams, 250 issues, 10MB file uploads.",
			"entryPlan": "Basic",
			"monthlyPerSeatAnnualBillingUsd": 10,
			"businessPlan": "Business",
			"businessMonthlyPerSeatAnnualBillingUsd": 16,
			"notes": "Basic and Business are shown billed yearly only; Basic allows 5 teams; Enterprise is custom-priced with annual billing only."
		},
		"sso": "Google SSO on all plans; SAML SSO and SCIM 2.0 (SCIM requires SAML) are Enterprise-only.",
		"ai": "Linear Agent, agent platform and MCP access are on every plan; Triage and Code Intelligence are Business+; cloud coding agents and Loops draw on opt-in prepaid AI credits (min $10 top-up, tokens at provider rates plus sandbox runtime).",
		"mcp": "yes: official remote server at https://mcp.linear.app/mcp (Streamable HTTP, OAuth 2.1 or API key, read-only endpoint available), included on all plans.",
		"api": "GraphQL API plus webhooks (admin-created) for issues, comments, projects, cycles and more; available on all plans.",
		"importers": "Jira, GitHub Issues, Asana, Shortcut, and Linear-to-Linear, plus a CLI importer.",
		"gitForges": "GitHub (PR/commit linking, two-way issue sync) and GitLab; GitHub Enterprise Server via a separate Enterprise-plan integration; Bitbucket not listed.",
		"exports": "CSV exports of workspace issues (admins, owners on Enterprise), views, projects and initiatives; Markdown copy, PDF print, and the API.",
		"sources": [
			{
				"label": "Linear pricing",
				"url": "https://linear.app/pricing",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Linear SAML and access control docs",
				"url": "https://linear.app/docs/saml-and-access-control",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Linear MCP docs",
				"url": "https://linear.app/docs/mcp",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Linear AI credits docs",
				"url": "https://linear.app/docs/ai-credits",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Linear import docs",
				"url": "https://linear.app/docs/import-issues",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Linear API and webhooks docs",
				"url": "https://linear.app/docs/api-and-webhooks",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Linear exporting data docs",
				"url": "https://linear.app/docs/exporting-data",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Linear GitHub integration docs",
				"url": "https://linear.app/docs/github",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Linear integrations",
				"url": "https://linear.app/integrations",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Linear security",
				"url": "https://linear.app/security",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "jira",
		"name": "Jira",
		"website": "https://www.atlassian.com/software/jira",
		"status": "active",
		"statusNote": "Jira Cloud is active; Jira Software and Jira Service Management Data Center stopped selling to new customers on 2026-03-30 and reach end of life on 2029-03-28.",
		"category": "hosted",
		"tier": "A",
		"tierJustification": "The default enterprise tracker; Data Center end of life pushes self-hosted teams to look elsewhere.",
		"license": "Proprietary",
		"hosting": "cloud",
		"hostingNote": "Cloud for new customers; self-managed Data Center is closed to new customers since 2026-03-30 and ends 2029-03-28 for existing ones.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Free for up to 10 users, 2 GB storage, 150 automation steps/month, community support; no Rovo AI.",
			"entryPlan": "Standard",
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": "Premium",
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"monthlyBillingPerSeatUsd": 8.48,
			"businessMonthlyBillingPerSeatUsd": 15.56,
			"notes": "Published per-user prices are for monthly billing: Standard $8.48, Premium $15.56; annual subscriptions are billed by user tier via a calculator; Enterprise is annual-only, by quote."
		},
		"sso": "SSO, SCIM and AD Sync need a separate Atlassian Guard Standard subscription on Free, Standard and Premium; Enterprise includes Guard Standard and multiple IdPs.",
		"ai": "Rovo Search, Chat and Agents on all paid plans with pooled monthly credits (25/70/150 per user on Standard/Premium/Enterprise); Atlassian says it does not currently charge for usage beyond the allowance.",
		"mcp": "yes: official Atlassian Rovo MCP Server at https://mcp.atlassian.com/v2/mcp (OAuth 2.1 or admin-enabled API token) covering Jira, Confluence, JSM, Bitbucket Cloud and more; some tools consume Rovo credits.",
		"api": "REST API (v2/v3) and webhooks.",
		"importers": "Asana, monday, ClickUp, Trello, Azure DevOps, GitHub, GitLab, Smartsheet, YouTrack, Notion, Linear, Wrike, Airtable, and CSV.",
		"gitForges": "Bitbucket Cloud, GitHub Cloud, GitHub Enterprise Server, GitLab (via Marketplace app) and Azure DevOps, plus Jenkins and Bamboo.",
		"exports": "CSV export of work items (all or selected fields, UTF-8) from list view or search; attachments only as URLs; plus the REST API.",
		"sources": [
			{
				"label": "Jira pricing",
				"url": "https://www.atlassian.com/software/jira/pricing",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Explore Jira Cloud plans",
				"url": "https://support.atlassian.com/jira-cloud-administration/docs/explore-jira-cloud-plans/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Atlassian Data Center End of Life",
				"url": "https://www.atlassian.com/licensing/data-center-end-of-life",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Ascend to the cloud (Atlassian blog)",
				"url": "https://www.atlassian.com/blog/company-news/atlassian-ascend",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Atlassian Rovo MCP overview",
				"url": "https://developer.atlassian.com/cloud/rovo-mcp/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Jira Cloud webhooks",
				"url": "https://developer.atlassian.com/cloud/jira/platform/webhooks/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Import data into Jira",
				"url": "https://support.atlassian.com/jira-software-cloud/docs/import-data-into-jira/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Integrate Jira with development tools",
				"url": "https://support.atlassian.com/jira-cloud-administration/docs/integrate-with-development-tools/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Export issues from Jira Cloud in CSV format",
				"url": "https://support.atlassian.com/jira/kb/how-to-export-issues-from-jira-cloud-in-csv-format/",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "plane",
		"name": "Plane",
		"displayName": "Plane.so",
		"website": "https://plane.so",
		"status": "active",
		"statusNote": null,
		"category": "open-source",
		"tier": "A",
		"tierJustification": "The closest open-source, self-hostable tracker in scope.",
		"license": "AGPL-3.0",
		"hosting": "both",
		"hostingNote": "Community Edition is AGPL-3.0 self-hosted and matches the Cloud Free tier; the Commercial Edition (closed-source) is needed for paid plans when self-hosting.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Free plan up to 12 users; the Commercial Edition free tier also gives 12 free seats per workspace.",
			"entryPlan": "Pro",
			"monthlyPerSeatAnnualBillingUsd": 6,
			"businessPlan": "Business",
			"businessMonthlyPerSeatAnnualBillingUsd": 13,
			"notes": "Monthly billing is $8 (Pro) and $15 (Business) per user; Enterprise Grid is custom pricing; Community users must switch to the Commercial Edition before upgrading."
		},
		"sso": "SAML and OIDC not on Free; Pro gets them only when self-hosted, Business/Enterprise on cloud and self-hosted; LDAP and group sync are Enterprise only; SCIM not listed on the pricing page.",
		"ai": "AI credits (500/seat/month on Pro, 1,000 on Business) and AI agents are cloud-only and included in paid plans.",
		"mcp": "yes: official MCP server (hosted OAuth or PAT at mcp.plane.so, or local stdio for self-hosted; OAuth transport not usable against Community Edition).",
		"api": "REST API (60 requests/min per key) plus workspace-level webhooks (v2).",
		"importers": "Jira, Linear, Asana, ClickUp and CSV on all plans; Confluence and Notion on Business/Enterprise.",
		"gitForges": "GitHub and GitLab (including Enterprise variants) from Pro upward; not on Free.",
		"exports": "Workspace admins can export projects as CSV, Excel or JSON (ZIP, 7-day download); filtered custom exports require Pro.",
		"sources": [
			{
				"label": "Plane pricing",
				"url": "https://plane.so/pricing",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Plane licence (GitHub)",
				"url": "https://raw.githubusercontent.com/makeplane/plane/preview/LICENSE.txt",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Plane editions and versions",
				"url": "https://developers.plane.so/self-hosting/editions-and-versions",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Plane API reference",
				"url": "https://developers.plane.so/api-reference/introduction",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Plane webhooks",
				"url": "https://developers.plane.so/dev-tools/intro-webhooks",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Plane MCP server",
				"url": "https://developers.plane.so/dev-tools/mcp-server",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Plane export docs",
				"url": "https://docs.plane.so/core-concepts/export",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "asana",
		"name": "Asana",
		"website": "https://asana.com",
		"status": "active",
		"statusNote": null,
		"category": "hosted",
		"tier": "A",
		"tierJustification": "A widely used work manager that software teams outgrow or want to self-host.",
		"license": "Proprietary",
		"hosting": "cloud",
		"hostingNote": "Hosted SaaS; data residency (region choice) is an Enterprise+ option.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Personal: free for up to 2 users, unlimited tasks and projects, 100MB per file; no timeline, Gantt or dashboards.",
			"entryPlan": "Starter",
			"monthlyPerSeatAnnualBillingUsd": 10.99,
			"businessPlan": "Advanced",
			"businessMonthlyPerSeatAnnualBillingUsd": 24.99,
			"notes": "Monthly billing is $13.49 (Starter) and $30.49 (Advanced); seats are sold in increments (by 1 up to 5, by 5 up to 30, by 10 up to 100, and so on); Enterprise and Enterprise+ are by quote."
		},
		"sso": "SAML SSO and SCIM provisioning are on Enterprise and Enterprise+; not on Personal, Starter or Advanced.",
		"ai": "AI Teammates and Asana Dash include 5 requests per user/month (capped at 50 per account on Starter/Advanced, 250 on Enterprise); extra requests are $0.50 prepaid or $0.60 pay-as-you-go; AI Studio includes 50K-200K credits per account/month.",
		"mcp": "yes: official V2 MCP server at https://mcp.asana.com/v2/mcp (Streamable HTTP, OAuth); legacy beta /sse endpoint is deprecated.",
		"api": "REST API with webhooks, workspace and audit log events, batch requests, SCIM and graph exports.",
		"importers": "Built-in CSV importer covering spreadsheets and exports from Excel, Smartsheet, Wrike, Monday.com, Airtable and Trello.",
		"gitForges": "GitHub (official app syncing PR status to tasks, no GitHub Enterprise Server); GitLab only via the partner-built Unito integration.",
		"exports": "Project data and reports export to CSV or PDF; full-organization JSON export is an Enterprise+ compliance feature; plus the REST API.",
		"sources": [
			{
				"label": "Asana pricing",
				"url": "https://asana.com/pricing",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Using Asana's MCP Server",
				"url": "https://developers.asana.com/docs/using-asanas-mcp-server",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Asana developer overview",
				"url": "https://developers.asana.com/docs/overview",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Asana CSV importer",
				"url": "https://asana.com/apps/csv-importer",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "GitHub + Asana",
				"url": "https://asana.com/apps/github",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "GitLab + Asana",
				"url": "https://asana.com/apps/gitlab",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "clickup",
		"name": "ClickUp",
		"website": "https://clickup.com",
		"status": "active",
		"statusNote": null,
		"category": "hosted",
		"tier": "A",
		"tierJustification": "A broad hosted work platform that teams often consolidate away from.",
		"license": "Proprietary",
		"hosting": "cloud",
		"hostingNote": "Hosted SaaS; US, EU and APAC data residency on Enterprise and MAX.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Free Forever: unlimited members and tasks, 60MB storage, 5 Spaces, 100 automation actions/month, 500 AI credits per workspace.",
			"entryPlan": "Core",
			"monthlyPerSeatAnnualBillingUsd": 9,
			"businessPlan": "Business",
			"businessMonthlyPerSeatAnnualBillingUsd": 17,
			"notes": "Page shows a 'regular price' of $12 (Core) and $25 (Business) and says yearly saves up to 33%; MAX is $100/user/month billed yearly; Enterprise is by quote; billing is per Workspace."
		},
		"sso": "Google SSO on Business and above; Microsoft, Okta and SAML SSO plus SCIM on Enterprise and MAX only.",
		"ai": "Brain² credits pooled per user/month (1K Core, 1.5K Business, 3K Enterprise, 10K MAX; 500 per workspace on Free); extra credits cost $10 per 10K.",
		"mcp": "yes: official remote server at https://mcp.clickup.com/mcp (OAuth only) on all plans; no deletion tools; rate limits by plan (100 calls/24h on Free Forever) unless the Everything AI add-on is bought.",
		"api": "REST API v2 (personal token or OAuth 2.0) with webhooks.",
		"importers": "Asana, Basecamp, Confluence, Jira, Monday.com, Todoist, Trello and Wrike.",
		"gitForges": "GitHub, GitLab and Bitbucket (commits, branches, pull requests).",
		"exports": "List view exports (trial on Core, included on Business and above) and Table and Form view exports (Business and above); plus the API.",
		"sources": [
			{
				"label": "ClickUp pricing",
				"url": "https://clickup.com/pricing",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "ClickUp integrations",
				"url": "https://clickup.com/integrations",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "ClickUp MCP server docs",
				"url": "https://developer.clickup.com/docs/connect-an-ai-assistant-to-clickups-mcp-server",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "ClickUp API authentication",
				"url": "https://developer.clickup.com/docs/authentication",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "ClickUp webhooks",
				"url": "https://developer.clickup.com/docs/webhooks",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "youtrack",
		"name": "YouTrack",
		"website": "https://www.jetbrains.com/youtrack/",
		"status": "active",
		"statusNote": null,
		"category": "hosted",
		"tier": "A",
		"tierJustification": "A developer-focused tracker with both cloud and self-hosted editions.",
		"license": "Proprietary",
		"hosting": "both",
		"hostingNote": "YouTrack Cloud and self-hosted YouTrack Server.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Free for up to 10 users and 3 helpdesk agents with no time limit (Cloud and Server).",
			"entryPlan": "YouTrack Cloud per-user subscription",
			"monthlyPerSeatAnnualBillingUsd": 4.5,
			"businessPlan": null,
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": "Since 2025-10-01: $4.50/user/month annual or $5.40 monthly for 11+ users, decreasing with volume; helpdesk agents beyond 3 cost $5.50 (annual) or $6.00 (monthly); Server sold as user packs (15-user pack €900/yr as shown to an EU visitor)."
		},
		"sso": "SAML 2.0 auth module with SCIM 2.0 provisioning documented for Cloud; plan gating not stated.",
		"ai": "AI Assistant (summaries, replies, text-to-issue) on Cloud and Server, currently free with weekly-refreshed credits; paid credits planned.",
		"mcp": "yes: official remote MCP server over Streamable HTTP at the instance /mcp endpoint with OAuth or permanent token (Cloud documentation).",
		"api": "REST API (JSON) with permanent-token auth.",
		"importers": "Jira, Jira Service Management, GitHub, GitLab, monday.com, Zendesk, ClickUp and Confluence, plus custom import scripts.",
		"gitForges": "GitHub, GitLab, Bitbucket, Azure Repos and Gitea/Gogs, including GitHub Enterprise, GitLab CE/EE and Bitbucket Server.",
		"exports": "Selected issues to XLSX or CSV (500 by default), or JSON via the REST API.",
		"sources": [
			{
				"label": "JetBrains sales: YouTrack Cloud per-user price changes",
				"url": "https://sales.jetbrains.com/hc/en-gb/articles/28382668077970-%D0%A1hanges-to-YouTrack-Cloud-per-user-subscription-prices",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "YouTrack buy page",
				"url": "https://www.jetbrains.com/youtrack/buy/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "YouTrack Cloud: Managing your licence",
				"url": "https://www.jetbrains.com/help/youtrack/cloud/switching-subscription-plans.html",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "YouTrack AI Assistant",
				"url": "https://www.jetbrains.com/youtrack/features/ai_assistant.html",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "YouTrack remote MCP server",
				"url": "https://www.jetbrains.com/help/youtrack/cloud/model-context-protocol-server.html",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "YouTrack Cloud imports",
				"url": "https://www.jetbrains.com/help/youtrack/cloud/imports.html",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "YouTrack VCS integrations",
				"url": "https://www.jetbrains.com/help/youtrack/server/integrate-with-version-control.html",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "YouTrack SAML 2.0 auth module",
				"url": "https://www.jetbrains.com/help/youtrack/cloud/saml-authentication-module.html",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "YouTrack export issue data",
				"url": "https://www.jetbrains.com/help/youtrack/server/export-issues-csv-excel-format.html",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "YouTrack REST API",
				"url": "https://www.jetbrains.com/help/youtrack/devportal/youtrack-rest-api.html",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "openproject",
		"name": "OpenProject",
		"website": "https://www.openproject.org",
		"status": "active",
		"statusNote": null,
		"category": "open-source",
		"tier": "A",
		"tierJustification": "A mature open-source project manager with a self-managed edition.",
		"license": "GPL-3.0",
		"hosting": "both",
		"hostingNote": "Community is self-managed; Enterprise is on-premises or cloud in an EU data centre; Git/SVN repository integration is on-premises only.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Community edition free (self-managed, best-effort community support).",
			"entryPlan": "Basic",
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": "Professional",
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": "Only EUR published: Basic €5.95, Professional €10.95, Premium €15.95 per user/month, Corporate on request; minimum 25 users (Premium 100, Corporate 250); 1-month term only for Cloud Basic at +€1/user."
		},
		"sso": "LDAP in Community; SAML and OIDC are Enterprise add-ons; SCIM only in the Corporate plan.",
		"ai": "AI via the MCP server (read since 17.2, create/update since 17.8), an Enterprise add-on on Professional and higher; no AI billing stated.",
		"mcp": "yes: official MCP server, Enterprise add-on on Professional, Premium and Corporate (cloud and on-premises).",
		"api": "REST API v3 (HATEOAS, OpenAPI spec), BCF 2.1 API and SCIM API.",
		"importers": "Jira Server/Data Center 10.x-11.x via the Jira Migrator (beta; Jira Cloud not supported).",
		"gitForges": "GitHub (maintained by OpenProject) and GitLab (community plugin); SVN/Git repositories on-premises only.",
		"exports": "Work packages as PDF, XLS or CSV (500 by default), single work packages as PDF or Atom, plus the API.",
		"sources": [
			{
				"label": "OpenProject pricing",
				"url": "https://www.openproject.org/pricing/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "OpenProject COPYRIGHT (GitHub)",
				"url": "https://raw.githubusercontent.com/opf/openproject/dev/COPYRIGHT",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "OpenProject Enterprise edition",
				"url": "https://www.openproject.org/enterprise-edition/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "OpenProject authentication docs",
				"url": "https://www.openproject.org/docs/system-admin-guide/authentication/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "OpenProject SAML docs",
				"url": "https://www.openproject.org/docs/system-admin-guide/authentication/saml/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "OpenProject OIDC docs",
				"url": "https://www.openproject.org/docs/system-admin-guide/authentication/openid-providers/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "OpenProject SCIM docs",
				"url": "https://www.openproject.org/docs/system-admin-guide/authentication/scim/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "OpenProject MCP server docs",
				"url": "https://www.openproject.org/docs/system-admin-guide/integrations/mcp-server/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "OpenProject 17.8 release",
				"url": "https://www.openproject.org/blog/openproject-17-8-release/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "OpenProject API docs",
				"url": "https://www.openproject.org/docs/api/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "OpenProject integrations",
				"url": "https://www.openproject.org/docs/system-admin-guide/integrations/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "OpenProject export docs",
				"url": "https://www.openproject.org/docs/user-guide/work-packages/exporting/",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "taiga",
		"name": "Taiga",
		"website": "https://taiga.io",
		"status": "active",
		"statusNote": "taigaio/taiga-back is not archived and had commits on 2026-11-03; taiga.io still sells hosted plans.",
		"category": "open-source",
		"tier": "A",
		"tierJustification": "A long-standing open-source agile tracker teams shortlist for self-hosting.",
		"license": "MPL-2.0",
		"hosting": "both",
		"hostingNote": "Hosted SaaS plans, a dedicated Taiga Private Cloud (quote), or self-hosted/on-premise.",
		"pricing": {
			"model": "flat",
			"freePlan": "1 public and 1 private project, 10MB storage, unlimited users, community support.",
			"entryPlan": "Enthusiast",
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": "Basic",
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": "EUR only, flat per account with unlimited users: Enthusiast €5/mo or €50/yr, Basic €20/mo or €200/yr, Premium €60/mo or €600/yr; Private Cloud and On-premise by quote."
		},
		"sso": "GitHub and GitLab authentication plugins; community LDAP/Kerberos plugins; no SAML or SCIM documented.",
		"ai": null,
		"mcp": null,
		"api": "REST API (/api/v1) with webhooks.",
		"importers": "Jira (up to 8.3.5, not Jira Cloud), GitHub and Trello; Asana per the Taiga community site.",
		"gitForges": "GitHub, GitLab, Bitbucket and Gogs.",
		"exports": "Project export as a JSON dump file from project settings or the exporter API.",
		"sources": [
			{
				"label": "Taiga website",
				"url": "https://taiga.io",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Taiga deployment and pricing options",
				"url": "https://taiga.io/deployment-pricing-options/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "taiga-back GitHub repository",
				"url": "https://github.com/kaleidos-ventures/taiga-back",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Taiga REST API",
				"url": "https://docs.taiga.io/api.html",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Taiga community: How to extend Taiga",
				"url": "https://community.taiga.io/t/how-to-extend-taiga/160",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Taiga community: Import/Export projects",
				"url": "https://community.taiga.io/t/import-export-taiga-projects/168",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "gitlab",
		"name": "GitLab",
		"website": "https://about.gitlab.com",
		"status": "active",
		"statusNote": null,
		"category": "open-source",
		"tier": "A",
		"tierJustification": "Many self-hosting teams already run GitLab issues and boards.",
		"license": "MIT core; EE under a separate licence",
		"hosting": "both",
		"hostingNote": "GitLab.com SaaS, GitLab Self-Managed (Linux package, Helm, Docker, Operator, source) and GitLab Dedicated.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Free: 5 users per top-level private group on GitLab.com, 10 GiB storage, 400 compute minutes/month; self-managed Free with your own storage and runners.",
			"entryPlan": "Premium",
			"monthlyPerSeatAnnualBillingUsd": 29,
			"businessPlan": "Ultimate",
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": "Premium is $29/user/month billed annually; Ultimate is custom pricing via sales."
		},
		"sso": "GitLab.com group SAML SSO requires Premium or Ultimate; instance-wide SAML on Self-Managed is available on Free; SCIM is documented for GitLab.com groups (tier not verified).",
		"ai": "GitLab Duo AI features billed through GitLab Credits ($1 each on demand); Premium and Ultimate include 12 and 24 credits per user/month as a limited-time promotion.",
		"mcp": "yes: official GitLab MCP server, GA since GitLab 19.5 on Free/Premium/Ultimate across GitLab.com, Self-Managed and Dedicated; HTTP at /api/v4/mcp or stdio via mcp-remote; includes work items and merge request toolsets.",
		"api": "REST API, GraphQL API and project/group webhooks.",
		"importers": "GitHub, Bitbucket Server, Bitbucket Cloud, Gitea, FogBugz, Git repo by URL/manifest, Jira (issues only), other GitLab instances via direct transfer.",
		"gitForges": "Built-in Git hosting; GitHub project integration (Premium/Ultimate) sends pipeline status to GitHub for externally hosted repos.",
		"exports": "Project file export (.tar.gz with NDJSON) including issues, MRs, labels, milestones and boards on all tiers; issue CSV export on all tiers; REST/GraphQL API.",
		"sources": [
			{
				"label": "GitLab pricing",
				"url": "https://about.gitlab.com/pricing/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "GitLab LICENSE file",
				"url": "https://gitlab.com/gitlab-org/gitlab/-/raw/master/LICENSE",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "GitLab SAML SSO for GitLab.com groups",
				"url": "https://docs.gitlab.com/user/group/saml_sso/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "GitLab SAML SSO for Self-Managed",
				"url": "https://docs.gitlab.com/integration/saml/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "GitLab MCP server",
				"url": "https://docs.gitlab.com/user/model_context_protocol/mcp_server/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "GitLab import sources",
				"url": "https://docs.gitlab.com/user/project/import/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "GitLab project import/export",
				"url": "https://docs.gitlab.com/user/project/settings/import_export/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "GitLab issue CSV export",
				"url": "https://docs.gitlab.com/user/project/issues/csv_export/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "GitLab issue boards",
				"url": "https://docs.gitlab.com/user/project/issue_board/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "GitLab install methods",
				"url": "https://docs.gitlab.com/install/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "GitLab API",
				"url": "https://docs.gitlab.com/api/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "GitLab GitHub integration",
				"url": "https://docs.gitlab.com/user/project/integrations/github/",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "trello",
		"name": "Trello",
		"website": "https://trello.com",
		"status": "active",
		"statusNote": null,
		"category": "hosted",
		"tier": "A",
		"tierJustification": "A popular board tool teams leave when they need issues, cycles and projects.",
		"license": "Proprietary",
		"hosting": "cloud",
		"hostingNote": "Atlassian SaaS; no self-managed edition is offered on the pricing page.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Free for up to 10 collaborators and 10 boards per Workspace, unlimited Power-Ups, 10MB per file, 250 command runs/month.",
			"entryPlan": "Standard",
			"monthlyPerSeatAnnualBillingUsd": 5,
			"businessPlan": "Premium",
			"businessMonthlyPerSeatAnnualBillingUsd": 10,
			"notes": "Monthly billing is $6 (Standard) and $12.50 (Premium); Enterprise is $17.50/user/month billed annually with no published seat minimum."
		},
		"sso": "SAML SSO via Atlassian Guard, a separate subscription from $4/user/month on non-Enterprise plans; Enterprise includes SSO and user provisioning with Guard.",
		"ai": "AI (quick capture, summarisation, organisation) included from Standard; AI on boards from Premium; no separate AI charge listed.",
		"mcp": "yes: official Trello MCP server at https://mcp.trello.com/v1 with OAuth 2.0, on all plans; covers boards, lists, cards, checklists, comments, search, Inbox and Planner.",
		"api": "Trello REST API with webhooks.",
		"importers": null,
		"gitForges": "GitHub Power-Up links branches, commits, issues and PRs to cards (GitHub Enterprise over https supported).",
		"exports": "Board JSON export on all plans (last 1,000 actions); board CSV and Workspace CSV/JSON export require Premium.",
		"sources": [
			{
				"label": "Trello pricing",
				"url": "https://trello.com/pricing",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Trello data export",
				"url": "https://support.atlassian.com/trello/docs/exporting-data-from-trello/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Trello REST API",
				"url": "https://developer.atlassian.com/cloud/trello/rest/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Trello MCP",
				"url": "https://support.atlassian.com/trello/docs/connect-trello-to-ai-assistants-with-trello-mcp/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Trello GitHub Power-Up",
				"url": "https://support.atlassian.com/trello/docs/using-the-github-power-up/",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "itsaplan",
		"name": "It's a Plan",
		"website": "https://itsaplan.dev",
		"status": "active",
		"statusNote": null,
		"category": "open-source",
		"tier": "A",
		"tierJustification": "A new open-source, self-hosted tracker that competes directly with Kuayle.",
		"license": "AGPL-3.0",
		"hosting": "self-hosted",
		"hostingNote": "Self-hosted only (Railway, Docker Compose, Coolify, Kubernetes/Helm); no hosted cloud is described; packages/runner is Apache-2.0 and a commercial licence is also offered.",
		"pricing": {
			"model": "flat",
			"freePlan": "Community edition is free with unlimited people and projects, one workspace per person.",
			"entryPlan": "Pro",
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": null,
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"flatAnnualUsd": 240,
			"notes": "Pro is $240/year flat per instance tied to one domain, no seat or workspace limits; without a valid key Pro runs as Community."
		},
		"sso": "Instance-wide OIDC and SCIM in Community (plus Google sign-in and passkeys); per-workspace SSO/SCIM, required SSO and email domains are Pro.",
		"ai": "AI agents as project members in Community using your own model provider API key or external CLI agents (e.g. Claude Code, Codex); agent workflows, analytics, event-triggered runs and AI budgets are Pro.",
		"mcp": "yes: built-in MCP server (200+ tools) using the caller's API key access.",
		"api": "REST API with OpenAPI reference and API keys; signed webhooks with retries and delivery log.",
		"importers": "Linear, Plane and Trello, plus an import API (per the product site).",
		"gitForges": "GitHub, GitLab, Gitea, Forgejo and Bitbucket pull-request linking with closing keywords.",
		"exports": null,
		"sources": [
			{
				"label": "It's a Plan website",
				"url": "https://itsaplan.dev",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "It's a Plan GitHub repository",
				"url": "https://github.com/croffasia/itsaplan",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "It's a Plan LICENSE",
				"url": "https://raw.githubusercontent.com/croffasia/itsaplan/main/LICENSE",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "github-projects",
		"name": "GitHub Issues & Projects",
		"website": "https://github.com",
		"status": "active",
		"statusNote": null,
		"category": "hosted",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "Proprietary",
		"hosting": "both",
		"hostingNote": "GitHub.com SaaS; self-hosted GitHub Enterprise Server is included only with the Enterprise plan.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Free includes Issues and Projects, unlimited public/private repos, 2,000 CI/CD minutes/month and 500MB Packages.",
			"entryPlan": "Team",
			"monthlyPerSeatAnnualBillingUsd": 4,
			"businessPlan": "Enterprise",
			"businessMonthlyPerSeatAnnualBillingUsd": 21,
			"notes": "Prices listed as USD per user/month with no separate annual rate shown; Enterprise has a 30-day free trial."
		},
		"sso": "SAML SSO, SCIM provisioning and Enterprise Managed Users only on Enterprise.",
		"ai": "GitHub Copilot is separate: Free, Pro $10, Pro+ $39, Max $100 per user/month with AI credits ($0.01 each); Enterprise plan includes Copilot access; Pro and above can assign work to the Copilot coding agent.",
		"mcp": "yes: official GitHub MCP server (MIT), hosted at https://api.githubcopilot.com/mcp/ or run locally; issues toolset by default, projects toolset opt-in.",
		"api": "REST and GraphQL APIs; Projects automatable via GraphQL API and GitHub Actions.",
		"importers": "GitHub Enterprise Importer migrates from Azure DevOps Cloud, Bitbucket Server/Data Center 5.14+ and GitLab; issue/work-item migration not confirmed on that page.",
		"gitForges": "Native GitHub repositories, pull requests and commits.",
		"exports": "Project views export as TSV; full data via REST/GraphQL API.",
		"sources": [
			{
				"label": "GitHub pricing",
				"url": "https://github.com/pricing",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "About Projects",
				"url": "https://docs.github.com/en/issues/planning-and-tracking-with-projects/learning-about-projects/about-projects",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Exporting project data",
				"url": "https://docs.github.com/en/issues/planning-and-tracking-with-projects/managing-your-project/exporting-your-projects-data",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "GitHub MCP server repo",
				"url": "https://github.com/github/github-mcp-server",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Copilot plans",
				"url": "https://github.com/features/copilot/plans",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "About GitHub Enterprise Importer",
				"url": "https://docs.github.com/en/migrations/using-github-enterprise-importer/understanding-github-enterprise-importer/about-github-enterprise-importer",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "shortcut",
		"name": "Shortcut",
		"displayName": "Shortcut.com",
		"website": "https://www.shortcut.com",
		"status": "active",
		"statusNote": null,
		"category": "hosted",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "Proprietary",
		"hosting": "cloud",
		"hostingNote": "FAQ states there is no on-premises or self-hosted version yet.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Free for up to 10 users, 1 team, 1 workspace, 1 workflow, 5GB storage.",
			"entryPlan": "Team",
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": "Business",
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": "Pricing page shows $8.50 (Team) and $12 (Business) per user/month under a Yearly/Monthly toggle without labelling the period; yearly saves up to 25%; no seat minimum stated; Enterprise is custom."
		},
		"sso": "SAML SSO (Okta, OneLogin, Google Workspace, Entra ID) is an add-on on paid plans and included in Enterprise; SCIM only via Okta; pricing table lists SSO/SCIM as Business add-ons.",
		"ai": "Pricing page lists AI Agent Collaboration; Korey is a separate AI agent product with no published price; agents such as Devin integrate via Stories.",
		"mcp": "yes: official hosted MCP server at https://mcp.shortcut.com/mcp with OAuth scopes; Stories, Epics, Iterations, Docs read/write; local @shortcut/mcp package also exists (its GitHub repo was archived Jul 2026).",
		"api": "REST API v3 and Webhook API v1.",
		"importers": "Automated importers for Jira and Trello; Pivotal Tracker import page linked from pricing; CSV for other tools.",
		"gitForges": "GitHub, GitLab and Bitbucket Cloud (commits, branches, PRs/MRs linked to Stories).",
		"exports": "CSV export of all Stories in a Workspace, or of Epics, Iterations, Labels and Spaces.",
		"sources": [
			{
				"label": "Shortcut pricing",
				"url": "https://www.shortcut.com/pricing",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Shortcut FAQ",
				"url": "https://www.shortcut.com/faq/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Shortcut SAML help",
				"url": "https://www.shortcut.com/help/admin/saml-sso-single-sign-on/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Shortcut MCP server help",
				"url": "https://www.shortcut.com/help/integrations/mcp-server/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Shortcut MCP server repo",
				"url": "https://github.com/useshortcut/mcp-server-shortcut",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Shortcut REST API",
				"url": "https://developer.shortcut.com/api/rest/v3",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Shortcut export data",
				"url": "https://www.shortcut.com/help/admin/export-data/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Shortcut integrations",
				"url": "https://www.shortcut.com/integrations/",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "monday-dev",
		"name": "monday dev",
		"website": "https://monday.com/dev",
		"status": "active",
		"statusNote": null,
		"category": "hosted",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "Proprietary",
		"hosting": "unknown",
		"hostingNote": null,
		"pricing": {
			"model": "per-seat",
			"freePlan": "none (14-day trial; the Free plan exists only on monday AI work platform)",
			"entryPlan": "Basic",
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": "Standard",
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": "Only EUR shown (price depends on billing country): Basic €9, Standard €12, Pro €20 per seat/month billed annually; seats sold in buckets starting at 3 then multiples of 5 (e.g. 4 users buy 5); yearly is 18% cheaper than monthly; Enterprise by quote with 12-month minimum term."
		},
		"sso": "SAML SSO (Okta, Entra ID, OneLogin, custom) and SCIM only on Enterprise; Google SSO on Pro and Enterprise.",
		"ai": "AI credits included in all monday dev tiers; AI Sidekick lite on Standard/Pro and plus on Enterprise.",
		"mcp": "yes: official hosted monday Platform MCP at https://mcp.monday.com/mcp with 60+ tools; auth via API token or OAuth; calls count toward the daily API limit.",
		"api": "GraphQL API.",
		"importers": "Excel/CSV, Trello (task name, label, dates only), Asana, Jira Server (Enterprise), Basecamp, Zendesk.",
		"gitForges": "GitHub two-way sync from Standard and Git UI app; GitLab workflow integration (merge request triggers) on monday dev.",
		"exports": "Board export to Excel (up to 100,000 items); admin full-account export as ZIP once per 24 hours.",
		"sources": [
			{
				"label": "monday dev pricing",
				"url": "https://monday.com/dev/pricing",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Plans and pricing for monday.com",
				"url": "https://support.monday.com/hc/en-us/articles/4405633151634-Plans-and-pricing-for-monday-com",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "monday SAML SSO",
				"url": "https://support.monday.com/hc/en-us/articles/360000460605-SAML-Single-Sign-on",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "monday SCIM provisioning",
				"url": "https://support.monday.com/hc/en-us/articles/360021861660-SCIM-Provisioning-Custom-Setup",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "monday Platform MCP",
				"url": "https://developer.monday.com/api-reference/docs/mondaycom-mcp",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "monday API basics",
				"url": "https://developer.monday.com/api-reference/docs/basics",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Import boards from Trello",
				"url": "https://support.monday.com/hc/en-us/articles/360000274165-How-to-import-boards-from-Trello",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Export to Excel",
				"url": "https://support.monday.com/hc/en-us/articles/26989749858578-Export-from-monday-to-Excel",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Export entire account",
				"url": "https://support.monday.com/hc/en-us/articles/360002543719-How-to-export-your-entire-account-s-data",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "monday dev GitLab integration",
				"url": "https://support.monday.com/hc/en-us/articles/27092615660050-monday-dev-GitLab-integration",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Git UI app",
				"url": "https://support.monday.com/hc/en-us/articles/6235770475282-Git-UI-app",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "zenhub",
		"name": "Zenhub",
		"website": "https://www.zenhub.com",
		"status": "active",
		"statusNote": null,
		"category": "hosted",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "Proprietary",
		"hosting": "both",
		"hostingNote": "Cloud SaaS; Zenhub Enterprise Server on Kubernetes or VMs (AWS, Azure, GCP, VMware, Hyper-V, KVM) with the Enterprise plan.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "1 workspace, up to 50 users, 1 GitHub org, 2 repos, 250 issues.",
			"entryPlan": "Teams",
			"monthlyPerSeatAnnualBillingUsd": 4.99,
			"businessPlan": "Enterprise",
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": "Teams $4.99/user/month billed yearly (monthly price not listed); Enterprise custom; billed in USD via Stripe."
		},
		"sso": "Google SSO on all plans; Enterprise adds enterprise identity and access management, and Enterprise Server lists SSO/SAML/LDAP.",
		"ai": "AI suggested labels and sprint review on Teams; AI acceptance criteria added on Enterprise; no separate AI price listed.",
		"mcp": "yes: official Zenhub MCP server at https://api.zenhub.com/mcp via mcp-remote, authenticated with API token and workspace ID; wraps the GraphQL API.",
		"api": "GraphQL API (recommended), legacy REST API and webhooks.",
		"importers": null,
		"gitForges": "GitHub (issues, repos, orgs); Zenhub Issues also work without GitHub.",
		"exports": null,
		"sources": [
			{
				"label": "Zenhub pricing",
				"url": "https://www.zenhub.com/pricing",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Zenhub Enterprise Server",
				"url": "https://www.zenhub.com/enterprise",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Zenhub FAQ",
				"url": "https://www.zenhub.com/faq",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Zenhub developers",
				"url": "https://developers.zenhub.com/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Zenhub MCP server",
				"url": "https://developers.zenhub.com/mcp/index.html",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "azure-devops-boards",
		"name": "Azure Boards (Azure DevOps)",
		"website": "https://azure.microsoft.com/en-us/products/devops/boards",
		"status": "active",
		"statusNote": null,
		"category": "hosted",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "Proprietary",
		"hosting": "both",
		"hostingNote": "Azure DevOps Services (cloud) or Azure DevOps Server on-premises (requires Windows Server licences).",
		"pricing": {
			"model": "per-seat",
			"freePlan": "First 5 Basic users free; unlimited free Stakeholder access (create/edit work items, view boards).",
			"entryPlan": "Basic",
			"monthlyPerSeatAnnualBillingUsd": 6,
			"businessPlan": "Basic + Test Plans",
			"businessMonthlyPerSeatAnnualBillingUsd": 52,
			"notes": "Billed monthly through Azure with no annual commitment; Server CALs via reseller (3-year commitment) or included with Visual Studio subscriptions ($45/$250 per user/month)."
		},
		"sso": "Services authenticates with Microsoft Entra ID (with Conditional Access) or Microsoft accounts; Server uses Active Directory.",
		"ai": null,
		"mcp": "yes: official Azure DevOps MCP server, remote GA at https://mcp.dev.azure.com/{org} (Entra ID OAuth, Services only) plus a local stdio server; work item, repo, pipeline, wiki and test plan toolsets.",
		"api": "REST API for Services and Server (webhooks not verified).",
		"importers": "CSV work item import (up to 1,000 per file).",
		"gitForges": "Azure Repos and GitHub.com/GitHub Enterprise Server only; other Git hosts are not supported.",
		"exports": "Query results export to CSV; REST API.",
		"sources": [
			{
				"label": "Azure DevOps Services pricing",
				"url": "https://azure.microsoft.com/en-us/pricing/details/devops/azure-devops-services/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Azure DevOps Server pricing",
				"url": "https://azure.microsoft.com/en-us/pricing/details/devops/server/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Azure DevOps authentication and security",
				"url": "https://learn.microsoft.com/en-us/azure/devops/organizations/security/about-security-identity?view=azure-devops",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Remote Azure DevOps MCP Server",
				"url": "https://learn.microsoft.com/en-us/azure/devops/mcp-server/remote-mcp-server?view=azure-devops",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Azure Boards GitHub integration",
				"url": "https://learn.microsoft.com/en-us/azure/devops/boards/github/?view=azure-devops",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Azure Boards CSV import/export",
				"url": "https://learn.microsoft.com/en-us/azure/devops/boards/queries/import-work-items-from-csv?view=azure-devops",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Azure DevOps REST API",
				"url": "https://learn.microsoft.com/en-us/rest/api/azure/devops/?view=azure-devops-rest-7.1",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "notion-projects",
		"name": "Notion Projects",
		"website": "https://www.notion.com",
		"status": "active",
		"statusNote": null,
		"category": "hosted",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "Proprietary",
		"hosting": "cloud",
		"hostingNote": "SaaS only; no self-hosted edition.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Free plan with 5 MB file uploads, 7-day page history, 10 guests, and a block limit for workspaces with 2+ members.",
			"entryPlan": "Plus",
			"monthlyPerSeatAnnualBillingUsd": 10,
			"businessPlan": "Business",
			"businessMonthlyPerSeatAnnualBillingUsd": 20,
			"notes": "USD price data on the pricing page: Plus $12 monthly / $120 yearly, Business $24 monthly / $240 yearly; page displays EUR by default (EUR 9.50 / 19.50); Enterprise is custom pricing."
		},
		"sso": "SAML SSO on Business and Enterprise; SCIM provisioning on Enterprise only.",
		"ai": "Notion AI trial on Free/Plus; Notion Agent, AI Meeting Notes and Enterprise Search included in Business; Custom Agents billed at $10 per 1,000 monthly Notion credits.",
		"mcp": "yes: official Notion MCP for connecting AI clients such as Claude, ChatGPT and Gemini.",
		"api": null,
		"importers": "Trello, Asana, Confluence, Monday.com, Evernote, Quip, Dropbox Paper, Google Docs, WorkFlowy, plus CSV, Markdown, HTML, Word, PDF and ZIP files.",
		"gitForges": "GitHub database sync of pull requests and issues (read-only synced properties, Business plan required); GitLab not mentioned.",
		"exports": "Pages as PDF, HTML or Markdown, databases as CSV; whole-workspace export on web/desktop; Enterprise admins can disable export.",
		"sources": [
			{
				"label": "Notion pricing",
				"url": "https://www.notion.com/pricing",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Notion MCP docs",
				"url": "https://developers.notion.com/docs/mcp",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Notion import help",
				"url": "https://www.notion.com/help/import-data-into-notion",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Notion export help",
				"url": "https://www.notion.com/help/export-your-content",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Notion GitHub help",
				"url": "https://www.notion.com/help/github",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "basecamp",
		"name": "Basecamp",
		"website": "https://basecamp.com",
		"status": "active",
		"statusNote": null,
		"category": "hosted",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "Proprietary",
		"hosting": "cloud",
		"hostingNote": "SaaS only.",
		"pricing": {
			"model": "flat",
			"freePlan": "1 project, 1 GB storage, up to 3 users.",
			"entryPlan": "Freelancer",
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": "Pro Unlimited",
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": "Flat, no per-user fees: Freelancer $29/mo (5 users, 3 projects), Pro $79/mo (10 users), Pro Plus $99/mo (15 users), Pro Unlimited $199/mo billed annually or $329/mo monthly (unlimited users, 1 TB)."
		},
		"sso": null,
		"ai": "Included on every plan: bring-your-own AI agents via CLI and skills, plus ChatGPT and Claude connectors.",
		"mcp": "yes: official Basecamp support for ChatGPT, Claude and AI agents with full MCP support (per pricing page).",
		"api": "REST JSON API with OAuth 2.0, official SDK and OpenAPI spec.",
		"importers": null,
		"gitForges": null,
		"exports": null,
		"sources": [
			{
				"label": "Basecamp pricing",
				"url": "https://basecamp.com/pricing",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Basecamp API docs (bc3-api)",
				"url": "https://github.com/basecamp/bc3-api",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "huly",
		"name": "Huly",
		"website": "https://huly.io",
		"status": "discontinued",
		"statusNote": "Huly's own repo README says hosted Huly has shut down (announced 2026-06-26, shutdown expected July 20, 2026) and the hcengineering/platform repo was frozen in Sept 2026; self-hosting remains possible and development continues in the Platform-Collective/platform repo.",
		"category": "open-source",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "EPL-2.0",
		"hosting": "self-hosted",
		"hostingNote": "Hosted service discontinued; self-host via the huly-selfhost Docker setup.",
		"pricing": {
			"model": "free",
			"freePlan": "Self-hosted source is free under EPL-2.0.",
			"entryPlan": null,
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": null,
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": "huly.io/pricing still lists flat cloud plans (Rare $19.99, Epic $99.99, Legendary $399.99 per month) but the hosted service has shut down per the official README."
		},
		"sso": null,
		"ai": null,
		"mcp": null,
		"api": null,
		"importers": null,
		"gitForges": null,
		"exports": "Official backup and restore guide; self-host setup includes an export service.",
		"sources": [
			{
				"label": "Huly platform repo README (shutdown notice)",
				"url": "https://github.com/hcengineering/platform",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Huly platform LICENSE",
				"url": "https://github.com/hcengineering/platform/blob/develop/LICENSE",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Huly self-host repo",
				"url": "https://github.com/hcengineering/huly-selfhost",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Platform-Collective continuation repo",
				"url": "https://github.com/Platform-Collective/platform",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Huly pricing",
				"url": "https://huly.io/pricing",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "vikunja",
		"name": "Vikunja",
		"website": "https://vikunja.io",
		"status": "active",
		"statusNote": "Latest release v2.8.0 on 2026-11-05.",
		"category": "open-source",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "AGPL-3.0",
		"hosting": "both",
		"hostingNote": "Self-hosted or paid Vikunja Cloud.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Self-hosting is free; Cloud has a 14-day trial only.",
			"entryPlan": "Organization",
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": null,
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": "Cloud prices in EUR only: Organization EUR 5/user/month (EUR 50/user/year); Personal EUR 4/month flat; Family EUR 10/month flat for up to 5 users."
		},
		"sso": "OpenID Connect and LDAP authentication on all editions.",
		"ai": null,
		"mcp": null,
		"api": "REST API (OpenAPI docs, API v2) and webhooks.",
		"importers": "Todoist, Trello, Microsoft To Do, Planka, TickTick, WeKan, CSV and Vikunja export files.",
		"gitForges": null,
		"exports": null,
		"sources": [
			{
				"label": "Vikunja home",
				"url": "https://vikunja.io/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Vikunja pricing",
				"url": "https://vikunja.io/pricing",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Vikunja docs",
				"url": "https://vikunja.io/docs/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Vikunja migration docs",
				"url": "https://vikunja.io/docs/migration-from-third-party-services/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Vikunja repo and releases",
				"url": "https://github.com/go-vikunja/vikunja",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "leantime",
		"name": "Leantime",
		"website": "https://leantime.io",
		"status": "active",
		"statusNote": "Latest release v3.13.2 on 2026-10-31.",
		"category": "open-source",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "AGPL-3.0",
		"hosting": "both",
		"hostingNote": "Self-hosted Community Edition, Leantime Pro cloud, or Enterprise on-premise.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Self-hosted Community Edition is free; the free SaaS tier has been eliminated.",
			"entryPlan": "Leantime Pro (cloud)",
			"monthlyPerSeatAnnualBillingUsd": 8,
			"businessPlan": "Enterprise",
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": "Pro is $10/user/month billed monthly or $8 billed yearly, with 500 AI credits/month; Enterprise is quote-only; some features are Marketplace plugins."
		},
		"sso": "LDAP and OIDC integration in the open-source app; SSO listed as an Enterprise feature on the pricing page.",
		"ai": "AI task features in Pro with 500 AI credits per month.",
		"mcp": "yes: built-in MCP endpoint at https://<your-leantime>/mcp (standalone leantime-mcp repo deprecated).",
		"api": "Extendable via plugins and API (per README).",
		"importers": null,
		"gitForges": null,
		"exports": null,
		"sources": [
			{
				"label": "Leantime pricing",
				"url": "https://leantime.io/pricing/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Leantime repo README and releases",
				"url": "https://github.com/Leantime/leantime",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Leantime MCP repo (deprecation note)",
				"url": "https://github.com/Leantime/leantime-mcp",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "redmine",
		"name": "Redmine",
		"website": "https://www.redmine.org",
		"status": "active",
		"statusNote": "Releases 7.0.3, 6.1.6 and 6.0.12 published 2026-11-01.",
		"category": "open-source",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "GPL-2.0",
		"hosting": "self-hosted",
		"hostingNote": "Self-hosted Ruby on Rails application.",
		"pricing": {
			"model": "free",
			"freePlan": "Free and open source.",
			"entryPlan": null,
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": null,
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": ""
		},
		"sso": "Multiple LDAP authentication sources.",
		"ai": null,
		"mcp": null,
		"api": "REST API in JSON and XML, authenticated by login/password or API key.",
		"importers": null,
		"gitForges": "SCM integration with Git, Subversion, Mercurial, Bazaar and CVS repositories.",
		"exports": null,
		"sources": [
			{
				"label": "Redmine home",
				"url": "https://www.redmine.org/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Redmine REST API wiki",
				"url": "https://www.redmine.org/projects/redmine/wiki/Rest_api",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Redmine licence (doc/COPYING)",
				"url": "https://github.com/redmine/redmine/blob/master/doc/COPYING",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "focalboard",
		"name": "Focalboard / Mattermost Boards",
		"website": "https://www.focalboard.com",
		"status": "discontinued",
		"statusNote": "The official standalone Focalboard repo README says it is currently not maintained (last release v8.0.0, 2024-06-13); the Mattermost Boards plugin is still released (v10.1.0, 2026-10-26).",
		"category": "open-source",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "AGPL-3.0",
		"hosting": "self-hosted",
		"hostingNote": "Standalone personal server/desktop, or as a plugin inside a Mattermost server.",
		"pricing": {
			"model": "free",
			"freePlan": "Free and open source.",
			"entryPlan": null,
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": null,
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": "Licence file: source under AGPL-3.0 or commercial licence; Mattermost-compiled binaries MIT; config files Apache-2.0."
		},
		"sso": null,
		"ai": null,
		"mcp": null,
		"api": null,
		"importers": "Asana, Jira, Nextcloud Deck, Notion, Todoist and Trello importers in the repo.",
		"gitForges": null,
		"exports": null,
		"sources": [
			{
				"label": "Focalboard repo README",
				"url": "https://github.com/mattermost-community/focalboard",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Focalboard LICENSE.txt",
				"url": "https://github.com/mattermost-community/focalboard/blob/main/LICENSE.txt",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Focalboard importers",
				"url": "https://github.com/mattermost-community/focalboard/tree/main/import",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Mattermost Boards plugin releases",
				"url": "https://github.com/mattermost/mattermost-plugin-boards/releases",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "gitea",
		"name": "Gitea",
		"website": "https://about.gitea.com",
		"status": "active",
		"statusNote": "Latest release v28.3.0 on 2026-11-01.",
		"category": "open-source",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "MIT",
		"hosting": "both",
		"hostingNote": "Self-hosted open source or Enterprise, or managed Gitea Cloud by CommitGo.",
		"pricing": {
			"model": "per-seat",
			"freePlan": "Open-source edition free with unlimited users and repositories.",
			"entryPlan": "Enterprise",
			"monthlyPerSeatAnnualBillingUsd": 9.5,
			"businessPlan": null,
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": "Enterprise shown at $9.5/user/month (struck-through $19) with 1-year commitment on online payment; Gitea Cloud price not shown on the fetched page."
		},
		"sso": "LDAP, OAuth2/OIDC, SMTP, PAM, SSPI and reverse-proxy auth in open source; SAML SSO only in Enterprise.",
		"ai": null,
		"mcp": "yes: official gitea-mcp server (gitea.com/gitea/gitea-mcp, v1.13.0).",
		"api": "REST API and webhooks.",
		"importers": "Repository migration from GitHub, GitLab, Bitbucket, Bitbucket Server, Gitea, Forgejo, Gogs, OneDev, GitBucket, Codebase and CodeCommit.",
		"gitForges": "Is itself a Git forge with issues, pull requests, projects and Gitea Actions CI.",
		"exports": null,
		"sources": [
			{
				"label": "Gitea pricing",
				"url": "https://about.gitea.com/pricing",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Gitea authentication docs",
				"url": "https://docs.gitea.com/usage/authentication",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Gitea repo, licence and releases",
				"url": "https://github.com/go-gitea/gitea",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Gitea migrations source",
				"url": "https://github.com/go-gitea/gitea/tree/main/services/migrations",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Gitea MCP server",
				"url": "https://gitea.com/gitea/gitea-mcp",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "forgejo",
		"name": "Forgejo",
		"website": "https://forgejo.org",
		"status": "active",
		"statusNote": "Latest release v17.0.1 on 2026-10-26.",
		"category": "open-source",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "GPL-3.0",
		"hosting": "self-hosted",
		"hostingNote": "Self-hosted; public instances such as Codeberg exist, no official Forgejo-run hosted plan.",
		"pricing": {
			"model": "free",
			"freePlan": "Free and open source, developed under the non-profit Codeberg e.V.",
			"entryPlan": null,
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": null,
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": ""
		},
		"sso": "Acts as OAuth2 provider; LDAP/OIDC login sources not verified from fetched pages.",
		"ai": null,
		"mcp": null,
		"api": "REST API (Swagger) and webhooks.",
		"importers": "Repository migration from GitHub, GitLab, Bitbucket Data Center, Gitea, Gogs, OneDev, GitBucket, Codebase and Pagure.",
		"gitForges": "Is itself a Git forge with issues, projects boards, Forgejo Actions CI and package registry.",
		"exports": null,
		"sources": [
			{
				"label": "Forgejo home",
				"url": "https://forgejo.org/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Forgejo user docs",
				"url": "https://forgejo.org/docs/latest/user/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Forgejo LICENSE",
				"url": "https://codeberg.org/forgejo/forgejo/src/branch/forgejo/LICENSE",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Forgejo releases",
				"url": "https://codeberg.org/forgejo/forgejo/releases",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Forgejo migrations source",
				"url": "https://codeberg.org/forgejo/forgejo/src/branch/forgejo/services/migrations",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "gogs",
		"name": "Gogs",
		"website": "https://gogs.io",
		"status": "active",
		"statusNote": "Latest release v0.14.3 on 2026-06-07; commits as recent as 2026-11-03.",
		"category": "open-source",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "MIT",
		"hosting": "self-hosted",
		"hostingNote": "Single binary or Docker; runs on minimal resources.",
		"pricing": {
			"model": "free",
			"freePlan": "Free and open source.",
			"entryPlan": null,
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": null,
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": ""
		},
		"sso": "LDAP, SMTP, PAM and reverse-proxy header authentication; GitHub/GitHub Enterprise login per README.",
		"ai": null,
		"mcp": null,
		"api": "Experimental REST API plus repository and organization webhooks.",
		"importers": "Migrate and mirror repositories with wiki from other code hosts.",
		"gitForges": "Is itself a Git service with issues, pull requests and wiki.",
		"exports": null,
		"sources": [
			{
				"label": "Gogs home",
				"url": "https://gogs.io/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Gogs authentication docs",
				"url": "https://gogs.io/advancing/authentication",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Gogs repo README, licence and releases",
				"url": "https://github.com/gogs/gogs",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "kanboard",
		"name": "Kanboard",
		"website": "https://kanboard.org",
		"status": "maintenance",
		"statusNote": "README states the application is in maintenance mode (small fixes, community releases); latest release v1.2.57 on 2026-10-31.",
		"category": "open-source",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "MIT",
		"hosting": "self-hosted",
		"hostingNote": null,
		"pricing": {
			"model": "free",
			"freePlan": "Free and open source.",
			"entryPlan": null,
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": null,
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": ""
		},
		"sso": "LDAP/Active Directory and OAuth2 providers such as Google, GitHub and GitLab.",
		"ai": null,
		"mcp": null,
		"api": "JSON-RPC 2.0 API with application token or user credentials/personal access tokens.",
		"importers": null,
		"gitForges": null,
		"exports": null,
		"sources": [
			{
				"label": "Kanboard home",
				"url": "https://kanboard.org/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Kanboard API docs",
				"url": "https://docs.kanboard.org/v1/api/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "Kanboard repo README, licence and releases",
				"url": "https://github.com/kanboard/kanboard",
				"checkedAt": "2026-11-06"
			}
		]
	},
	{
		"slug": "wekan",
		"name": "WeKan",
		"website": "https://wekan.fi",
		"status": "active",
		"statusNote": "Latest release v12.99 on 2026-11-06.",
		"category": "open-source",
		"tier": "B",
		"tierJustification": "Relevant, but lower relevance or search demand for Kuayle; shown in the comparison matrix.",
		"license": "MIT",
		"hosting": "self-hosted",
		"hostingNote": "Self-hosted (Snap, Docker, Kubernetes); site links to SaaS and commercial support pages without published prices.",
		"pricing": {
			"model": "free",
			"freePlan": "Free and open source.",
			"entryPlan": null,
			"monthlyPerSeatAnnualBillingUsd": null,
			"businessPlan": null,
			"businessMonthlyPerSeatAnnualBillingUsd": null,
			"notes": ""
		},
		"sso": "SAML, LDAP and OAuth2 login providers plus passwordless login.",
		"ai": null,
		"mcp": null,
		"api": "API and global webhooks.",
		"importers": "Trello, Jira, Asana, Focalboard, Todoist, Microsoft Planner, Linear, ClickUp, Redmine, monday.com and more, plus CSV/TSV, Excel and WeKan JSON.",
		"gitForges": null,
		"exports": "Boards export to CSV/TSV, JSON (with attachments), Excel, PDF, HTML and iCal.",
		"sources": [
			{
				"label": "WeKan home",
				"url": "https://wekan.fi/",
				"checkedAt": "2026-11-06"
			},
			{
				"label": "WeKan repo README, licence and releases",
				"url": "https://github.com/wekan/wekan",
				"checkedAt": "2026-11-06"
			}
		]
	}
];

export const tierACompetitors: Competitor[] = competitors.filter((competitor) => competitor.tier === 'A');
