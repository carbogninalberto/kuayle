import { expect, test, type Page } from '@playwright/test';

const base = '/api/workspaces/studio';
const member = { user_id: 'ann', name: 'Ann Rivera', email: 'ann@example.test', role: 'member' };
const owner = { id: 'lee', name: 'Lee Chan', email: 'lee@example.test' };
const groups = [
	{
		area: 'issues',
		permissions: [
			{ code: 'issues:read', hint: 'Read visible issues and their history.' },
			{ code: 'issue:update', hint: 'Edit issues and change their status.' }
		]
	},
	{ area: 'wiki', permissions: [{ code: 'wiki:read', hint: 'Read visible wiki pages.' }] }
];

async function mockRoles(page: Page, currentRole = 'owner') {
	const workspace = {
		id: 'studio',
		slug: 'studio',
		name: 'Studio',
		owner_id: owner.id,
		current_user_role: currentRole
	};
	const roles = ['owner', 'admin', 'member', 'guest'].map((key) => ({
		key,
		name: key[0].toUpperCase() + key.slice(1),
		description: '',
		permissions: key === 'guest' ? ['issues:read'] : groups.flatMap((g) => g.permissions.map((p) => p.code)),
		member_count: Number(['member', 'owner'].includes(key)),
		team_assignments: 0
	}));
	let assigned = member.role;
	let override = '';
	let rejectFirstSave = true;
	const writes: { method: string; path: string; body: any }[] = [];
	await page.routeWebSocket('**/ws', () => {});
	await page.route(
		(url) => url.pathname.startsWith('/api/'),
		async (route) => {
			const req = route.request();
			const path = new URL(req.url()).pathname;
			const method = req.method();
			const json = (data: unknown) => route.fulfill({ json: data });
			if (method !== 'GET') writes.push({ method, path, body: req.postDataJSON() });
			if (path === '/api/auth/me') return json(owner);
			if (path === '/api/workspaces') return json([workspace]);
			if (path === base) return json(workspace);
			if (path === '/api/notifications') return json({ notifications: [], unread_count: 0 });
			if (path === base + '/teams') return json([{ id: 'eng', name: 'Engineering', key: 'ENG' }]);
			if (path === base + '/members')
				return json([
					{ ...owner, user_id: owner.id, role: 'owner' },
					{ ...member, role: assigned }
				]);
			if (path === base + '/roles/permissions') return json(groups);
			if (path === base + '/roles' && method === 'GET')
				return json(
					roles.map((r) => ({
						...r,
						member_count: Number(r.key === assigned || r.key === 'owner'),
						team_assignments: Number(r.key === override)
					}))
				);
			if (path === base + '/members/ann/team-roles') return json(override ? [{ team_id: 'eng', role: override }] : []);
			if (path === base + '/members/ann/team-roles/eng') {
				override = method === 'DELETE' ? '' : req.postDataJSON().role;
				return route.fulfill({ status: 204 });
			}
			if (path === base + '/members/ann' && method === 'PATCH') {
				assigned = req.postDataJSON().role;
				return json({ status: 'updated' });
			}
			if (path === base + '/roles' && method === 'POST') {
				if (rejectFirstSave) {
					rejectFirstSave = false;
					return route.fulfill({ status: 409, json: { error: { message: 'Role name already exists' } } });
				}
				const role = { ...req.postDataJSON(), key: 'triage', member_count: 0, team_assignments: 0 };
				roles.push(role);
				return json(role);
			}
			if (path === base + '/roles/triage') {
				const index = roles.findIndex((r) => r.key === 'triage');
				if (method === 'DELETE') {
					roles.splice(index, 1);
					return route.fulfill({ status: 204 });
				}
				Object.assign(roles[index], req.postDataJSON());
				return json(roles[index]);
			}
			if (path === '/api/preferences' || path === '/api/config') return json({});
			return json([]);
		}
	);
	return writes;
}

for (const width of [390, 1280]) {
	test(`owner manages a role and team override at ${width}px`, async ({ page }, testInfo) => {
		const writes = await mockRoles(page);
		await page.setViewportSize({ width, height: 900 });
		await page.goto('/studio/settings/members');
		const roles = page.getByRole('table', { name: 'Roles' });
		await expect(roles).toBeVisible();
		await expect(roles.getByRole('row', { name: /Owner/ }).getByRole('button')).toHaveCount(0);
		await page.getByRole('button', { name: 'Create role' }).click();
		const dialog = page.getByRole('dialog');
		await expect(dialog.getByText(/treat this member as a Guest/)).toBeVisible();
		await dialog.getByLabel('Name', { exact: true }).fill('Triage');
		await dialog.getByLabel('Template').click();
		await page.getByRole('option', { name: 'Guest', exact: true }).click();
		await expect(dialog.getByRole('checkbox', { name: 'issues:read', exact: true })).toBeChecked();
		const update = dialog.getByRole('checkbox', { name: 'issue:update', exact: true });
		await update.focus();
		await update.press('Space');
		await expect(update).toBeChecked();
		await expect(update).toHaveAccessibleDescription('Edit issues and change their status.');
		await dialog.getByLabel('Description', { exact: true }).fill('Review incoming issues.');
		await page.screenshot({ path: testInfo.outputPath(`role-editor-${width}.png`), animations: 'disabled' });
		await dialog.getByRole('button', { name: 'Save', exact: true }).click();
		await expect(dialog.getByRole('alert')).toContainText('Role name already exists');
		await dialog.getByRole('button', { name: 'Save', exact: true }).click();
		const row = roles.getByRole('row', { name: /Triage/ });
		await expect(row).toBeVisible();
		expect(
			writes
				.filter((w) => w.method === 'POST')
				.at(-1)
				?.body.permissions.sort()
		).toEqual(['issue:update', 'issues:read']);
		const assignment = page.getByRole('combobox', { name: 'Role Ann Rivera' });
		await assignment.click();
		await page.getByRole('option', { name: 'Triage', exact: true }).click();
		await expect(row.getByRole('cell').nth(1)).toHaveText('1');
		await row.getByRole('button', { name: 'Edit' }).click();
		await expect(dialog.getByRole('button', { name: 'Delete' })).toBeDisabled();
		await dialog.getByLabel('Name', { exact: true }).fill('Triage lead');
		await dialog.getByRole('button', { name: 'Save', exact: true }).click();
		await expect(assignment).toHaveText('Triage lead');
		await page.getByRole('button', { name: 'Team roles Ann Rivera' }).click();
		await expect(
			dialog.getByText(/A Guest override can block project, label, view and wiki edits in other teams/)
		).toBeVisible();
		const team = dialog.getByRole('combobox', { name: 'Engineering' });
		await team.click();
		await page.getByRole('option', { name: 'Guest', exact: true }).click();
		await expect(team).toHaveText('Guest');
		await page.screenshot({ path: testInfo.outputPath(`team-roles-${width}.png`), animations: 'disabled' });
		await team.click();
		await page.getByRole('option', { name: 'Use workspace role' }).click();
		await expect(team).toHaveText('Use workspace role');
		await dialog.press('Escape');
		await page.screenshot({ path: testInfo.outputPath(`roles-${width}.png`), animations: 'disabled' });
		await assignment.click();
		await page.getByRole('option', { name: 'Member', exact: true }).click();
		await row.getByRole('button', { name: 'Edit' }).click();
		await dialog.getByRole('button', { name: 'Delete', exact: true }).click();
		await expect(row).toHaveCount(0);
		expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
	});
}

test('members have no role administration controls', async ({ page }) => {
	await mockRoles(page, 'member');
	await page.goto('/studio/settings/members');
	await expect(page.getByText(member.email, { exact: true })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Create role' })).toHaveCount(0);
	await expect(page.getByRole('button', { name: /Team roles/ })).toHaveCount(0);
	await expect(page.getByRole('combobox', { name: 'Role Ann Rivera' })).toHaveCount(0);
});

test('permission loading can be retried without losing the draft or granting default permissions', async ({ page }) => {
	await mockRoles(page);
	await page.route('**/roles/permissions', (route) => route.fulfill({ status: 503, json: {} }), { times: 1 });
	await page.goto('/studio/settings/members');
	await page.getByRole('button', { name: 'Create role' }).click();
	const dialog = page.getByRole('dialog');
	const name = 'T'.repeat(100);
	await dialog.getByLabel('Name', { exact: true }).fill(name);
	await expect(dialog.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
	await dialog.getByRole('button', { name: 'Retry' }).click();
	await expect(dialog.getByRole('checkbox', { checked: true })).toHaveCount(0);
	await expect(dialog.getByLabel('Name', { exact: true })).toHaveValue(name);
	const save = dialog.getByRole('button', { name: 'Save', exact: true });
	await save.click();
	await expect(dialog.getByRole('alert')).toBeVisible();
	await save.click();
	await expect(page.getByRole('row', { name: new RegExp(name) })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Create role' })).toBeFocused();
});

test('team role dialog keeps its controls reachable after retry and with long team names', async ({ page }) => {
	await mockRoles(page);
	await page.setViewportSize({ width: 390, height: 640 });
	await page.goto('/studio/settings/members');
	const trigger = page.getByRole('button', { name: 'Team roles Ann Rivera' });
	await expect(trigger).toBeEnabled();
	const teamsPath = '**/api/workspaces/studio/teams';
	await page.route(teamsPath, (route) => route.fulfill({ json: [] }));
	await page.route('**/members/ann/team-roles', (route) => route.fulfill({ status: 503, json: {} }), { times: 1 });
	await trigger.click();
	const dialog = page.getByRole('dialog');
	await expect(dialog.getByRole('alert')).toBeVisible();
	await dialog.getByRole('button', { name: 'Retry' }).click();
	await expect(dialog.getByText('No team', { exact: true })).toBeVisible();
	await dialog.press('Escape');
	await expect(trigger).toBeFocused();

	const teams = Array.from({ length: 12 }, (_, i) => ({ id: 'team-' + i, name: 'Engineering'.repeat(8) + i }));
	await page.route(teamsPath, (route) => route.fulfill({ json: teams }));
	await trigger.press('Enter');
	await expect(
		dialog.getByText(/A Guest override can block project, label, view and wiki edits in other teams/)
	).toBeInViewport();
	const lastTeam = dialog.getByRole('combobox').last();
	await lastTeam.focus();
	await expect(lastTeam).toBeInViewport();
	expect(await dialog.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
	await lastTeam.press('Tab');
	const focused = page.locator(':focus');
	await expect(focused).toHaveAccessibleName('Close');
	await expect(focused).toBeInViewport();
	await focused.press('Enter');
	await expect(trigger).toBeFocused();
});
