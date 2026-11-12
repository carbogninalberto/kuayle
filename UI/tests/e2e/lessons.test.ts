import { test, expect, type Page } from '@playwright/test';

// Settings → Lessons (KUA-134): the workspace memory. Proposals wait in one
// place; only approved lessons are in a repository's list. Served from
// mocked API routes that keep the lessons the way the server does.
const time = '2026-11-10T10:00:00Z';
const shots = process.env.KUA134_SHOTS;

type Lesson = Record<string, any>;
const lesson = (id: string, repository: string, state: string, text: string, over: Lesson = {}): Lesson => ({
	id,
	repository,
	team_id: null,
	state,
	text,
	source_event: 'task',
	proposer: 'agent',
	source_task_id: null,
	proposed_by: null,
	decided_by: null,
	decided_at: null,
	position: 0,
	created_at: time,
	updated_at: time,
	...over
});
const seed = (): Lesson[] => [
	lesson('a1', 'acme/api', 'approved', 'Run make check before pushing', { position: 1, source_event: 'manual', proposer: 'person', proposed_by: 'user-1' }),
	lesson('a2', 'acme/api', 'approved', 'Migration numbers must follow main', { position: 2 }),
	lesson('a3', 'acme/api', 'approved', 'Never edit generated files', { position: 3 }),
	lesson('w1', 'acme/web', 'approved', 'Use the shared Button', { position: 1 }),
	lesson('p1', 'acme/api', 'proposed', 'run the linter', { source_task_id: 'task-9', proposed_text: 'run the linter' }),
	lesson('p2', 'acme/web', 'proposed', 'Keep strings in the catalogs', { source_event: 'review', proposer: 'external', source_task_id: 'task-7', proposed_text: 'keep strings in catalogs pls' }),
	lesson('p3', 'acme/api', 'proposed', 'Ask before adding a dependency', { source_event: 'manual', proposer: 'person', proposed_by: 'user-2' }),
	lesson('p4', 'acme/api', 'proposed', 'A private proposal of someone else', { source_event: 'manual', proposer: 'person', proposed_by: 'user-3' }),
	lesson('r1', 'acme/api', 'retired', 'An old lesson', { position: 9 })
];

async function fixture(page: Page, role: 'owner' | 'member') {
	const me = role === 'owner' ? 'user-1' : 'user-2';
	const state = { lessons: seed(), calls: [] as string[], refuse: '' };
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request(),
			url = new URL(request.url()),
			path = url.pathname,
			method = request.method();
		const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: role };
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: me, name: role === 'owner' ? 'Olga Owner' : 'Mia Member', email: `${role}@example.test` } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/members')
			return route.fulfill({
				json: [
					{ user_id: 'user-1', name: 'Olga Owner', email: 'owner@example.test', role: 'owner', created_at: time },
					{ user_id: 'user-2', name: 'Mia Member', email: 'member@example.test', role: 'member', created_at: time }
				]
			});
		if (path === '/api/workspaces/test/github/status')
			return route.fulfill({
				json: {
					configured: true,
					installed: true,
					global_app: false,
					repos: ['acme/api', 'acme/web'].map((full_name, i) => ({ id: `repo-${i}`, github_repo_id: i, full_name, default_branch: 'main', is_active: true }))
				}
			});
		// The owner created task-9; nobody here can open task-7.
		if (path === '/api/workspaces/test/agent-tasks/task-9' && role === 'owner')
			return route.fulfill({ json: { id: 'task-9', conversation_id: 'chat-9', creator_id: 'user-1' } });
		if (path.startsWith('/api/workspaces/test/agent-tasks/'))
			return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });

		const base = '/api/workspaces/test/memory/lessons';
		if (!path.startsWith(base)) return route.fulfill({ json: [] });
		const manage = role === 'owner';
		// Members read approved lessons and their own proposals, without the
		// text before an edit.
		const visible = (item: Lesson) =>
			manage || item.state === 'approved' || (item.state === 'proposed' && item.proposed_by === me);
		const shown = (item: Lesson) => (manage ? item : { ...item, proposed_text: undefined });
		const approved = (repository: string) =>
			state.lessons.filter((item) => item.repository === repository && item.state === 'approved').sort((a, b) => a.position - b.position);
		const limits = { lesson_characters: 400, approved_per_repository: 30, prompt_bytes: 4000 };
		if (path === base && method === 'GET') {
			const wanted = url.searchParams.get('state');
			return route.fulfill({
				json: {
					lessons: state.lessons
						.filter((item) => visible(item) && (!wanted || item.state === wanted))
						.sort((a, b) => a.repository.localeCompare(b.repository) || a.position - b.position)
						.map(shown),
					can_manage: manage,
					limits
				}
			});
		}
		const body = method === 'GET' ? {} : (request.postDataJSON() ?? {});
		state.calls.push(`${method} ${path.slice(base.length) || '/'} ${JSON.stringify(body)}`);
		if (state.refuse)
			return route.fulfill({ status: 409, json: { error: { code: 'LESSON_LIMIT', message: state.refuse } } });
		if (path === base && method === 'POST') {
			const created = lesson(`n${state.lessons.length}`, body.repository, manage ? 'approved' : 'proposed', body.text, {
				source_event: 'manual',
				proposer: 'person',
				proposed_by: me,
				proposed_text: body.text,
				position: manage ? approved(body.repository).length + 1 : 0
			});
			state.lessons.push(created);
			return route.fulfill({ status: 201, json: shown(created) });
		}
		if (!manage) return route.fulfill({ status: 403, json: { error: { code: 'FORBIDDEN', message: 'Forbidden' } } });
		if (path === `${base}/order` && method === 'PUT') {
			body.ids.forEach((id: string, index: number) => (state.lessons.find((item) => item.id === id)!.position = index + 1));
			return route.fulfill({ json: { lessons: approved(body.repository) } });
		}
		const [, id, action] = path.slice(base.length).match(/^\/([^/]+)(?:\/(\w+))?$/) ?? [];
		const item = state.lessons.find((entry) => entry.id === id);
		if (!item) return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
		if (action === 'approve') {
			item.state = 'approved';
			if (body.text) item.text = body.text;
			item.position = approved(item.repository).length;
		} else if (action === 'drop') item.state = 'dropped';
		else if (action === 'retire') item.state = 'retired';
		else if (method === 'PUT') item.text = body.text;
		return route.fulfill({ json: item });
	});
	return state;
}

const sizes = [
	{ name: 'desktop', width: 1280, height: 900 },
	{ name: 'phone', width: 390, height: 844 }
];
const fits = (page: Page) =>
	page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth);

for (const size of sizes) {
	test.describe(size.name, () => {
		test.beforeEach(async ({ page }) => {
			await page.setViewportSize({ width: size.width, height: size.height });
		});

		test('an admin decides proposals and keeps the lessons of each repository', async ({ page }) => {
			const state = await fixture(page, 'owner');
			await page.goto('/test/settings/lessons');
			await expect(page.getByRole('heading', { name: 'Lessons', level: 1 })).toBeVisible();
			const proposals = page.getByRole('region', { name: 'Proposals' });
			const rows = proposals.getByRole('listitem');
			await expect(rows).toHaveCount(4);
			await expect(proposals.getByRole('heading')).toContainText('4');
			const api = page.getByRole('region', { name: 'acme/api' });
			const web = page.getByRole('region', { name: 'acme/web' });
			// Nothing is a lesson until approved: a proposal is in no repository's list.
			await expect(api.getByRole('listitem')).toHaveText([
				/Run make check before pushing/,
				/Migration numbers must follow main/,
				/Never edit generated files/
			]);
			await expect(api).not.toContainText('run the linter');
			// Retired lessons are gone.
			await expect(page.getByText('An old lesson')).toHaveCount(0);

			// Where each proposal came from, quietly: a link to the task the
			// reader can open, plain words otherwise.
			const linter = rows.filter({ hasText: 'run the linter' });
			await expect(linter.getByRole('link', { name: 'Written by an agent' })).toHaveAttribute(
				'href',
				'/test/assistant?conversation=chat-9&agent=task-9'
			);
			const strings = rows.filter({ hasText: 'Keep strings in the catalogs' });
			await expect(strings).toContainText('From a GitHub review');
			await expect(strings.getByRole('link')).toHaveCount(0);
			await expect(rows.filter({ hasText: 'Ask before adding' })).toContainText('Proposed by Mia Member');
			expect(await fits(page)).toBe(true);
			if (shots)
				await page.screenshot({
					path: `${shots}/${size.name === 'desktop' ? '3-lessons-admin-desktop' : '4-lessons-admin-phone'}.png`,
					fullPage: true
				});
			// The text before a person edited it is one click away.
			await strings.getByRole('button', { name: 'Edited' }).click();
			await expect(page.getByText('keep strings in catalogs pls')).toBeVisible();
			await page.keyboard.press('Escape');
			await expect(linter.getByRole('button', { name: 'Edited' })).toHaveCount(0);
			// Approving can edit the text first.
			await linter.getByRole('button', { name: 'Edit' }).click();
			const dialog = page.getByRole('dialog', { name: 'Approve' });
			await expect(dialog).toContainText('acme/api');
			await dialog.getByRole('textbox', { name: 'Lesson' }).fill('Run npm run lint before every commit');
			await dialog.getByRole('button', { name: 'Approve' }).click();
			await expect(dialog).toHaveCount(0);
			await expect(rows).toHaveCount(3);
			await expect(api.getByRole('listitem')).toHaveCount(4);
			await expect(api.getByRole('listitem').last()).toContainText('Run npm run lint before every commit');
			expect(state.calls.at(-1)).toBe('POST /p1/approve {"text":"Run npm run lint before every commit"}');

			// Approving as it is.
			await strings.getByRole('button', { name: 'Approve' }).click();
			await expect(web.getByRole('listitem')).toHaveText([/Use the shared Button/, /Keep strings in the catalogs/]);
			expect(state.calls.at(-1)).toBe('POST /p2/approve {}');

			// Dropping asks once.
			const other = rows.filter({ hasText: 'A private proposal' });
			await other.getByRole('button', { name: 'Drop' }).click();
			const confirm = page.getByRole('alertdialog', { name: 'Drop this proposal?' });
			await expect(confirm).toContainText('A private proposal of someone else');
			await confirm.getByRole('button', { name: 'Drop' }).click();
			await expect(rows).toHaveCount(1);
			await expect(page.getByText('A private proposal of someone else')).toHaveCount(0);

			// An approved lesson has one control: its menu.
			const menu = (text: string) =>
				api.getByRole('listitem').filter({ hasText: text }).getByRole('button', { name: 'More actions' });
			await expect(api.getByRole('listitem').first().getByRole('button')).toHaveCount(1);
			await menu('Never edit generated files').click();
			await page.getByRole('menuitem', { name: 'Move up' }).click();
			await expect(api.getByRole('listitem')).toHaveText([
				/Run make check/,
				/Never edit generated files/,
				/Migration numbers/,
				/Run npm run lint/
			]);
			expect(state.calls.at(-1)).toBe('PUT /order {"repository":"acme/api","ids":["a1","a3","a2","p1"]}');
			// The first cannot move up, the last not down.
			await menu('Run make check').click();
			await expect(page.getByRole('menuitem', { name: 'Move up' })).toBeDisabled();
			await page.getByRole('menuitem', { name: 'Edit' }).click();
			const edit = page.getByRole('dialog', { name: 'Edit' });
			await edit.getByRole('textbox', { name: 'Lesson' }).fill('Run make check before every push');
			await edit.getByRole('button', { name: 'Save' }).click();
			await expect(api.getByRole('listitem').first()).toContainText('Run make check before every push');

			await menu('Migration numbers').click();
			await page.getByRole('menuitem', { name: 'Retire' }).click();
			await page.getByRole('alertdialog', { name: 'Retire this lesson?' }).getByRole('button', { name: 'Retire' }).click();
			await expect(api.getByRole('listitem')).toHaveCount(3);
			await expect(page.getByText('Migration numbers must follow main')).toHaveCount(0);

			// A lesson an admin writes by hand is a lesson at once.
			await page.getByRole('button', { name: 'Add lesson' }).click();
			const add = page.getByRole('dialog', { name: 'Add lesson' });
			await add.getByRole('combobox', { name: 'Repository' }).click();
			await page.getByRole('option', { name: 'acme/web' }).click();
			await add.getByRole('textbox', { name: 'Lesson' }).fill('Screenshots go in the pull request');
			await add.getByRole('button', { name: 'Add' }).click();
			await expect(web.getByRole('listitem').last()).toContainText('Screenshots go in the pull request');
			await expect(rows).toHaveCount(1);

			// A refusal is said and keeps what was typed.
			state.refuse = 'This repository has its maximum of approved lessons; retire one first';
			await page.getByRole('button', { name: 'Add lesson' }).click();
			await add.getByRole('textbox', { name: 'Lesson' }).fill('One too many');
			await add.getByRole('button', { name: 'Add' }).click();
			await expect(page.getByText('This repository has its maximum of approved lessons')).toBeVisible();
			await expect(add.getByRole('textbox', { name: 'Lesson' })).toHaveValue('One too many');
			expect(await fits(page)).toBe(true);
		});

		test('a member reads approved lessons and their own proposal, and can only propose', async ({ page }) => {
			const state = await fixture(page, 'member');
			await page.goto('/test/settings/lessons');
			const proposals = page.getByRole('region', { name: 'Proposals' });
			await expect(proposals.getByRole('listitem')).toHaveText([/Ask before adding a dependency/]);
			await expect(page.getByText('run the linter')).toHaveCount(0);
			await expect(page.getByText('A private proposal of someone else')).toHaveCount(0);
			const api = page.getByRole('region', { name: 'acme/api' });
			await expect(api.getByRole('listitem')).toHaveCount(3);
			// No decision is offered anywhere.
			const main = page.getByRole('main');
			for (const name of ['Approve', 'Edit', 'Drop', 'More actions', 'Edited'])
				await expect(main.getByRole('button', { name, exact: true })).toHaveCount(0);

			// What a member writes waits as a proposal; it is not a lesson yet.
			await page.getByRole('button', { name: 'Add lesson' }).click();
			const add = page.getByRole('dialog', { name: 'Add lesson' });
			await add.getByRole('textbox', { name: 'Lesson' }).fill('Name branches after the issue');
			await add.getByRole('button', { name: 'Add' }).click();
			await expect(proposals.getByRole('listitem')).toHaveCount(2);
			await expect(proposals).toContainText('Name branches after the issue');
			await expect(proposals).toContainText('Proposed by Mia Member');
			await expect(api).not.toContainText('Name branches after the issue');
			expect(state.calls).toEqual(['POST / {"repository":"acme/api","text":"Name branches after the issue"}']);
			expect(await fits(page)).toBe(true);
		});
	});
}
