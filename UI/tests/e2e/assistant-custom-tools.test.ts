import { test, expect, type Page, type Route } from '@playwright/test';

const at = '2026-10-20T09:30:00Z';
const workspaceId = 'workspace-1';
const releaseId = '00000000-0000-4000-8000-0000000000a1';
const branchesId = '00000000-0000-4000-8000-0000000000a2';
const specSchema = {
	type: 'object',
	properties: { spec: { type: 'string', description: 'Spec file to run', maxLength: 200 } },
	required: ['spec'],
	additionalProperties: false
};

function seed() {
	const version = (tool_id: string, n: number, status: 'draft' | 'active', script: string, author_kind: 'user' | 'assistant' = 'user') => ({
		tool_id,
		version: n,
		name: tool_id === releaseId ? 'run_e2e' : 'list_branches',
		description: tool_id === releaseId ? 'Run one Playwright spec.' : 'List the git branches.',
		parameters: tool_id === releaseId ? specSchema : { type: 'object', properties: {}, additionalProperties: false },
		script,
		interpreter: tool_id === releaseId ? 'sh' : 'python3',
		run_mode: tool_id === releaseId ? 'approval' : 'auto',
		read_only: tool_id !== releaseId,
		timeout_seconds: 600,
		status,
		author_kind,
		author_user_id: author_kind === 'user' ? 'user-1' : null,
		created_at: at
	});
	const versions: Record<string, ReturnType<typeof version>[]> = {
		[releaseId]: [
			version(releaseId, 3, 'draft', 'cd UI\nnpx playwright test "$KUAYLE_ARG_SPEC" --reporter=line', 'assistant'),
			version(releaseId, 2, 'active', 'cd UI\nnpx playwright test "$KUAYLE_ARG_SPEC"'),
			version(releaseId, 1, 'active', 'npx playwright test')
		],
		[branchesId]: [version(branchesId, 1, 'active', 'import subprocess\nprint(subprocess.check_output(["git", "branch"]).decode())')]
	};
	const tool = (id: string, extra: Record<string, unknown>) => ({
		id,
		workspace_id: workspaceId,
		name: versions[id].at(-1)!.name,
		description: versions[id][0].description,
		parameters: versions[id][0].parameters,
		interpreter: versions[id][0].interpreter,
		run_mode: versions[id][0].run_mode,
		read_only: versions[id][0].read_only,
		timeout_seconds: 600,
		enabled: true,
		status: 'active',
		current_version: 1,
		latest_version: 1,
		pending_version: null as number | null,
		created_by_user_id: 'user-1',
		author_kind: 'user',
		created_at: at,
		updated_at: at,
		...extra
	});
	return {
		versions,
		tools: [
			tool(releaseId, { current_version: 2, latest_version: 3, pending_version: 3 }),
			tool(branchesId, { enabled: false })
		]
	};
}

const run = (extra: Record<string, unknown>) => ({
	id: 'run-1',
	workspace_id: workspaceId,
	tool_id: releaseId,
	tool_version: 2,
	tool_name: 'run_e2e',
	inputs: { spec: 'login.spec.ts' },
	requested_by_user_id: 'user-1',
	conversation_id: null,
	tool_call_id: null,
	approval_id: null,
	machine_id: 'machine-1',
	issue_id: null,
	task_id: null,
	working_directory: '/workspace',
	timeout_seconds: 600,
	test: true,
	status: 'running',
	exit_code: null,
	output: '',
	output_truncated: false,
	error: null,
	created_at: at,
	started_at: at,
	finished_at: null,
	duration_ms: null,
	...extra
});

type Write = { method: string; path: string; body: any };

async function base(page: Page, role: 'admin' | 'member') {
	const workspace = { id: workspaceId, slug: 'test', name: 'Test Workspace', current_user_role: role };
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	return async (route: Route, path: string) => {
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user-1', name: 'Ada', display_name: 'Ada', email: 'ada@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		return null;
	};
}

async function settings(page: Page, role: 'admin' | 'member') {
	const { tools, versions } = seed();
	const state = { tools, versions, writes: [] as Write[], runs: [run({ status: 'succeeded', exit_code: 0, output: 'old run', duration_ms: 1200, finished_at: at, id: 'run-0' })], polls: 0 };
	const common = await base(page, role);
	const prefix = '/api/workspaces/test/assistant-tools';
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const method = request.method();
		const handled = await common(route, path);
		if (handled !== null) return handled;
		if (path === '/api/workspaces/test/dev-machines')
			return route.fulfill({
				json: {
					data: [
						{ id: 'machine-1', name: 'ada-box', status: 'running', created_by_user_id: 'user-1' },
						{ id: 'machine-2', name: 'someone-else', status: 'running', created_by_user_id: 'user-2' }
					],
					total_count: 2,
					page: 1,
					per_page: 50,
					has_more: false
				}
			});
		if (path === '/api/workspaces/test/assistant-tool-runs/run-1') {
			state.polls++;
			const done = state.polls >= 2;
			return route.fulfill({
				json: run(
					done
						? { status: 'succeeded', exit_code: 0, output: '1 passed\n<img src=x onerror="window.evil=1">', duration_ms: 4200, finished_at: at }
						: {}
				)
			});
		}
		if (!path.startsWith(prefix)) return route.fulfill({ json: [] });
		if (method !== 'GET') {
			state.writes.push({ method, path: path.slice(prefix.length), body: request.postData() ? request.postDataJSON() : null });
			if (role !== 'admin') return route.fulfill({ status: 403, json: { error: { code: 'FORBIDDEN', message: 'Forbidden' } } });
		}
		if (path === prefix && method === 'GET')
			return route.fulfill({ json: { tools: state.tools, interpreters: ['sh', 'bash', 'node', 'python3'], can_manage: role === 'admin' } });
		if (path === prefix && method === 'POST') {
			const input = request.postDataJSON();
			const created = { ...state.tools[0], ...input, id: 'tool-new', current_version: 1, latest_version: 1, pending_version: null, status: input.status ?? 'active' };
			state.tools.push(created);
			state.versions['tool-new'] = [{ ...state.versions[releaseId][0], ...input, tool_id: 'tool-new', version: 1, status: created.status, author_kind: 'user' }];
			return route.fulfill({ status: 201, json: created });
		}
		const match = path.slice(prefix.length).match(/^\/([\w-]+)(\/.*)?$/);
		const tool = state.tools.find((entry) => entry.id === match?.[1]);
		if (!match || !tool) return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND', message: 'Not found' } } });
		const rest = match[2] ?? '';
		const current = () => state.versions[tool.id].find((entry) => entry.version === tool.current_version)!;
		if (rest === '' && method === 'GET') return route.fulfill({ json: { ...tool, script: current().script } });
		if (rest === '/versions') return route.fulfill({ json: { versions: state.versions[tool.id] } });
		if (rest === '/runs') return route.fulfill({ json: { runs: state.runs } });
		if (rest === '/enabled') {
			tool.enabled = request.postDataJSON().enabled;
			return route.fulfill({ json: tool });
		}
		if (rest === '/activate') {
			const draft = state.versions[tool.id].find((entry) => entry.version === request.postDataJSON().version)!;
			draft.status = 'active';
			Object.assign(tool, { status: 'active', current_version: draft.version, pending_version: null });
			return route.fulfill({ json: tool });
		}
		if (rest === '/test-run') {
			state.runs.unshift(run({ status: 'queued', started_at: null }));
			return route.fulfill({ status: 202, json: state.runs[0] });
		}
		if (rest === '' && method === 'DELETE') {
			state.tools = state.tools.filter((entry) => entry.id !== tool.id);
			return route.fulfill({ status: 204 });
		}
		return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND', message: 'Not found' } } });
	});
	return state;
}

const noHorizontalScroll = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);

test('admins build a tool, activate a reviewed draft and test-run it', async ({ page }) => {
	const state = await settings(page, 'admin');
	await page.goto('/test/settings/assistant?tab=custom-tools');
	await expect(page.getByRole('tab', { name: 'Custom tools', exact: true })).toHaveAttribute('aria-selected', 'true');
	const list = page.getByRole('list', { name: 'Custom tools' });
	await expect(list.getByRole('listitem')).toHaveCount(2);
	const e2e = list.getByRole('listitem', { name: 'run_e2e' });
	await expect(e2e.getByText('custom_run_e2e', { exact: true })).toBeVisible();
	await expect(e2e.getByText('Run one Playwright spec.')).toBeVisible();
	await expect(e2e.getByText('Active', { exact: true })).toBeVisible();
	await expect(e2e.getByText('Approval each run')).toBeVisible();
	await expect(e2e.getByText('Draft v3 pending')).toBeVisible();
	await expect(e2e.getByText('v2', { exact: true })).toBeVisible();
	await expect(e2e.getByText('sh', { exact: true })).toBeVisible();
	await expect(e2e.getByText('By a person')).toBeVisible();
	const branches = list.getByRole('listitem', { name: 'list_branches' });
	await expect(branches.getByText('Runs automatically')).toBeVisible();
	await expect(branches.getByText('Read-only', { exact: true })).toBeVisible();
	await expect(branches.getByText('python3', { exact: true })).toBeVisible();
	const toggle = page.getByRole('switch', { name: 'Enable list_branches' });
	await expect(toggle).not.toBeChecked();
	await toggle.click();
	await expect(toggle).toBeChecked();
	expect(state.writes.at(-1)).toEqual({ method: 'PUT', path: `/${branchesId}/enabled`, body: { enabled: true } });

	// Create with the parameter builder.
	await page.getByRole('button', { name: 'New tool' }).click();
	const editor = page.getByRole('dialog', { name: 'New custom tool' });
	await editor.getByLabel('Name', { exact: true }).fill('run_spec');
	await expect(editor.getByText('custom_run_spec')).toBeVisible();
	await editor.getByLabel('Description', { exact: true }).fill('Run a Playwright spec I name.');
	await editor.getByRole('combobox', { name: 'Interpreter' }).click();
	await page.getByRole('option', { name: 'bash' }).click();
	await editor.getByLabel('Script', { exact: true }).fill('npx playwright test "$KUAYLE_ARG_SPEC"');
	await editor.getByRole('button', { name: 'Add parameter' }).click();
	await editor.getByLabel('Parameter name').nth(0).fill('spec');
	await editor.getByLabel('Parameter description').nth(0).fill('Spec file');
	await editor.getByLabel('Max length').nth(0).fill('200');
	await editor.getByRole('checkbox', { name: 'Required' }).nth(0).click();
	await editor.getByRole('button', { name: 'Add parameter' }).click();
	await editor.getByLabel('Parameter name').nth(1).fill('browser');
	await editor.getByRole('combobox', { name: 'Type' }).nth(1).click();
	await page.getByRole('option', { name: 'One of a list' }).click();
	await editor.getByLabel('Values, one per line').fill('chromium\nwebkit');
	await editor.getByRole('button', { name: 'Add parameter' }).click();
	await editor.getByLabel('Parameter name').nth(2).fill('Bad Name');
	await expect(editor.getByText('Parameter names use lowercase letters')).toBeVisible();
	await expect(editor.getByRole('button', { name: 'Create and activate' })).toBeDisabled();
	await editor.getByRole('button', { name: 'Remove parameter Bad Name' }).click();
	// Auto runs need the read-only mark.
	const mode = editor.getByRole('combobox', { name: 'Run mode' });
	await mode.click();
	await expect(page.getByRole('option', { name: 'Runs automatically' })).toHaveAttribute('data-disabled');
	await page.keyboard.press('Escape');
	await editor.getByRole('checkbox', { name: /^Read-only/ }).click();
	await mode.click();
	await page.getByRole('option', { name: 'Runs automatically' }).click();
	await editor.getByLabel('Timeout (seconds)').fill('120');
	await editor.getByRole('tab', { name: 'JSON' }).click();
	await expect(editor.getByText('"additionalProperties": false')).toBeVisible();
	await editor.getByRole('button', { name: 'Create and activate' }).click();
	await expect(editor).toBeHidden();
	const created = state.writes.find((write) => write.method === 'POST' && write.path === '');
	expect(created?.body).toEqual({
		name: 'run_spec',
		description: 'Run a Playwright spec I name.',
		interpreter: 'bash',
		script: 'npx playwright test "$KUAYLE_ARG_SPEC"',
		parameters: {
			type: 'object',
			properties: {
				spec: { type: 'string', description: 'Spec file', maxLength: 200 },
				browser: { type: 'string', enum: ['chromium', 'webkit'] }
			},
			required: ['spec'],
			additionalProperties: false
		},
		run_mode: 'auto',
		read_only: true,
		timeout_seconds: 120,
		status: 'active'
	});

	// Review the pending draft against the version in effect, then activate it.
	await page.goto(`/test/settings/assistant?tab=custom-tools&tool=${releaseId}`);
	await expect(page.getByRole('heading', { name: 'run_e2e' })).toBeVisible();
	await expect(page.getByText('Draft version 3 is waiting for activation')).toBeVisible();
	await page.getByRole('button', { name: 'Review and activate' }).click();
	const review = page.getByRole('dialog', { name: 'Review version 3' });
	await expect(review.getByRole('group', { name: 'Added' })).toHaveText('+ npx playwright test "$KUAYLE_ARG_SPEC" --reporter=line');
	await expect(review.getByRole('group', { name: 'Removed' })).toHaveText('- npx playwright test "$KUAYLE_ARG_SPEC"');
	await review.getByRole('button', { name: 'Activate' }).click();
	await expect(review).toBeHidden();
	expect(state.writes.at(-1)).toEqual({ method: 'POST', path: `/${releaseId}/activate`, body: { version: 3 } });
	await expect(page.getByText('Draft version 3 is waiting for activation')).toBeHidden();

	// Test run: inputs from the schema, a machine of mine, and the run polled to its end.
	await page.getByRole('button', { name: 'Test run' }).click();
	const tester = page.getByRole('dialog', { name: 'Test run run_e2e' });
	await tester.getByRole('combobox', { name: 'Machine' }).click();
	await expect(page.getByRole('option', { name: /someone-else/ })).toHaveCount(0);
	await page.getByRole('option', { name: /ada-box/ }).click();
	await tester.getByRole('button', { name: 'Run', exact: true }).click();
	await expect(tester.getByText('spec is required.')).toBeVisible();
	await tester.getByLabel('spec').fill('login.spec.ts; rm -rf /');
	await tester.getByRole('button', { name: 'Run', exact: true }).click();
	await expect(tester.getByText('Succeeded', { exact: true })).toBeVisible({ timeout: 10_000 });
	await expect(tester.getByText('Exit code 0')).toBeVisible();
	await expect(tester.getByText('<img src=x onerror="window.evil=1">', { exact: false })).toBeVisible();
	expect(await page.evaluate(() => (window as any).evil)).toBeUndefined();
	expect(state.writes.at(-1)).toEqual({
		method: 'POST',
		path: `/${releaseId}/test-run`,
		body: { inputs: { spec: 'login.spec.ts; rm -rf /' }, machine_id: 'machine-1', version: 3 }
	});
	await tester.getByRole('button', { name: 'Close' }).first().click();

	// Run history lists the runs with details.
	const runs = page.getByRole('list', { name: 'Run history' });
	await expect(runs.getByRole('listitem')).toHaveCount(2);

	// Delete asks first.
	await page.getByRole('button', { name: 'Delete', exact: true }).click();
	await page.getByRole('alertdialog').getByRole('button', { name: 'Delete' }).click();
	await expect(page.getByRole('list', { name: 'Custom tools' })).toBeVisible();
	expect(state.writes.at(-1)).toMatchObject({ method: 'DELETE', path: `/${releaseId}` });
});

test('members read custom tools without write controls, also at phone width', async ({ page }) => {
	const state = await settings(page, 'member');
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/settings/assistant?tab=custom-tools');
	const list = page.getByRole('list', { name: 'Custom tools' });
	await expect(list.getByRole('listitem')).toHaveCount(2);
	await expect(page.getByText('Only workspace owners and admins can write')).toBeVisible();
	await expect(page.getByRole('button', { name: 'New tool' })).toHaveCount(0);
	await expect(page.getByRole('switch')).toHaveCount(0);
	await expect(list.getByRole('listitem', { name: 'list_branches' }).getByText('Disabled', { exact: true })).toBeVisible();
	expect(await noHorizontalScroll(page)).toBe(true);
	await list.getByRole('button', { name: 'run_e2e' }).click();
	await expect(page.getByRole('heading', { name: 'run_e2e' })).toBeVisible();
	await expect(page.getByText('npx playwright test "$KUAYLE_ARG_SPEC"').first()).toBeVisible();
	for (const name of ['Edit', 'Delete', 'Test run', 'Review and activate', 'Restore', 'Activate'])
		await expect(page.getByRole('button', { name, exact: true })).toHaveCount(0);
	expect(await noHorizontalScroll(page)).toBe(true);
	expect(state.writes).toEqual([]);
});

// Chat cards.
const conversation = { id: 'chat-1', title: 'Tools', workspace_id: workspaceId, creator_id: 'user-1', created_at: at, updated_at: at };
const userMessage = { id: 'message-1', conversation_id: 'chat-1', sequence: 1, role: 'user', content: 'Make a tool', status: 'completed', provider: '', model: '', created_at: at };
const answer = { ...userMessage, id: 'message-2', sequence: 2, role: 'assistant', content: 'Done.', provider: 'openai_compatible', model: 'm' };

async function chat(page: Page, tools: any[], approvals: any[] = []) {
	const state = { tools, approvals, decisions: [] as string[], runs: [] as any[], runLookups: 0 };
	const common = await base(page, 'admin');
	await page.routeWebSocket('**/conversations/chat-1/stream*', () => {});
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const handled = await common(route, path);
		if (handled !== null) return handled;
		if (path.endsWith('/assistant/status')) return route.fulfill({ json: { configured: true, provider: 'openai_compatible', model: 'm' } });
		if (path.endsWith('/conversations')) return route.fulfill({ json: [conversation] });
		if (path === `/api/workspaces/test/assistant-tools/${releaseId}/runs`) {
			state.runLookups++;
			return route.fulfill({ json: { runs: state.runs } });
		}
		const one = path.match(/^\/api\/workspaces\/test\/assistant-tool-runs\/([\w-]+)$/);
		if (one) {
			const found = state.runs.find((entry) => entry.id === one[1]);
			return found ? route.fulfill({ json: found }) : route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
		}
		if (path.includes('/approvals/')) {
			const decision = request.postDataJSON().decision;
			state.decisions.push(decision);
			const resolved = { ...state.approvals[0], status: decision === 'approve' ? 'approved' : 'denied', resolved_at: at, updated_at: '2026-10-20T09:31:00Z' };
			state.approvals = [resolved];
			return route.fulfill({ json: resolved });
		}
		if (path.endsWith('/events')) return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/conversations/chat-1')
			return route.fulfill({
				json: { conversation, messages: [userMessage, answer], tools: state.tools, approvals: state.approvals, cursor: 0, has_more: false }
			});
		return route.fulfill({ json: [] });
	});
	return state;
}

const call = (extra: Record<string, unknown>) => ({
	id: 'tool-call-1',
	conversation_id: 'chat-1',
	message_id: answer.id,
	sequence: 0,
	call_id: 'call_1',
	status: 'completed',
	created_at: at,
	...extra
});

test('a proposed tool shows its full script as text and deep-links to the draft', async ({ page }) => {
	await chat(page, [
		call({
			name: 'propose_tool',
			input: { name: 'run_e2e' },
			output: {
				tool_id: releaseId,
				name: 'run_e2e',
				description: 'Run one Playwright spec.',
				parameters: specSchema,
				script: 'echo "<script>window.evil=1</script>"\nnpx playwright test "$KUAYLE_ARG_SPEC"',
				interpreter: 'sh',
				read_only: true,
				run_mode: 'approval',
				timeout_seconds: 600,
				version: 3,
				status: 'draft',
				tool_status: 'active',
				note: 'Saved as draft version 3.'
			}
		})
	]);
	await page.goto('/test/assistant?conversation=chat-1');
	const card = page.getByRole('article', { name: 'Proposed tool run_e2e' });
	await expect(card.getByText('Draft — not active until activated')).toBeVisible();
	await expect(card.getByText('spec: string*')).toBeVisible();
	await expect(card.getByText('echo "<script>window.evil=1</script>"', { exact: false })).toBeVisible();
	expect(await page.evaluate(() => (window as any).evil)).toBeUndefined();
	await expect(card.getByRole('link', { name: 'Review in Settings' })).toHaveAttribute(
		'href',
		`/test/settings/assistant?tab=custom-tools&tool=${releaseId}`
	);
});

test('a custom tool run waits for approval with its exact inputs, then shows its result', async ({ page }) => {
	const state = await chat(
		page,
		[
			call({
				name: 'custom_run_e2e',
				input: { identifier: 'ENG-1', inputs: { spec: '; rm -rf /' } },
				output: { approval_id: 'approval-1', status: 'pending_user_approval', identifier: 'ENG-1', tool: 'run_e2e' }
			}),
			call({
				id: 'tool-call-2',
				sequence: 1,
				call_id: 'call_2',
				name: 'get_tool_run',
				input: { run_id: 'run-0' },
				output: { run_id: 'run-0', tool: 'list_branches', version: 1, status: 'succeeded', exit_code: 0, output_tail: 'main' }
			})
		],
		[
			{
				id: 'approval-1',
				conversation_id: 'chat-1',
				tool_call_id: 'tool-call-1',
				action: 'run_custom_tool',
				actor_id: 'user-1',
				issue_id: 'issue-1',
				payload: {
					identifier: 'ENG-1',
					title: 'Login page',
					tool_id: releaseId,
					tool_name: 'run_e2e',
					version: 2,
					description: 'Run one Playwright spec.',
					read_only: false,
					interpreter: 'sh',
					inputs: { spec: '; rm -rf /' },
					task_id: 'task-1',
					generation: 1
				},
				status: 'pending',
				expires_at: '2026-10-21T09:30:00Z',
				created_at: at,
				updated_at: at
			}
		]
	);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByText('Run custom_run_e2e v2 on ENG-1?', { exact: true })).toBeVisible();
	await expect(page.getByText('Run one Playwright spec.')).toBeVisible();
	await expect(page.getByText('"spec": "; rm -rf /"', { exact: false })).toBeVisible();
	await expect(page.getByText('Read the list_branches run: Succeeded')).toBeVisible();
	expect(await noHorizontalScroll(page)).toBe(true);
	state.runs = [run({ id: 'run-5', approval_id: 'approval-1', test: false, status: 'running', output: 'starting' })];
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Started custom_run_e2e on ENG-1', { exact: true })).toBeVisible();
	await expect(page.getByText('custom_run_e2e is running')).toBeVisible();
	state.runs = [run({ id: 'run-5', approval_id: 'approval-1', test: false, status: 'failed', exit_code: 1, output: '1 failed', finished_at: at, duration_ms: 3000 })];
	await expect(page.getByText('custom_run_e2e failed')).toBeVisible({ timeout: 10_000 });
	await expect(page.getByText('Exit code 1')).toBeVisible();
	await expect(page.getByText('1 failed', { exact: true })).toBeVisible();
	expect(state.decisions).toEqual(['approve']);
});
