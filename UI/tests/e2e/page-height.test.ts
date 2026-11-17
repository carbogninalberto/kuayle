import { test, expect as baseExpect, type Page } from '@playwright/test';

// Many pages load at once on a busy machine; the default 5s is too tight for the first paint.
const expect = baseExpect.configure({ timeout: 15_000 });

// KUA-201: the workspace shell is exactly one viewport tall. The document must
// never scroll on a workspace route: a page that needs scrolling owns its own
// region and the shell clips everything else. Otherwise trackpad momentum at the
// end of a region lifts the whole app (sidebar included) off the top of the
// window and leaves an empty band below it.
//
// Every route is served from mocked API routes with deliberately heavy data
// (long lists, long descriptions, many conversations and coding tasks), so the
// content is always taller than the window and the assertions prove something.

// Every route is its own browser page: the cases are independent and numerous.
test.describe.configure({ mode: 'parallel' });

const createdAt = '2026-01-01T00:00:00Z';
const id = (kind: number, n = 0) =>
	`00000000-0000-0000-${String(kind).padStart(4, '0')}-${String(n).padStart(12, '0')}`;
const userId = id(1);
const teamId = id(10);
const secondTeamId = id(11);
const projectId = id(20);
const cycleId = id(30);
const viewId = id(40);
const machineId = id(50);
const wikiPageId = id(60);
const statusIds = [0, 1, 2, 3, 4].map((n) => id(70, n));
const workflowId = 'cheap';

const user = { id: userId, email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null };
const workspace = {
	id: id(2),
	name: 'Test Workspace',
	slug: 'test',
	logo_url: null,
	current_user_role: 'owner',
	created_at: createdAt,
	updated_at: createdAt
};
const team = (teamKey: string, teamName: string, teamIdValue: string) => ({
	id: teamIdValue,
	name: teamName,
	key: teamKey,
	description: null,
	color: '#6366f1',
	icon: 'layers',
	triage_enabled: true,
	parent_auto_close_enabled: false,
	sub_issue_auto_close_enabled: false,
	issue_copy_prompt: null,
	created_at: createdAt,
	updated_at: createdAt
});
const teams = [team('ENG', 'Engineering', teamId), team('DES', 'Design', secondTeamId)];

const categories = ['backlog', 'unstarted', 'started', 'completed', 'cancelled'];
const statuses = (forTeam: string) =>
	statusIds.map((statusId, index) => ({
		id: statusId,
		team_id: forTeam,
		name: ['Backlog', 'Todo', 'In Progress', 'Done', 'Cancelled'][index],
		slug: ['backlog', 'todo', 'in-progress', 'done', 'cancelled'][index],
		category: categories[index],
		color: '#6366f1',
		position: index,
		is_default: index === 1,
		project_ids: [],
		created_at: createdAt,
		updated_at: createdAt
	}));

const longParagraph = 'Long description paragraph that keeps going and going. '.repeat(8);
const longHtml = Array.from({ length: 60 }, (_, index) => `<p>${index + 1}. ${longParagraph}</p>`).join('');

const issue = (number: number, extra: Record<string, unknown> = {}) => {
	const index = number % 5;
	return {
		id: id(100, number),
		identifier: `ENG-${number}`,
		title: `Heavy page issue ${number}`,
		description: null,
		status: ['backlog', 'todo', 'in-progress', 'done', 'cancelled'][index],
		status_id: statusIds[index],
		status_info: {
			id: statusIds[index],
			name: ['Backlog', 'Todo', 'In Progress', 'Done', 'Cancelled'][index],
			category: categories[index],
			color: '#6366f1',
			position: index
		},
		priority: number % 5,
		team_id: teamId,
		project_id: number % 2 ? projectId : null,
		cycle_id: number % 3 ? cycleId : null,
		creator_id: userId,
		assignee_id: userId,
		creator: user,
		assignee: user,
		assignees: [user],
		parent_id: null,
		due_date: null,
		sort_order: number * 1000,
		labels: [],
		sub_issue_count: 0,
		sub_issue_done: 0,
		triage_status: 'pending',
		created_at: createdAt,
		updated_at: createdAt,
		...extra
	};
};
const issues = Array.from({ length: 120 }, (_, index) => issue(index + 1));
issues[0] = issue(1, { description: longHtml });

const project = (number: number) => ({
	id: number === 0 ? projectId : id(21, number),
	name: `Project ${number}`,
	description: 'A project description',
	status: 'in_progress',
	team_id: teamId,
	lead_id: userId,
	start_date: null,
	target_date: null,
	sort_order: number,
	color: '#3b82f6',
	icon: null,
	progress: { total: 10, completed: 3, cancelled: 1 },
	created_at: createdAt,
	updated_at: createdAt
});
const projects = Array.from({ length: 40 }, (_, index) => project(index));

const labels = Array.from({ length: 60 }, (_, index) => ({
	id: id(80, index),
	name: `Label ${index}`,
	color: '#ef4444',
	description: 'A label',
	parent_id: null,
	project_id: null,
	created_at: createdAt,
	updated_at: createdAt
}));
const members = Array.from({ length: 60 }, (_, index) => ({
	user_id: index === 0 ? userId : id(1, index),
	email: `member${index}@example.com`,
	name: `Member ${index}`,
	avatar_url: null,
	role: index === 0 ? 'owner' : 'member',
	created_at: createdAt
}));
const views = Array.from({ length: 80 }, (_, index) => ({
	id: index === 0 ? viewId : id(41, index),
	workspace_id: workspace.id,
	creator_id: userId,
	name: `View ${index}`,
	description: null,
	filters: index % 2 === 0 ? { view_scope: 'team', view_team: teamId } : {},
	is_shared: index % 2 === 0,
	created_at: createdAt,
	updated_at: createdAt
}));
const cycles = Array.from({ length: 24 }, (_, index) => ({
	id: index === 0 ? cycleId : id(31, index),
	team_id: teamId,
	name: `Cycle ${index + 1}`,
	number: index + 1,
	status: index === 0 ? 'active' : 'completed',
	description: null,
	goals: null,
	retrospective: null,
	start_date: '2026-01-01',
	end_date: '2026-01-14',
	completed_at: null,
	progress: { total: 10, completed: 4, cancelled: 0, started: 3, unstarted: 3 },
	created_at: createdAt,
	updated_at: createdAt
}));

const conversationFor = (n: number) => ({
	id: `chat-${n}`,
	workspace_id: workspace.id,
	creator_id: userId,
	issue_id: n === 1 ? issues[0].id : null,
	team_id: teamId,
	title: `Conversation ${n}`,
	created_at: createdAt,
	updated_at: createdAt
});
const conversations = Array.from({ length: 60 }, (_, index) => conversationFor(index + 1));

const queue = Array.from({ length: 15 }, (_, index) => ({
	id: `task-${index}`,
	conversation_id: `chat-${index + 1}`,
	state: index % 3 === 0 ? 'running' : index % 3 === 1 ? 'awaiting_review' : 'failed',
	reason: '',
	issue_identifier: `ENG-${index + 1}`,
	issue_title: `Coding task for issue ${index + 1}`,
	updated_at: createdAt
}));

const codingTasks = Array.from({ length: 40 }, (_, index) => ({
	id: `task-${index}`,
	workspace_id: workspace.id,
	conversation_id: 'chat-1',
	creator_id: userId,
	issue_id: issues[0].id,
	team_id: teamId,
	prompt: `Implement heavy page issue ${index}, following the issue description closely.`,
	provider: 'claude-code',
	model: 'claude-opus-5-5',
	thinking: 'high',
	state: index === 0 ? 'running' : index % 2 ? 'completed' : 'failed',
	reason: 'Native coding harness started',
	generation: index + 1,
	failure_count: 0,
	machine_id: 'machine-1',
	checkout_id: 'checkout-1',
	run_id: `run-${index}`,
	...(index === 0 ? { workflow_version_id: 'version-1', workflow_node: 'implement', workflow_status: 'running' } : {}),
	created_at: `2026-10-26T10:${String(59 - index).padStart(2, '0')}:00Z`,
	updated_at: createdAt
}));

const taskWorkflow = {
	workflow_id: 'workflow-1',
	name: 'Plan and review',
	version: 2,
	builtin: false,
	graph: {
		nodes: [
			{ id: 'start', type: 'start', label: 'Start', position: { x: 0, y: 0 } },
			...['plan', 'implement', 'publish', 'review', 'done'].map((node, index) => ({
				id: node,
				type: 'agent',
				label: node,
				position: { x: 280 * (index + 1), y: 0 }
			}))
		],
		edges: [],
		budget: {}
	},
	node: 'implement',
	status: 'running',
	visits: {},
	iterations: 3,
	sequence: 1,
	steps: [],
	runs: [],
	profiles: [],
	can_decide: false,
	task_state: 'running',
	task_reason: '',
	task_generation: 1
};

const machine = (number: number) => ({
	id: number === 0 ? machineId : id(51, number),
	workspace_id: workspace.id,
	routing_key: `machine${number}`,
	name: `task-machine-${number}`,
	status: number % 3 ? 'running' : 'stopped',
	desired_status: number % 3 ? 'running' : 'stopped',
	generation: 1,
	repo_provider: 'github',
	repo_owner: 'kuayle',
	repo_name: 'kuayle',
	base_branch: 'main',
	working_branch: `kuayle/task-${number}`,
	machine_size: 'medium',
	profile: 'developer',
	cpu_millis: 4000,
	memory_mb: 8192,
	disk_gb: 40,
	pids_limit: 1024,
	environment_builder: false,
	created_at: createdAt,
	updated_at: createdAt
});
const machines = Array.from({ length: 30 }, (_, index) => machine(index));

const wikiPage = (number: number) => ({
	id: number === 0 ? wikiPageId : id(61, number),
	team_id: teamId,
	project_id: null,
	title: `Wiki page ${number}`,
	content: longHtml,
	version: 1,
	updated_by_name: 'Ada',
	updated_at: createdAt,
	created_at: createdAt,
	created_by: userId,
	registry_date: null
});
const wikiPages = Array.from({ length: 40 }, (_, index) => wikiPage(index));

const workflowGraph = {
	nodes: [
		{ id: 'start', type: 'start', label: 'Start', position: { x: 0, y: 0 } },
		...Array.from({ length: 12 }, (_, index) => ({
			id: `step-${index}`,
			type: 'agent',
			label: `Step ${index}`,
			prompt: 'Do the work.',
			position: { x: 280 * (index + 1), y: (index % 3) * 160 }
		})),
		{ id: 'done', type: 'end', label: 'Done', outcome: 'done', position: { x: 280 * 14, y: 0 } }
	],
	edges: [],
	budget: {}
};
const workflowSummary = {
	id: workflowId,
	name: 'Cheap Workflow',
	description: '',
	builtin: false,
	customized: false,
	latest_version: 4,
	version_id: 'cheap-v4',
	archived: false,
	graph: workflowGraph,
	updated_at: createdAt
};

const webhooks = Array.from({ length: 30 }, (_, index) => ({
	id: id(90, index),
	url: `https://example.com/hooks/${index}`,
	events: ['issue.created', 'issue.updated'],
	is_active: index % 2 === 0,
	created_at: createdAt,
	updated_at: createdAt
}));
const tokens = Array.from({ length: 30 }, (_, index) => ({
	id: id(91, index),
	name: `Token ${index}`,
	token_prefix: `kyl_${index}`,
	scopes: ['read'],
	workspace_slugs: null,
	expires_at: null,
	last_used_at: null,
	created_at: createdAt
}));
const templates = Array.from({ length: 40 }, (_, index) => ({
	id: id(92, index),
	team_id: index % 2 ? teamId : null,
	title: `Template ${index}`,
	description: '<p>Template description</p>',
	priority: 0,
	label_ids: []
}));
const lessons = Array.from({ length: 40 }, (_, index) => ({
	id: id(93, index),
	repository: 'kuayle/kuayle',
	team_id: null,
	state: index % 4 === 0 ? 'proposed' : 'approved',
	text: `Lesson ${index}: ${'always run the tests before opening a pull request. '.repeat(3)}`,
	source_event: 'manual',
	proposer: 'person',
	source_task_id: null,
	proposed_by: null,
	decided_by: null,
	decided_at: null,
	position: index,
	created_at: createdAt,
	updated_at: createdAt
}));
const analytics = {
	overview: {
		total_issues: 42,
		open_issues: 18,
		completed_issues: 24,
		overdue_issues: 3,
		total_projects: 4,
		total_members: 8,
		started_issues: 6,
		unassigned_issues: 2,
		completion_rate: 57.14,
		avg_lead_time_hours: 48,
		avg_cycle_time_hours: 24
	},
	distribution: {
		by_status: statusIds.map((statusId, index) => ({
			status_id: statusId,
			name: ['Backlog', 'Todo', 'In Progress', 'Done', 'Cancelled'][index],
			color: '#6366f1',
			category: categories[index],
			count: 5 + index
		})),
		by_priority: [0, 1, 2, 3, 4].map((priority) => ({ priority, count: 3 + priority }))
	},
	burnup: {
		interval: 'week',
		from: '2026-01-01',
		to: '2026-06-01',
		points: Array.from({ length: 20 }, (_, index) => ({
			date: `2026-01-${String(index + 1).padStart(2, '0')}`,
			created: 5,
			completed: 3,
			total_created: 5 * (index + 1),
			total_completed: 3 * (index + 1),
			scope: 2 * (index + 1)
		}))
	}
};

const preferences = (theme: 'light' | 'dark') => ({
	font_size: 'default',
	pointer_cursors: true,
	theme_mode: theme,
	light_theme: 'light',
	dark_theme: 'dark',
	workflow_sort_mode: 'default',
	workflow_sort_order: categories,
	team_workflow_sort_overrides: {},
	recent_due_dates: [],
	issues_group_by: 'status',
	browser_notifications: true,
	workflow_editor_panel_open: true,
	notification_sound: 'chime',
	notification_events: {}
});

const issuesPage = { data: issues, total_count: issues.length, page: 1, per_page: 200, has_more: false };

async function mockApi(page: Page, theme: 'light' | 'dark') {
	await page.routeWebSocket('**/ws', () => {});
	await page.routeWebSocket('**/conversations/*/stream*', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		const path = url.pathname;
		const json = (body: unknown) => route.fulfill({ json: body });
		const w = '/api/workspaces/test';
		if (path === '/api/auth/me') return json(user);
		if (path === '/api/preferences') return json(preferences(theme));
		if (path === '/api/workspaces') return json([workspace]);
		if (path === w) return json(workspace);
		if (path === `${w}/teams`) return json(teams);
		const teamMatch = path.match(/^\/api\/workspaces\/test\/teams\/([^/]+)(\/.*)?$/);
		if (teamMatch) {
			const [, forTeam, rest = ''] = teamMatch;
			const found = teams.find((candidate) => candidate.id === forTeam) ?? teams[0];
			if (rest === '') return json(found);
			if (rest === '/statuses') return json(statuses(forTeam));
			if (rest === '/cycles') return json(cycles);
			if (rest === '/projects') return json(projects);
			if (rest === '/members') return json(members);
			if (rest === '/cycles/velocity') return json([]);
			if (rest.match(/^\/cycles\/[^/]+$/)) return json(cycles[0]);
			if (rest.match(/^\/cycles\/[^/]+\/burndown$/)) return json([]);
			const wiki = rest.match(/^\/wiki(?:\/([^/]+))?(?:\/(versions|backlinks|publish))?$/);
			if (wiki) return wikiResponse(json, wiki[1], wiki[2]);
		}
		if (path === '/api/notifications') return json({ notifications: [], unread_count: 0 });
		if (path === `${w}/issues`) return json(issuesPage);
		if (path.match(/^\/api\/workspaces\/test\/issues\/ENG-\d+$/)) {
			const number = Number(path.split('-').pop());
			return json(issues.find((candidate) => candidate.identifier === `ENG-${number}`) ?? issues[0]);
		}
		if (path.match(/^\/api\/workspaces\/test\/issues\/ENG-\d+\/github$/))
			return json({ pull_requests: [], branches: [], commits: [] });
		if (path.match(/^\/api\/workspaces\/test\/issues\/ENG-\d+\/forges$/))
			return json({ merge_requests: [], branches: [], commits: [] });
		if (
			path.match(/^\/api\/workspaces\/test\/issues\/ENG-\d+\/(comments|history|relations|sub-issues|wiki-references)$/)
		)
			return json([]);
		if (path === `${w}/issue-templates`) return json(templates);
		if (path === `${w}/webhooks`) return json(webhooks);
		if (path === '/api/tokens') return json(tokens);
		if (path === `${w}/memory/lessons`)
			return json({
				lessons: lessons.filter((lesson) => lesson.state === url.searchParams.get('state')),
				can_manage: true,
				limits: { lesson_characters: 600, approved_per_repository: 50, prompt_bytes: 8000 }
			});
		if (path === `${w}/analytics/overview`) return json(analytics.overview);
		if (path === `${w}/analytics/distribution`) return json(analytics.distribution);
		if (path === `${w}/analytics/burnup`) return json(analytics.burnup);
		if (path === `${w}/invite-links`) return json([]);
		if (path === `${w}/projects`) return json(projects);
		const projectMatch = path.match(/^\/api\/workspaces\/test\/projects\/([^/]+)(\/.*)?$/);
		if (projectMatch) {
			const [, , rest = ''] = projectMatch;
			if (rest === '') return json(projects[0]);
			const wiki = rest.match(/^\/wiki(?:\/([^/]+))?(?:\/(versions|backlinks|publish))?$/);
			if (wiki) return wikiResponse(json, wiki[1], wiki[2], true);
		}
		if (path === `${w}/labels`) return json(labels);
		if (path === `${w}/members`) return json(members);
		if (path === `${w}/views`) return json(views);
		if (path.match(/^\/api\/workspaces\/test\/views\/[^/]+$/)) return json(views[0]);
		if (path === `${w}/dev-machines`)
			return json({ data: machines, total_count: machines.length, page: 1, per_page: 50, has_more: false });
		if (path === `${w}/dev-machines/${machineId}`) return json(machines[0]);
		if (path === `${w}/agent-tasks`) return json(queue);
		const taskRest = path.match(/^\/api\/workspaces\/test\/agent-tasks\/[^/]+(\/.*)?$/);
		if (taskRest) {
			if (taskRest[1] === '/workflow') return json(taskWorkflow);
			if (taskRest[1] === '/history') return json({ transitions: [] });
			if (taskRest[1] === '/runs') return json({ runs: [] });
		}
		if (path === `${w}/agent-messages`) return json([]);
		if (path === `${w}/workflows`) return json({ workflows: [workflowSummary], defaults: [], can_manage: true });
		if (path === `${w}/workflows/validate`) return json({ issues: [], warnings: [] });
		if (path === `${w}/workflows/${workflowId}`) return json({ ...workflowSummary, versions: [] });
		if (path === `${w}/agent-profiles`)
			return json({
				profiles: [],
				can_manage: true,
				harnesses: [],
				model_harnesses: [],
				model_formats: {},
				behaviours: []
			});
		if (path === `${w}/assistant/status`)
			return json({ configured: true, provider: 'openai_compatible', model: 'configured-model' });
		if (path === `${w}/conversations`) return json(request.method() === 'POST' ? conversations[0] : conversations);
		const conversation = path.match(/^\/api\/workspaces\/test\/conversations\/([^/]+)(\/.*)?$/);
		if (conversation) {
			const [, conversationId, rest = ''] = conversation;
			if (rest === '/events') return json([]);
			if (rest === '/tasks') return json({ tasks: conversationId === 'chat-1' ? codingTasks : [] });
			if (rest === '/task-options') return json({ providers: [], model_servers: [] });
			if (rest === '')
				return json({
					conversation: conversations.find((candidate) => candidate.id === conversationId) ?? conversations[0],
					issue:
						conversationId === 'chat-1'
							? { id: issues[0].id, identifier: 'ENG-1', title: issues[0].title, status: 'todo', description: 'Heavy' }
							: undefined,
					agent_messages: [],
					messages: [],
					tools: [],
					approvals: [],
					cursor: 0,
					has_more: false
				});
		}
		if (path.endsWith('/task-options')) return json({ providers: [], model_servers: [] });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});
}

function wikiResponse(
	json: (body: unknown) => Promise<void>,
	pageId: string | undefined,
	sub: string | undefined,
	isProject = false
) {
	const scoped = wikiPages.map((p) => ({
		...p,
		team_id: isProject ? null : p.team_id,
		project_id: isProject ? projectId : null
	}));
	if (pageId === 'settings') return json({ registry_enabled: false });
	if (pageId === 'tree')
		return json(scoped.map((p, index) => ({ id: p.id, parent_id: null, position: index, kind: 'page' })));
	if (pageId === 'registry') return json({ days: [], next_before: null });
	if (sub === 'versions') return json([]);
	if (sub === 'backlinks') return json({ issues: [], pages: [] });
	if (sub === 'publish') return json({ mode: 'internal', children: true, url: null });
	if (!pageId) return json(scoped.map(({ content: _content, ...summary }) => summary));
	return json(scoped.find((p) => p.id === pageId) ?? scoped[0]);
}

type Scroller = { path: string; scrollHeight: number; clientHeight: number; nestedIn: string | null };

/**
 * The page-height contract of a workspace route: the document does not scroll
 * and the pages scrolling regions are not nested in one another.
 *
 * The literal "exactly one vertical scroll region" is too strict. The assistant
 * shows a conversation list, a chat and a side panel side by side and settings
 * has a navigation next to its content, so several sibling scrollers are
 * correct. What must never happen is a scroller inside another scroller (two
 * scrollbars on one gesture), or the shell's own region scrolling while the page
 * also scrolls.
 */
async function expectNoPageScroll(page: Page, options: { expectOverflow: boolean }) {
	const viewportHeight = await page.evaluate(() => window.innerHeight);
	// 1. The document is never taller than the window.
	await expect
		.poll(() => page.evaluate(() => document.documentElement.scrollHeight), { message: 'document scrollHeight' })
		.toBeLessThanOrEqual(viewportHeight + 1);
	await expect
		.poll(() => page.evaluate(() => document.body.scrollHeight), { message: 'body scrollHeight' })
		.toBeLessThanOrEqual(viewportHeight + 1);

	const readScrollers = () =>
		page.evaluate((): Scroller[] => {
			const main = document.querySelector('main');
			if (!main) return [];
			const describe = (element: Element) =>
				`${element.tagName.toLowerCase()}${element.id ? `#${element.id}` : ''}${
					typeof element.className === 'string' && element.className
						? `.${element.className.trim().split(/\s+/).slice(0, 4).join('.')}`
						: ''
				}`;
			const isScroller = (element: Element) => {
				const style = getComputedStyle(element);
				// A short list capped with max-height (the events list on settings/preferences) is a
				// deliberate widget, not a page region. A capped element taller than half the window is.
				if (style.maxHeight !== 'none' && element.clientHeight < window.innerHeight / 2) return false;
				const overflowY = style.overflowY;
				return (
					(overflowY === 'auto' || overflowY === 'scroll') &&
					element.scrollHeight > element.clientHeight + 1 &&
					element.clientHeight > 0
				);
			};
			const scrollers = [...main.querySelectorAll('*')].filter(isScroller);
			return scrollers.map((element) => {
				let parent = element.parentElement;
				while (parent && parent !== main && !scrollers.includes(parent)) parent = parent.parentElement;
				return {
					path: describe(element),
					scrollHeight: element.scrollHeight,
					clientHeight: element.clientHeight,
					nestedIn: parent && parent !== main ? describe(parent) : null
				};
			});
		});

	const scrollers = await readScrollers();
	// The data is heavy enough that something must scroll, or nothing below proves anything.
	if (options.expectOverflow) expect(scrollers.length, 'at least one region scrolls').toBeGreaterThan(0);
	// 2. No scroller sits inside another scroller.
	expect(
		scrollers.filter((scroller) => scroller.nestedIn),
		'nested vertical scrollers'
	).toEqual([]);
	// 3. The shell's own region does not scroll when the page scrolls itself.
	const shell = await page.getByTestId('shell-scroll').evaluate((element) => ({
		scrolls: element.scrollHeight > element.clientHeight + 1
	}));
	if (shell.scrolls)
		expect(
			scrollers.map((scroller) => scroller.path),
			'shell region is the only scroller'
		).toHaveLength(1);

	// 4. A wheel gesture at the end of every region never moves the document.
	await page.evaluate(() => {
		for (const element of document.querySelectorAll('main *')) {
			if (element.scrollHeight > element.clientHeight + 1) element.scrollTop = element.scrollHeight;
		}
	});
	await wheelOverMain(page);
	expect(await page.evaluate(() => window.scrollY), 'window.scrollY after wheel').toBe(0);
	expect(await page.evaluate(() => document.documentElement.scrollTop), 'document scrollTop after wheel').toBe(0);
}

/**
 * Wheel over the middle of main and wait until the browser has handled the gesture:
 * the page saw the wheel event and has painted twice since, which is when a scroll
 * chained to the document would have moved it.
 */
async function wheelOverMain(page: Page) {
	await page.evaluate(() => {
		const counter = window as unknown as { __wheelEvents: number };
		counter.__wheelEvents = 0;
		window.addEventListener('wheel', () => (counter.__wheelEvents += 1), { capture: true, passive: true });
	});
	const box = await page.locator('main').first().boundingBox();
	await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
	await page.mouse.wheel(0, 5000);
	await expect
		.poll(() => page.evaluate(() => (window as unknown as { __wheelEvents: number }).__wheelEvents), {
			message: 'the page receives the wheel gesture'
		})
		.toBeGreaterThan(0);
	await page.evaluate(
		() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))
	);
}

/** Resolves once the document and the shell region report the same layout on two reads in a row. */
async function waitForLayoutToSettle(page: Page) {
	let previous = '';
	await expect
		.poll(
			async () => {
				const current = await page.evaluate(() => {
					const shell = document.querySelector('[data-testid="shell-scroll"]');
					return [
						document.documentElement.scrollHeight,
						shell?.scrollHeight,
						document.querySelectorAll('main *').length
					].join('/');
				});
				const settled = current === previous;
				previous = current;
				return settled;
			},
			{ message: 'layout settles', intervals: [100, 150, 250, 500] }
		)
		.toBe(true);
}

/** A side panel body wider than its column shows a horizontal scrollbar next to the vertical one. */
async function expectNoPanelHorizontalScroll(page: Page) {
	const region = page.getByTestId('side-panel').locator('[role=region]').first();
	await expect(region).toBeVisible();
	await expect
		.poll(() => region.evaluate((element) => element.scrollWidth - element.clientWidth), {
			message: 'side panel horizontal overflow'
		})
		.toBeLessThanOrEqual(1);
}

type Route = {
	name: string;
	path: string;
	/** Text that proves the page rendered its data instead of an error or an empty state. */
	ready?: string | RegExp;
	/**
	 * The mocked data is taller than even a 1600px window, so a region must scroll. 'wide':
	 * only where the conversation list is a column, not a drawer.
	 */
	long?: boolean | 'wide';
	/** The route opens a side panel whose body must not scroll sideways. */
	panel?: boolean;
	/** Runs after the route has loaded, e.g. to open a side panel through the UI. */
	afterLoad?: (page: Page) => Promise<void>;
};

// On a wide screen the panel starts as 'auto': Details until the conversation's issue has
// loaded, then Agents. The buttons flip under the test, so both helpers retry until the state
// holds instead of trusting one read of aria-pressed.
const panelGroup = (page: Page) => page.getByRole('group', { name: 'Side panels' });
const openPanel = (name: string) => async (page: Page) => {
	const button = panelGroup(page).getByRole('button', { name, exact: true });
	await expect(button).toBeVisible();
	await expect
		.poll(
			async () => {
				if ((await button.getAttribute('aria-pressed')) !== 'true') await button.click();
				return button.getAttribute('aria-pressed');
			},
			{ message: `${name} panel opens` }
		)
		.toBe('true');
};
const closePanels = async (page: Page) => {
	await expect(panelGroup(page).getByRole('button').first()).toBeVisible();
	await expect
		.poll(
			async () => {
				const open = panelGroup(page).getByRole('button', { pressed: true });
				if (await open.count()) await open.first().click();
				return page.getByTestId('side-panel').count();
			},
			{ message: 'side panels close' }
		)
		.toBe(0);
};

const wikiParagraph = '1. Long description paragraph';
const teamPath = `/test/teams/${teamId}`;
const routes: Route[] = [
	{ name: 'team issues', path: teamPath, ready: 'Heavy page issue 2', long: true },
	{ name: 'team board', path: `${teamPath}/board`, ready: 'Heavy page issue 2', long: true },
	{ name: 'team cycles', path: `${teamPath}/cycles`, ready: 'Cycle 2' },
	{ name: 'team cycle', path: `${teamPath}/cycles/${cycleId}`, ready: 'Heavy page issue 2', long: true },
	{ name: 'team projects', path: `${teamPath}/projects`, ready: 'Project 2', long: true },
	{ name: 'team triage', path: `${teamPath}/triage`, ready: 'Heavy page issue 2', long: true },
	{ name: 'team views', path: `${teamPath}/views`, ready: 'View 2', long: true },
	{ name: 'team wiki', path: `${teamPath}/wiki`, ready: 'Wiki page 2', long: true },
	{ name: 'team wiki page', path: `${teamPath}/wiki/read/${wikiPageId}`, ready: wikiParagraph, long: true },
	{ name: 'projects', path: '/test/projects', ready: 'Project 2', long: true },
	{ name: 'project', path: `/test/projects/${projectId}`, ready: 'Heavy page issue 3', long: true },
	{ name: 'project wiki', path: `/test/projects/${projectId}/wiki`, ready: 'Wiki page 2', long: true },
	{
		name: 'project wiki page',
		path: `/test/projects/${projectId}/wiki/read/${wikiPageId}`,
		ready: wikiParagraph,
		long: true
	},
	{ name: 'view', path: `/test/views/${viewId}`, ready: 'Heavy page issue 2', long: true },
	{ name: 'my issues', path: '/test/my-issues', ready: 'Heavy page issue 2', long: true },
	{ name: 'my views', path: '/test/my-views', ready: 'View 2', long: true },
	{ name: 'inbox', path: '/test/inbox' },
	{ name: 'insights', path: '/test/insights' },
	{ name: 'issue with a long description', path: '/test/issue/ENG-1', ready: 'Heavy page issue 1', long: true },
	{ name: 'assistant', path: '/test/assistant', long: 'wide' },
	{
		name: 'assistant conversation with the details panel',
		path: '/test/assistant?conversation=chat-2',
		long: 'wide',
		afterLoad: openPanel('Details'),
		panel: true
	},
	{
		name: 'assistant conversation without a side panel',
		path: '/test/assistant?conversation=chat-2',
		long: 'wide',
		afterLoad: closePanels
	},
	{
		name: 'assistant coding tasks panel',
		path: '/test/assistant?conversation=chat-1',
		long: 'wide',
		afterLoad: openPanel('Agents'),
		panel: true,
		ready: 'Coding tasks'
	},
	{
		name: 'assistant coding tasks panel closed',
		path: '/test/assistant?conversation=chat-1',
		long: 'wide',
		afterLoad: closePanels
	},
	{ name: 'machines', path: '/test/machines', ready: 'task-machine-2', long: true },
	{ name: 'machine', path: `/test/machines/${machineId}` },
	{ name: 'settings', path: '/test/settings' },
	...[
		'agent-messages',
		'agents',
		'ai',
		'assistant',
		'backups',
		'dev-machines',
		'forges',
		'github',
		'labels',
		'lessons',
		'licenses',
		'mcp',
		'members',
		'preferences',
		'profile',
		'sso',
		'telegram',
		'templates',
		'tokens',
		'version',
		'voice',
		'webhooks',
		'workflows'
	].map((name) => ({
		name: `settings ${name}`,
		path: `/test/settings/${name}`,
		// Pages whose mocked lists, or own copy, are taller than the window.
		long: ['labels', 'members', 'preferences', 'templates', 'tokens', 'webhooks', 'lessons'].includes(name)
	})),
	{ name: 'settings team', path: `/test/settings/teams/${teamId}` },
	{ name: 'settings team statuses', path: `/test/settings/teams/${teamId}/statuses` },
	{ name: 'settings workflow', path: `/test/settings/workflows/${workflowId}` }
];

const viewports = [
	{ width: 1280, height: 900 },
	{ width: 1440, height: 1600 },
	{ width: 390, height: 844 }
];
// Layout does not depend on the colours: the dark theme is covered once, at the common desktop size.
const cases = viewports.flatMap((viewport) =>
	(['light', 'dark'] as const)
		.filter((theme) => theme === 'light' || viewport.width === 1280)
		.map((theme) => ({ viewport, theme }))
);

// Out-of-flow content is what lifted the shell in KUA-201: an absolutely positioned
// element (a visually hidden label, a chart tooltip, a popover anchor) inside a
// scrolling region has the initial containing block (the viewport-sized box of the
// root) as its containing block when no ancestor is positioned, so overflow-hidden
// on a non-positioned ancestor does not clip it and its box past the bottom of the
// window grows the document. The shell is positioned (relative), so it is the
// containing block and clips it.
for (const { theme, viewport } of cases) {
	test(`out-of-flow content cannot lift the shell at ${viewport.width}x${viewport.height} in ${theme}`, async ({
		page
	}) => {
		await page.setViewportSize(viewport);
		await mockApi(page, theme);
		await page.goto(teamPath);
		await expect(page.getByText('Heavy page issue 2').first()).toBeVisible();
		await page.getByTestId('shell-scroll').evaluate((region) => {
			const host = region.firstElementChild ?? region;
			for (const [index, offset] of [400, 1200, 3000].entries()) {
				const leak = document.createElement('span');
				leak.textContent = `out of flow ${index}`;
				leak.style.cssText = `position:absolute;top:${offset}px;left:20px;height:40px;width:40px`;
				host.appendChild(leak);
			}
			const hidden = document.createElement('span');
			hidden.className = 'sr-only';
			hidden.textContent = 'visually hidden label';
			host.appendChild(hidden);
		});
		const viewportHeight = await page.evaluate(() => window.innerHeight);
		await expect
			.poll(() => page.evaluate(() => document.documentElement.scrollHeight))
			.toBeLessThanOrEqual(viewportHeight + 1);
		await wheelOverMain(page);
		expect(await page.evaluate(() => window.scrollY)).toBe(0);
	});
}

for (const { theme, viewport } of cases) {
	for (const route of routes) {
		test(`${route.name} never scrolls the document at ${viewport.width}x${viewport.height} in ${theme}`, async ({
			page
		}) => {
			await page.setViewportSize(viewport);
			await mockApi(page, theme);
			const pageErrors: string[] = [];
			page.on('pageerror', (error) => pageErrors.push(error.message));
			await page.goto(route.path);
			await expect(page.locator('html')).toHaveClass(new RegExp(theme));
			await expect(page.getByTestId('shell-scroll')).toBeVisible();
			await route.afterLoad?.(page);
			if (route.ready) await expect(page.getByText(route.ready).locator('visible=true').first()).toBeVisible();
			await page.waitForLoadState('networkidle');
			await waitForLayoutToSettle(page);
			// The fixtures are taller than the tallest window, so every long route must scroll in a region.
			await expectNoPageScroll(page, {
				expectOverflow: !!route.long && (route.long !== 'wide' || viewport.width >= 640)
			});
			if (route.panel) await expectNoPanelHorizontalScroll(page);
			expect(pageErrors).toEqual([]);
		});
	}
}
// The workflow editor's canvas keeps a minimum height. In a window shorter than that the
// shell's region may scroll, but the document still must not.
test('the workflow editor never scrolls the document in a short window', async ({ page }) => {
	await page.setViewportSize({ width: 1280, height: 560 });
	await mockApi(page, 'light');
	await page.goto(`/test/settings/workflows/${workflowId}`);
	await expect(page.getByTestId('shell-scroll')).toBeVisible();
	await page.waitForLoadState('networkidle');
	await waitForLayoutToSettle(page);
	await expectNoPageScroll(page, { expectOverflow: false });
});
