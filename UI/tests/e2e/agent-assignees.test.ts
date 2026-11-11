import { expect, test, type Page } from '@playwright/test';

const userId = '00000000-0000-0000-0000-000000000001';
const teamId = '00000000-0000-0000-0000-000000000010';
const otherTeamId = '00000000-0000-0000-0000-000000000011';
const statusId = '00000000-0000-0000-0000-000000000020';
const issueId = '00000000-0000-0000-0000-000000000101';
const fixerId = '00000000-0000-0000-0000-000000000301';
const writerId = '00000000-0000-0000-0000-000000000302';
const createdAt = '2026-01-01T00:00:00Z';

type Assigned = { id: string; task_state: string | null; task_reason?: string | null; error?: string | null };

// A workspace with one issue, a "Fixer" agent enabled for its team and a
// "Docs writer" agent enabled only elsewhere.
async function mockWorkspace(page: Page, options: { role?: string; assigned?: Assigned[]; refuse?: string; groupBy?: string; withPersonIssue?: boolean } = {}) {
	const role = options.role ?? 'owner';
	const state = {
		assigned: options.assigned ?? [],
		puts: [] as string[][],
		updates: [] as { id: string; body: any }[],
		issueQueries: [] as string[],
		patches: [] as { path: string; body: any }[],
		comments: [] as string[],
		unhandled: [] as string[]
	};
	const agents = [
		{ id: fixerId, name: 'Fixer', role: 'Implementer', harness: 'codex', model: 'gpt-5', effort: '', avatar_color: '#3366ff', team_ids: [teamId], available: true },
		{ id: writerId, name: 'Docs writer', role: 'Technical writer', harness: 'goose', model: '', effort: '', avatar_color: '', team_ids: [otherTeamId], available: false, unavailable_reason: 'No allowed coding agent is available' }
	];
	const user = { id: userId, email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null };
	const assignees = () =>
		state.assigned.map((entry) => {
			const agent = agents.find((candidate) => candidate.id === entry.id)!;
			return {
				issue_id: issueId, id: agent.id, name: agent.name, role: agent.role, harness: agent.harness, model: agent.model, avatar_color: agent.avatar_color,
				assigned_by: userId, assigned_by_name: 'Test User', task_id: entry.task_state ? 'task-1' : null, task_state: entry.task_state,
				task_reason: entry.task_reason ?? null, error: entry.error ?? null, created_at: createdAt
			};
		});
	const issue = () => ({
		id: issueId, identifier: 'ENG-1', title: 'Flaky import', description: null, status: 'todo', status_id: statusId,
		status_info: { id: statusId, name: 'Todo', category: 'unstarted', color: '#6366f1', position: 0 }, priority: 3, team_id: teamId,
		project_id: null, cycle_id: null, creator_id: userId, assignee_id: null, creator: user, assignees: [], agent_assignees: assignees(),
		parent_id: null, due_date: null, sort_order: 1000, labels: [], sub_issue_count: 0, sub_issue_done: 0, created_at: createdAt, updated_at: createdAt
	});
	// A second issue assigned to a person, for grouping by assignee.
	const personIssue = () => ({ ...issue(), id: '00000000-0000-0000-0000-000000000102', identifier: 'ENG-2', title: 'Person issue', assignee_id: userId, assignee: user, assignees: [user], agent_assignees: [], sort_order: 2000 });
	await page.routeWebSocket('**/ws', () => {});
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		const path = url.pathname;
		if (!path.startsWith('/api/')) return route.continue();
		if (path === '/api/auth/me') return route.fulfill({ json: user });
		if (path === '/api/preferences')
			return route.fulfill({
				json: {
					font_size: 'default', pointer_cursors: true, theme_mode: 'dark', light_theme: 'light', dark_theme: 'dark', workflow_sort_mode: 'default',
					workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'], team_workflow_sort_overrides: {}, issues_group_by: options.groupBy ?? 'status'
				}
			});
		const workspace = { id: '00000000-0000-0000-0000-000000000002', name: 'Test Workspace', slug: 'test', logo_url: null, current_user_role: role, created_at: createdAt, updated_at: createdAt };
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/teams')
			return route.fulfill({
				json: [teamId, otherTeamId].map((id, index) => ({
					id, name: index === 0 ? 'Engineering' : 'Docs', key: index === 0 ? 'ENG' : 'DOC', description: null, color: '#6366f1', icon: 'layers',
					triage_enabled: false, created_at: createdAt, updated_at: createdAt
				}))
			});
		if (path === '/api/workspaces/test/members') return route.fulfill({ json: [{ user_id: userId, name: 'Test User', email: 'test@example.com', role }] });
		if (path === '/api/workspaces/test/agents' && request.method() === 'GET')
			return route.fulfill({ json: { agents: role === 'guest' ? [] : agents, can_manage: role === 'owner' || role === 'admin', can_assign: role !== 'guest' } });
		const agentUpdate = path.match(/^\/api\/workspaces\/test\/agents\/([^/]+)$/);
		if (agentUpdate && request.method() === 'PUT') {
			const body = request.postDataJSON();
			state.updates.push({ id: agentUpdate[1], body });
			const agent = agents.find((candidate) => candidate.id === agentUpdate[1])!;
			if (body.team_ids) agent.team_ids = body.team_ids;
			return route.fulfill({ status: 204, body: '' });
		}
		if (path === '/api/workspaces/test/issues/ENG-1/agents' && request.method() === 'GET') return route.fulfill({ json: assignees() });
		if (path === '/api/workspaces/test/issues/ENG-1/agents' && request.method() === 'PUT') {
			const ids: string[] = request.postDataJSON().agent_ids;
			state.puts.push(ids);
			if (options.refuse) return route.fulfill({ status: 403, json: { error: { code: options.refuse, message: 'Refused' } } });
			state.assigned = ids.map((id) => state.assigned.find((entry) => entry.id === id) ?? { id, task_state: 'queued', task_reason: 'Waiting for execution' });
			return route.fulfill({ json: assignees() });
		}
		if (path === '/api/workspaces/test/issues' && request.method() === 'GET') {
			state.issueQueries.push(url.search);
			const data = options.withPersonIssue ? [personIssue(), issue()] : [issue()];
			return route.fulfill({ json: { data, total_count: data.length, page: 1, has_more: false } });
		}
		if (/^\/api\/workspaces\/test\/issues\/ENG-[12]$/.test(path) && request.method() === 'PATCH') {
			state.patches.push({ path, body: request.postDataJSON() });
			return route.fulfill({ json: path.endsWith('ENG-2') ? personIssue() : issue() });
		}
		if (path === '/api/workspaces/test/issues/ENG-1') return route.fulfill({ json: issue() });
		if (path === '/api/workspaces/test/issues/ENG-1/comments' && request.method() === 'POST') {
			state.comments.push(request.postDataJSON().body);
			return route.fulfill({ status: 201, json: { id: 'comment-1', issue_id: issueId, user_id: userId, body: request.postDataJSON().body, created_at: createdAt, updated_at: createdAt, user } });
		}
		if (path === '/api/workspaces/test/issues/ENG-1/history')
			return route.fulfill({
				json: [
					{ id: 'h1', issue_id: issueId, user_id: userId, field: 'status', old_value: 'Todo', new_value: 'In Progress', agent: 'codex', agent_name: 'Fixer', created_at: '2026-01-02T00:00:00Z' }
				]
			});
		if (path === '/api/workspaces/test/issues/ENG-1/github') return route.fulfill({ json: { pull_requests: [], branches: [], commits: [] } });
		if (path === '/api/workspaces/test/dev-machines') return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path.match(/^\/api\/workspaces\/test\/teams\/[^/]+\/statuses$/))
			return route.fulfill({
				json: [{ id: statusId, team_id: teamId, name: 'Todo', slug: 'todo', category: 'unstarted', color: '#6366f1', position: 0, is_default: true, project_ids: [], created_at: createdAt, updated_at: createdAt }]
			});
		if (
			['/api/workspaces/test/projects', '/api/workspaces/test/labels', '/api/workspaces/test/views'].includes(path) ||
			path.match(/^\/api\/workspaces\/test\/teams\/[^/]+\/cycles$/) ||
			path.match(/^\/api\/workspaces\/test\/issues\/ENG-1\/(comments|relations|sub-issues|wiki-references)$/)
		)
			return route.fulfill({ json: [] });
		state.unhandled.push(`${request.method()} ${path}`);
		return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND', message: `Unhandled ${path}` } } });
	});
	return state;
}

const agentsUpdated = (page: Page) =>
	page.evaluate((id) => window.dispatchEvent(new CustomEvent('ws:issue-agents-updated', { detail: { issue_id: id } })), issueId);

test('assigning an issue to an agent shows it as assignee with a live status', async ({ page }, testInfo) => {
	const state = await mockWorkspace(page);
	await page.goto('/test/issue/ENG-1');
	await expect(page.getByTestId('issue-agents')).toHaveCount(0);

	await page.getByRole('button', { name: 'Assignee', exact: true }).click();
	// Only agents enabled for the issue's team are offered.
	await expect(page.locator('[data-agent-option="Fixer"]')).toBeVisible();
	await expect(page.locator('[data-agent-option="Docs writer"]')).toHaveCount(0);
	await page.locator('[data-agent-option="Fixer"]').click();
	expect(state.puts).toEqual([[fixerId]]);
	await page.keyboard.press('Escape');

	const row = page.locator('[data-agent-assignee="Fixer"]');
	await expect(row).toBeVisible();
	await expect(row.locator('[data-agent-status]')).toHaveText('Queued');
	await expect(row).toContainText('Acting for Test User');

	// The task moves on: a realtime event reloads the agent's status.
	state.assigned = [{ id: fixerId, task_state: 'running', task_reason: 'Native coding iteration is running' }];
	await agentsUpdated(page);
	await expect(row.locator('[data-agent-status]')).toHaveText('Running');
	await expect(row).toContainText('Native coding iteration is running');
	await page.screenshot({ path: testInfo.outputPath('agent-assignee.png') });

	// History names the agent identity and the member it acted for.
	await expect(page.locator('[data-history-actor]')).toHaveText('by Fixer on behalf of Test User');
	expect(state.unhandled).toEqual([]);
});

test('unassigning a working agent asks before cancelling its task', async ({ page }) => {
	const state = await mockWorkspace(page, { assigned: [{ id: fixerId, task_state: 'running' }] });
	await page.goto('/test/issue/ENG-1');
	const row = page.locator('[data-agent-assignee="Fixer"]');
	await row.getByRole('button', { name: 'Unassign Fixer?' }).click();
	const dialog = page.getByRole('alertdialog');
	await expect(dialog).toContainText('Unassigning cancels its task');
	await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
	expect(state.puts).toEqual([]);
	await expect(row).toBeVisible();

	await row.getByRole('button', { name: 'Unassign Fixer?' }).click();
	await page.getByRole('alertdialog').getByRole('button', { name: 'Unassign and cancel task' }).click();
	await expect(row).toHaveCount(0);
	expect(state.puts).toEqual([[]]);

	// An agent whose pull request awaits review is unassigned without asking:
	// nothing is cancelled.
	state.assigned = [{ id: fixerId, task_state: 'awaiting_review' }];
	await agentsUpdated(page);
	await expect(row.locator('[data-agent-status]')).toHaveText('Awaiting review');
	await row.getByRole('button', { name: 'Unassign Fixer?' }).click();
	await expect(row).toHaveCount(0);
	await expect(page.getByRole('alertdialog')).toHaveCount(0);
});

test('a refused assignment says why and changes nothing', async ({ page }) => {
	const state = await mockWorkspace(page, { refuse: 'AGENT_NOT_ASSIGNABLE' });
	await page.goto('/test/issue/ENG-1');
	await page.getByRole('button', { name: 'Assignee', exact: true }).click();
	await page.locator('[data-agent-option="Fixer"]').click();
	await expect(page.getByText('This agent is not enabled for this team')).toBeVisible();
	expect(state.puts).toEqual([[fixerId]]);
	await expect(page.getByTestId('issue-agents')).toHaveCount(0);
});

test('a guest is offered no agents', async ({ page }) => {
	await mockWorkspace(page, { role: 'guest' });
	await page.goto('/test/issue/ENG-1');
	await page.getByRole('button', { name: 'Assignee', exact: true }).click();
	await expect(page.getByRole('option', { name: /Test User/ })).toBeVisible();
	await expect(page.locator('[data-agent-option]')).toHaveCount(0);
});

test('an agent can be mentioned in a comment', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.goto('/test/issue/ENG-1');
	const editor = page.locator('.ProseMirror').last();
	await editor.click();
	await page.keyboard.type('@Fix');
	await page.locator('[data-mention-agent="Fixer"]').dispatchEvent('pointerdown');
	await page.keyboard.type('why does the import fail?');
	await page.getByRole('button', { name: 'Send (Ctrl+Enter)', exact: true }).click();
	await expect.poll(() => state.comments.length).toBe(1);
	expect(state.comments[0]).toContain(`data-id="${fixerId}"`);
	expect(state.comments[0]).toContain('data-type="mention"');
	// The server tells agent chips from people by this kind.
	expect(state.comments[0]).toContain('data-kind="agent"');
});

test('agents appear in the issue list, the assignee filter and my issues', async ({ page }) => {
	const state = await mockWorkspace(page, { assigned: [{ id: fixerId, task_state: 'running' }] });
	await page.goto(`/test/teams/${teamId}`);
	await expect(page.getByText('Flaky import')).toBeVisible();
	await expect(page.locator('[data-agent-avatar][title="Fixer"]').first()).toBeVisible();

	await page.getByRole('button', { name: 'Filter', exact: true }).click();
	await page.getByRole('button', { name: 'Assignee', exact: true }).click();
	await page.locator('[data-agent-filter="Fixer"]').click();
	await expect.poll(() => state.issueQueries.some((query) => query.includes(`assignee=${fixerId}`))).toBe(true);
	await expect(page.getByRole('button', { name: 'Fixer (agent)' })).toBeVisible();

	await page.goto('/test/my-issues');
	await page.getByRole('tab', { name: 'Assigned to my agents' }).click();
	await expect.poll(() => state.issueQueries.some((query) => query.includes(`agent_assigned_by=${userId}`))).toBe(true);
});

test('administrators choose the teams where an agent can be assigned', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.route('**/api/workspaces/test/dev-machine-policy', (route) =>
		route.fulfill({ json: { workspace_id: 'w', enabled: true, allowed_providers: ['codex'], allow_custom_providers: false, size_options: [], disk_quota_mode: 'monitored' } })
	);
	// The page's other cards load their own data; none of it matters here.
	for (const part of ['agent-model-servers', 'agent-model-defaults', 'agent-sign-ins', 'assistant-skills'])
		await page.route(`**/api/workspaces/test/${part}`, (route) => route.fulfill({ json: [] }));
	await page.route('**/api/workspaces/test/agent-profiles', (route) =>
		route.fulfill({ json: { profiles: [], can_manage: true, harnesses: ['codex'], behaviours: ['plans', 'changes_code', 'reviews'] } })
	);
	await page.goto('/test/settings/agents');
	const card = page.getByTestId('agent-identities');
	await expect(card.locator('[data-agent-identity="Fixer"]')).toContainText('Codex');
	await expect(card.locator('[data-agent-identity="Docs writer"]')).toContainText('No allowed coding agent is available');
	await card.locator('[data-agent-teams="Fixer"]').click();
	await page.getByRole('option', { name: 'Docs' }).click();
	await expect.poll(() => state.updates).toEqual([{ id: fixerId, body: { team_ids: [teamId, otherTeamId] } }]);
	await expect(card.locator('[data-agent-teams="Fixer"]')).toHaveText('2 teams');
});

test('dropping an issue on an agent group neither assigns the agent nor sends its key as a user', async ({ page }) => {
	const state = await mockWorkspace(page, { assigned: [{ id: fixerId, task_state: 'running' }], groupBy: 'assignee', withPersonIssue: true });
	await page.goto(`/test/teams/${teamId}`);
	// The agent-only issue is in the agent's group, not under "Unassigned".
	await expect(page.getByText('Fixer (agent)')).toBeVisible();
	await expect(page.getByText('Unassigned', { exact: true })).toHaveCount(0);

	// Person's group -> agent's group: nothing is sent.
	await page.getByText('Person issue', { exact: true }).dragTo(page.getByText('Flaky import', { exact: true }));
	// Agent's group -> person's group: the person is assigned. This also
	// proves drops are live, so the empty list above is not a dead drag.
	await page.getByText('Flaky import', { exact: true }).dragTo(page.getByText('Person issue', { exact: true }));
	await expect.poll(() => state.patches.length).toBe(1);
	expect(state.patches[0].path).toMatch(/\/issues\/ENG-1$/);
	expect(state.patches[0].body.assignee_ids).toEqual([userId]);
	expect(state.puts).toEqual([]);
});
