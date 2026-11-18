import { expect, test, type Page } from '@playwright/test';

const teamA = '11111111-1111-4111-8111-111111111111';
const teamB = '22222222-2222-4222-8222-222222222222';

const options = {
	teams: [
		{ id: teamA, name: 'Engineering', key: 'ENG', is_private: false },
		{ id: teamB, name: 'Security', key: 'SEC', is_private: true }
	],
	statuses: {
		[teamA]: [
			{ id: 'a-todo', name: 'Todo', category: 'unstarted' },
			{ id: 'a-doing', name: 'Doing', category: 'started' }
		],
		[teamB]: [
			{ id: 'b-todo', name: 'Triage', category: 'unstarted' },
			{ id: 'b-doing', name: 'Investigating', category: 'started' }
		]
	},
	members: [
		{ user_id: 'user-1', email: 'owner@example.test', display_name: 'Owner' },
		{ user_id: 'user-2', email: 'jane@example.test', display_name: 'Jane' }
	]
};

const sourcePreview = {
	counts: { issues: 250, comments: 12 },
	statuses: ['In Progress'],
	priorities: ['High'],
	labels: ['backend'],
	users: [{ external_id: 'acc-9', email: '', display_name: 'Hidden Email' }],
	projects: [
		{ external_id: '10', name: 'Web', key: 'WEB' },
		{ external_id: '20', name: 'Ops', key: 'OPS' }
	],
	unmapped_fields: ['custom field: customfield_10050'],
	mapping_gaps: ['project: Ops (OPS)', 'project: Web (WEB)', 'status: In Progress', 'user: Hidden Email']
};

type Job = Record<string, unknown> & { id: string; status: string };

function newJob(): Job {
	return {
		id: 'job-1',
		source: 'jira',
		source_site: 'acme.atlassian.net',
		status: 'draft',
		config: { cloud: true },
		mapping: { projects: {}, statuses: {}, users: {}, priorities: {}, epic_mode: 'project', sprint_mode: 'cycle' },
		preview: null,
		total: 0,
		processed: 0,
		created_count: 0,
		updated_count: 0,
		skipped_count: 0,
		failed_count: 0,
		error: null,
		cancel_requested: false,
		created_at: '2026-11-17T10:00:00Z',
		updated_at: '2026-11-17T10:00:00Z',
		started_at: null,
		finished_at: null
	};
}

const reportItems = Array.from({ length: 250 }, (_, index) => ({
	external_type: 'issue',
	external_id: String(10000 + index),
	external_key: `WEB-${index + 1}`,
	outcome: 'failed',
	reason: 'no project mapping',
	created_at: '2026-11-17T10:05:00Z'
}));

interface Recorded {
	creates: unknown[];
	mappings: Array<Record<string, any>>;
	itemQueries: string[];
}

async function mockApi(
	page: Page,
	role: 'owner' | 'admin' | 'member',
	recorded: Recorded,
	previewFailure?: { status: number; code: string; message: string }
) {
	const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: role };
	let job: Job | null = null;
	let polls = 0;
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		const path = url.pathname;
		const json = (body: unknown, status = 200) => route.fulfill({ status, json: body });
		if (path === '/api/auth/me')
			return json({ id: 'user-1', name: 'Owner', display_name: 'Owner', email: 'owner@example.test', avatar_url: null });
		if (path === '/api/workspaces') return json([workspace]);
		if (path === '/api/workspaces/test') return json(workspace);
		if (path === '/api/notifications') return json({ notifications: [], unread_count: 0 });
		if (path === '/api/workspaces/test/teams') return json([]);
		const base = '/api/workspaces/test/imports';
		if (path === base && request.method() === 'GET') return json({ imports: job ? [job] : [] });
		if (path === base && request.method() === 'POST') {
			recorded.creates.push(request.postDataJSON());
			job = newJob();
			return json(job, 201);
		}
		if (path === `${base}/options`) return json(options);
		if (job && path === `${base}/${job.id}/preview`) {
			if (previewFailure)
				return json({ error: { code: previewFailure.code, message: previewFailure.message } }, previewFailure.status);
			job = { ...job, status: 'previewed', preview: sourcePreview };
			return json(sourcePreview);
		}
		if (job && path === `${base}/${job.id}/mapping`) {
			const { mapping } = request.postDataJSON();
			recorded.mappings.push(mapping);
			// The server recomputes the gaps against the saved mapping.
			job = { ...job, mapping, preview: { ...sourcePreview, mapping_gaps: ['status: In Progress'] } };
			return json(job);
		}
		if (job && path === `${base}/${job.id}/start`) {
			job = { ...job, status: 'queued', total: 250 };
			return json(job);
		}
		if (job && path === `${base}/${job.id}/items`) {
			recorded.itemQueries.push(url.search);
			const offset = Number(url.searchParams.get('offset') ?? 0);
			const limit = Number(url.searchParams.get('limit') ?? 200);
			const items = reportItems.slice(offset, offset + limit);
			return json({ items, has_more: offset + items.length < reportItems.length });
		}
		if (job && path === `${base}/${job.id}`) {
			if (job.status === 'queued' || job.status === 'running') {
				polls += 1;
				job =
					polls === 1
						? { ...job, status: 'running', processed: 120 }
						: { ...job, status: 'failed', processed: 250, created_count: 0, failed_count: 250, error: null };
			}
			return json(job);
		}
		return json([]);
	});
}

async function choose(page: Page, label: string, option: string) {
	await page.getByRole('combobox', { name: label, exact: true }).click();
	await page.getByRole('option', { name: option, exact: true }).click();
}

test('an admin previews Jira, maps two projects to two teams and pages through the report', async ({ page }) => {
	const recorded: Recorded = { creates: [], mappings: [], itemQueries: [] };
	await mockApi(page, 'admin', recorded);
	await page.goto('/test/settings/import');

	await expect(page.getByRole('heading', { name: 'Import issues' })).toBeVisible();
	await page.getByLabel('Site URL').fill('https://acme.atlassian.net');
	await page.getByLabel('Email').fill('owner@example.test');
	await page.getByLabel('API token').fill('jira-token');
	await page.getByRole('button', { name: 'Connect', exact: true }).click();
	await expect.poll(() => recorded.creates.length).toBe(1);
	expect(recorded.creates[0]).toMatchObject({
		source: 'jira',
		config: { cloud: true },
		credentials: { base_url: 'https://acme.atlassian.net', email: 'owner@example.test', token: 'jira-token' }
	});

	await page.getByRole('button', { name: 'Run preview' }).click();
	// The preview read back from the job is in the API's shape: counts and
	// the unmapped list render instead of crashing the page.
	await expect(page.getByText('issues: 250')).toBeVisible();
	const unmapped = page.getByTestId('import-unmapped');
	await expect(unmapped).toContainText('custom field: customfield_10050');
	await expect(unmapped).toContainText('project: Ops (OPS)');

	await choose(page, 'Source project: Web (WEB)', 'Engineering');
	await choose(page, 'Source project: Ops (OPS)', 'Security');
	// Each mapped team gets its own status choices.
	await choose(page, 'Source status: In Progress · Engineering', 'Doing');
	await choose(page, 'Source status: In Progress · Security', 'Investigating');
	// A user whose email Jira hides can still be mapped, by source id.
	await choose(page, 'Source user: Hidden Email', 'Jane');
	await choose(page, 'Source priority: High', 'High');
	await page.getByRole('button', { name: 'Save mapping' }).click();

	await expect.poll(() => recorded.mappings.length).toBe(1);
	expect(recorded.mappings[0]).toMatchObject({
		projects: { '10': { team_id: teamA, project_id: null }, '20': { team_id: teamB, project_id: null } },
		statuses: {
			[`${teamA}:in progress`]: { team_id: teamA, status_id: 'a-doing' },
			[`${teamB}:in progress`]: { team_id: teamB, status_id: 'b-doing' }
		},
		users: { 'acc-9': { user_id: 'user-2' } },
		priorities: { high: 2 }
	});
	expect(recorded.mappings[0].users).not.toHaveProperty('');
	// The saved mapping's gaps replace the ones from the first preview.
	await expect(unmapped).not.toContainText('project: Ops (OPS)');
	await expect(unmapped).toContainText('status: In Progress');

	await page.getByRole('button', { name: 'Start import' }).click();
	await expect(page.getByText('Progress', { exact: true })).toBeVisible();

	// The finished job's report shows Jira keys and pages with Load more.
	const rows = page.getByTestId('import-item');
	await expect(rows).toHaveCount(200, { timeout: 15_000 });
	await expect(rows.first()).toContainText('WEB-1');
	await page.getByRole('button', { name: 'Load more' }).click();
	await expect(rows).toHaveCount(250);
	await expect(page.getByRole('button', { name: 'Load more' })).toHaveCount(0);
	expect(recorded.itemQueries).toEqual(['?offset=0&limit=200', '?offset=200&limit=200']);
});

test('a failed Jira call during preview shows the server explanation', async ({ page }) => {
	const recorded: Recorded = { creates: [], mappings: [], itemQueries: [] };
	const message = 'Jira rejected the credentials (HTTP 401). Check the email and API token and that the account can browse the projects.';
	await mockApi(page, 'owner', recorded, { status: 502, code: 'IMPORT_SOURCE_FAILED', message });
	await page.goto('/test/settings/import');

	await page.getByLabel('Site URL').fill('https://acme.atlassian.net');
	await page.getByLabel('API token').fill('wrong-token');
	await page.getByRole('button', { name: 'Connect', exact: true }).click();
	await page.getByRole('button', { name: 'Run preview' }).click();
	await expect(page.getByRole('alert')).toContainText(message);
	await expect(page.getByRole('button', { name: 'Start import' })).toHaveCount(0);
});

test('a member sees the admin-only note and no import controls', async ({ page }) => {
	await mockApi(page, 'member', { creates: [], mappings: [], itemQueries: [] });
	await page.goto('/test/settings/import');
	await expect(page.getByText('Only workspace owners and admins can run imports.')).toBeVisible();
	await expect(page.getByRole('button', { name: 'Connect', exact: true })).toHaveCount(0);
});
