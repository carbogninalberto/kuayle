import { test, expect, type Page } from '@playwright/test';

type Version = {
	skill_id: string;
	version: number;
	name: string;
	description: string;
	body: string;
	status: 'draft' | 'active';
	author_kind: 'user' | 'assistant';
	author_user_id: string | null;
	created_at: string;
};

const at = '2026-10-20T09:30:00Z';
const teams = [
	{ id: 'team-eng', name: 'Engineering', key: 'ENG', is_private: false },
	{ id: 'team-sec', name: 'Security', key: 'SEC', is_private: true }
];

function seed() {
	const version = (skill_id: string, n: number, status: Version['status'], body: string, author_kind: Version['author_kind'] = 'user'): Version => ({
		skill_id,
		version: n,
		name: skill_id === 'skill-release' ? 'How we release' : 'How we triage',
		description: skill_id === 'skill-release' ? 'Use when asked how to cut a release.' : 'Use when sorting new issues.',
		body,
		status,
		author_kind,
		author_user_id: author_kind === 'user' ? 'user-1' : null,
		created_at: at
	});
	const versions: Record<string, Version[]> = {
		'skill-release': [
			version('skill-release', 3, 'draft', '## Steps\n\n1. Bump the version\n2. Run the checks\n3. Tag the release', 'assistant'),
			version('skill-release', 2, 'active', '## Steps\n\n1. Bump the version\n2. Tag the release'),
			version('skill-release', 1, 'active', 'Tag it.')
		],
		'skill-triage': [version('skill-triage', 1, 'draft', 'Label every issue.', 'assistant')]
	};
	const skills = [
		{
			id: 'skill-release',
			workspace_id: 'workspace-1',
			team_id: null as string | null,
			team_key: null as string | null,
			team_name: null as string | null,
			name: 'How we release',
			description: 'Use when asked how to cut a release.',
			body: versions['skill-release'][1].body,
			enabled: true,
			status: 'active',
			current_version: 2,
			latest_version: 3,
			pending_version: 3 as number | null,
			created_by_user_id: 'user-1',
			author_kind: 'user',
			created_at: at,
			updated_at: at
		},
		{
			id: 'skill-triage',
			workspace_id: 'workspace-1',
			team_id: 'team-eng' as string | null,
			team_key: 'ENG' as string | null,
			team_name: 'Engineering' as string | null,
			name: 'How we triage',
			description: 'Use when sorting new issues.',
			body: 'Label every issue.',
			enabled: true,
			status: 'draft',
			current_version: 1,
			latest_version: 1,
			pending_version: null as number | null,
			created_by_user_id: 'user-1',
			author_kind: 'assistant',
			created_at: at,
			updated_at: at
		}
	];
	return { skills, versions };
}

async function fixture(page: Page, role: 'admin' | 'member') {
	const { skills, versions } = seed();
	const state = { skills, versions, writes: [] as Array<{ method: string; path: string; body: unknown }> };
	const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: role };
	const base = '/api/workspaces/test/assistant-skills';
	const strip = ({ body: _body, ...skill }: (typeof skills)[number]) => skill;
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const method = request.method();
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user-1', name: 'Ada', display_name: 'Ada', email: 'ada@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: teams });
		if (!path.startsWith(base)) return route.fulfill({ json: [] });
		if (method !== 'GET') {
			state.writes.push({ method, path: path.slice(base.length), body: request.postData() ? request.postDataJSON() : null });
			if (role !== 'admin') return route.fulfill({ status: 403, json: { error: { code: 'FORBIDDEN', message: 'Forbidden' } } });
		}
		if (path === base && method === 'GET')
			return route.fulfill({ json: { skills: state.skills.map(strip), can_manage: role === 'admin' } });
		if (path === base && method === 'POST') {
			const input = request.postDataJSON();
			const team = teams.find((entry) => entry.id === input.team_id);
			const created = {
				...state.skills[0],
				id: 'skill-new',
				team_id: team?.id ?? null,
				team_key: team?.key ?? null,
				team_name: team?.name ?? null,
				name: input.name,
				description: input.description,
				body: input.body,
				status: input.status ?? 'active',
				current_version: 1,
				latest_version: 1,
				pending_version: null
			};
			state.skills.push(created);
			state.versions['skill-new'] = [
				{ skill_id: 'skill-new', version: 1, name: input.name, description: input.description, body: input.body, status: created.status, author_kind: 'user', author_user_id: 'user-1', created_at: at }
			];
			return route.fulfill({ status: 201, json: created });
		}
		const match = path.slice(base.length).match(/^\/([\w-]+)(\/.*)?$/);
		const skill = state.skills.find((entry) => entry.id === match?.[1]);
		if (!match || !skill) return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND', message: 'Not found' } } });
		const rest = match[2] ?? '';
		if (rest === '' && method === 'GET') return route.fulfill({ json: skill });
		if (rest === '/versions') return route.fulfill({ json: { versions: state.versions[skill.id] } });
		if (rest === '/enabled') {
			skill.enabled = request.postDataJSON().enabled;
			return route.fulfill({ json: strip(skill) });
		}
		if (rest === '/activate') {
			const draft = state.versions[skill.id].find((entry) => entry.version === (request.postDataJSON().version ?? skill.latest_version))!;
			draft.status = 'active';
			Object.assign(skill, { body: draft.body, status: 'active', current_version: draft.version, pending_version: null });
			return route.fulfill({ json: skill });
		}
		const restore = rest.match(/^\/versions\/(\d+)\/restore$/);
		if (restore) {
			const old = state.versions[skill.id].find((entry) => entry.version === Number(restore[1]))!;
			const next = skill.latest_version + 1;
			state.versions[skill.id].unshift({ ...old, version: next, status: skill.status as Version['status'], author_kind: 'user' });
			Object.assign(skill, { body: old.body, current_version: next, latest_version: next, pending_version: null });
			return route.fulfill({ json: skill });
		}
		if (rest === '' && method === 'PUT') {
			const input = request.postDataJSON();
			const next = skill.latest_version + 1;
			const status = input.status ?? skill.status;
			state.versions[skill.id].unshift({ skill_id: skill.id, version: next, name: input.name, description: input.description, body: input.body, status, author_kind: 'user', author_user_id: 'user-1', created_at: at });
			skill.latest_version = next;
			if (skill.status === 'active' && status === 'draft') skill.pending_version = next;
			else Object.assign(skill, { name: input.name, description: input.description, body: input.body, status, current_version: next, pending_version: null });
			return route.fulfill({ json: skill });
		}
		if (rest === '' && method === 'DELETE') {
			state.skills = state.skills.filter((entry) => entry.id !== skill.id);
			return route.fulfill({ status: 204 });
		}
		return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND', message: 'Not found' } } });
	});
	return state;
}

const noHorizontalScroll = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);

test('admins list, filter, create, edit, activate, restore and delete skills', async ({ page }) => {
	const state = await fixture(page, 'admin');
	await page.goto('/test/settings/assistant?tab=skills');
	await expect(page.getByRole('tab', { name: 'Skills', exact: true })).toHaveAttribute('aria-selected', 'true');
	const list = page.getByRole('list', { name: 'Skills' });
	await expect(list.getByRole('listitem')).toHaveCount(2);
	const release = list.getByRole('listitem', { name: 'How we release' });
	await expect(release.getByText('Workspace', { exact: true })).toBeVisible();
	await expect(release.getByText('Active', { exact: true })).toBeVisible();
	await expect(release.getByText('v2', { exact: true })).toBeVisible();
	await expect(release.getByText('Draft v3 pending')).toBeVisible();
	await expect(release.getByText('By a person')).toBeVisible();
	await expect(release.getByText('Use when asked how to cut a release.')).toBeVisible();
	const triage = list.getByRole('listitem', { name: 'How we triage' });
	await expect(triage.getByText('Engineering', { exact: true })).toBeVisible();
	await expect(triage.getByText('Draft', { exact: true })).toBeVisible();
	await expect(triage.getByText('By the assistant')).toBeVisible();

	// Scope filter.
	await page.getByRole('combobox', { name: 'Filter by scope' }).click();
	await page.getByRole('option', { name: 'Engineering' }).click();
	await expect(list.getByRole('listitem')).toHaveCount(1);
	await expect(list.getByRole('listitem', { name: 'How we triage' })).toBeVisible();
	await page.getByRole('combobox', { name: 'Filter by scope' }).click();
	await page.getByRole('option', { name: 'All scopes' }).click();
	await expect(list.getByRole('listitem')).toHaveCount(2);

	// Enable switch.
	await page.getByRole('switch', { name: 'Enable How we release' }).click();
	await expect.poll(() => state.writes.at(-1)).toEqual({ method: 'PUT', path: '/skill-release/enabled', body: { enabled: false } });
	await expect(page.getByRole('switch', { name: 'Enable How we release' })).not.toBeChecked();

	// Create a team skill for a private team the admin can see, with a preview.
	await page.getByRole('button', { name: 'New skill' }).click();
	const create = page.getByRole('dialog', { name: 'New skill' });
	await create.getByRole('combobox', { name: 'Scope' }).click();
	await page.getByRole('option', { name: 'Security' }).click();
	await create.getByRole('textbox', { name: 'Name' }).fill('On-call');
	await create.getByRole('textbox', { name: 'Description' }).fill('Use for incidents.');
	await create.getByRole('textbox', { name: 'Instructions (Markdown)' }).fill('# Pager\n\nCall **ops**.\n\n<script>window.__xss=1</script>');
	await create.getByRole('tab', { name: 'Preview' }).click();
	await expect(create.getByRole('heading', { name: 'Pager' })).toBeVisible();
	await expect(create.locator('strong', { hasText: 'ops' })).toBeVisible();
	await create.getByRole('button', { name: 'Create', exact: true }).click();
	await expect.poll(() => state.writes.at(-1)).toEqual({
		method: 'POST',
		path: '',
		body: { team_id: 'team-sec', name: 'On-call', description: 'Use for incidents.', body: '# Pager\n\nCall **ops**.\n\n<script>window.__xss=1</script>', status: 'active' }
	});
	await expect(create).toBeHidden();
	await expect(page.getByRole('heading', { name: 'On-call', level: 2 })).toBeVisible();
	expect(await page.evaluate(() => (window as unknown as { __xss?: number }).__xss)).toBeUndefined();
	await page.getByRole('button', { name: 'Back to skills' }).click();
	await expect(list.getByRole('listitem')).toHaveCount(3);

	// Open a skill: the URL deep-links to it.
	await list.getByRole('button', { name: 'How we release' }).click();
	await expect(page).toHaveURL(/tab=skills&skill=skill-release/);
	await expect(page.getByRole('heading', { name: 'How we release', level: 2 })).toBeVisible();
	await expect(page.getByText('Draft version 3 is not active until it is activated.')).toBeVisible();

	// Review the pending draft against the active text, then activate it.
	await page.getByRole('button', { name: 'Review draft' }).click();
	const review = page.getByRole('dialog', { name: 'Activate draft version 3?' });
	await expect(review.getByText('2. Run the checks')).toBeVisible();
	await expect(review.getByLabel('Added').filter({ hasText: '2. Run the checks' })).toBeVisible();
	await expect(review.getByLabel('Removed').filter({ hasText: '2. Tag the release' })).toBeVisible();
	await review.getByRole('button', { name: 'Activate', exact: true }).click();
	await expect.poll(() => state.writes.at(-1)).toEqual({ method: 'POST', path: '/skill-release/activate', body: { version: 3 } });
	await expect(review).toBeHidden();
	await expect(page.getByText('Draft version 3 is not active until it is activated.')).toBeHidden();

	// Edit as a draft of the active skill.
	await page.getByRole('button', { name: 'Edit', exact: true }).click();
	const edit = page.getByRole('dialog', { name: 'Edit How we release' });
	await expect(edit.getByRole('textbox', { name: 'Instructions (Markdown)' })).toHaveValue(/Run the checks/);
	await edit.getByRole('textbox', { name: 'Description' }).fill('Use when releasing.');
	await edit.getByRole('button', { name: 'Save as draft' }).click();
	await expect.poll(() => state.writes.at(-1)).toMatchObject({
		method: 'PUT',
		path: '/skill-release',
		body: { name: 'How we release', description: 'Use when releasing.', status: 'draft' }
	});
	await expect(edit).toBeHidden();
	await expect(page.getByText('Draft version 4 is not active until it is activated.')).toBeVisible();

	// Restore version 1 from the history.
	const history = page.getByRole('list', { name: 'Version history' });
	await expect(history.getByRole('listitem')).toHaveCount(4);
	const first = history.getByRole('listitem', { name: 'Version 1' });
	await first.getByRole('button', { name: 'Show text' }).click();
	await expect(first.getByText('Tag it.')).toBeVisible();
	await first.getByRole('button', { name: 'Restore' }).click();
	await expect.poll(() => state.writes.at(-1)).toEqual({ method: 'POST', path: '/skill-release/versions/1/restore', body: null });
	await expect(history.getByRole('listitem')).toHaveCount(5);
	await page.screenshot({ path: test.info().outputPath('assistant-skill-detail.png'), fullPage: true });

	// Delete with confirmation.
	await page.getByRole('button', { name: 'Delete', exact: true }).click();
	const confirm = page.getByRole('alertdialog');
	await expect(confirm.getByText('Delete How we release?')).toBeVisible();
	await confirm.getByRole('button', { name: 'Delete', exact: true }).click();
	await expect.poll(() => state.writes.at(-1)).toEqual({ method: 'DELETE', path: '/skill-release', body: null });
	await expect(list.getByRole('listitem')).toHaveCount(2);
	await expect(list.getByRole('listitem', { name: 'How we release' })).toHaveCount(0);
	await expect(page).not.toHaveURL(/skill=/);
});

test('members read skills without write controls', async ({ page }) => {
	const state = await fixture(page, 'member');
	await page.goto('/test/settings/assistant?tab=skills');
	await expect(page.getByText('Only workspace owners and admins can write, activate or delete skills.')).toBeVisible();
	const list = page.getByRole('list', { name: 'Skills' });
	await expect(list.getByRole('listitem')).toHaveCount(2);
	await expect(page.getByRole('button', { name: 'New skill' })).toHaveCount(0);
	await expect(page.getByRole('switch')).toHaveCount(0);
	await expect(list.getByRole('listitem', { name: 'How we release' }).getByText('Enabled', { exact: true })).toBeVisible();
	await list.getByRole('button', { name: 'How we release' }).click();
	await expect(page.getByRole('heading', { name: 'Steps' })).toBeVisible();
	await expect(page.getByRole('list', { name: 'Version history' }).getByRole('listitem')).toHaveCount(3);
	for (const name of ['Edit', 'Delete', 'Review draft', 'Restore', 'Activate'])
		await expect(page.getByRole('button', { name, exact: true })).toHaveCount(0);
	expect(state.writes).toEqual([]);
});

test('a deep link opens one skill and both views fit a phone', async ({ page }) => {
	await fixture(page, 'admin');
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/settings/assistant?tab=skills&skill=skill-triage');
	await expect(page.getByRole('heading', { name: 'How we triage', level: 2 })).toBeVisible();
	await expect(page.getByText('This draft is not active until it is activated.')).toBeVisible();
	await expect(page.getByText('Label every issue.')).toBeVisible();
	expect(await noHorizontalScroll(page)).toBe(true);
	await page.getByRole('button', { name: 'Review draft' }).click();
	await expect(page.getByRole('dialog', { name: 'Activate draft version 1?' }).getByText('Label every issue.')).toBeVisible();
	expect(await noHorizontalScroll(page)).toBe(true);
	await page.keyboard.press('Escape');
	await page.getByRole('button', { name: 'Back to skills' }).click();
	await expect(page.getByRole('list', { name: 'Skills' }).getByRole('listitem')).toHaveCount(2);
	expect(await noHorizontalScroll(page)).toBe(true);
	await page.screenshot({ path: test.info().outputPath('assistant-skills-phone.png'), fullPage: true });
});
