import { expect, test, type Page } from '@playwright/test';

type Ability = { key: string; enabled: boolean; enabled_by_default: boolean };

function botSettings(overrides: Record<string, unknown> = {}) {
	const abilities: Ability[] = [
		'link_issues',
		'task_feedback',
		'review_comments',
		'pull_request_actions',
		'merge_pull_requests',
		'releases',
		'mention_reviews'
	].map((key) => ({ key, enabled: key !== 'mention_reviews', enabled_by_default: key !== 'mention_reviews' }));
	return {
		display_name: 'Kuayle',
		default_name: 'Kuayle',
		avatar_url: '',
		avatar_source: 'none',
		abilities,
		mention_harness: '',
		mention_model: '',
		mention_handle: '@kuayle-acme',
		harnesses: ['claude-code', 'codex', 'opencode'],
		...overrides
	};
}

async function mockApi(page: Page, role: string, patches: unknown[]) {
	const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: role };
	let bot = botSettings();
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: 'user-1', name: 'Owner', display_name: 'Owner', email: 'owner@example.test', avatar_url: null }
			});
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/github/status')
			return route.fulfill({
				json: {
					configured: true,
					installed: true,
					global_app: true,
					app_slug: 'kuayle-acme',
					installation: { id: 'inst-1', installation_id: 77, account_login: 'acme', account_type: 'Organization', created_at: '2026-01-01T00:00:00Z' },
					repos: []
				}
			});
		if (path === '/api/workspaces/test/github/app-setup')
			return route.fulfill({ json: { managed: true, app_slug: 'kuayle-acme', account_login: 'acme', state: 'ok', missing_events: [], missing_permissions: [] } });
		if (path === '/api/workspaces/test/github/bot') {
			if (route.request().method() === 'PATCH') {
				const update = route.request().postDataJSON();
				patches.push(update);
				bot = {
					...bot,
					...(update.display_name !== undefined ? { display_name: update.display_name || 'Kuayle' } : {}),
					...(update.mention_harness !== undefined ? { mention_harness: update.mention_harness } : {}),
					...(update.mention_model !== undefined ? { mention_model: update.mention_model } : {}),
					abilities: bot.abilities.map((a) => (update.abilities && a.key in update.abilities ? { ...a, enabled: update.abilities[a.key] } : a))
				};
			}
			return route.fulfill({ json: bot });
		}
		return route.fulfill({ json: [] });
	});
}

// Owners rename the bot, turn the mention trigger on and pick its harness;
// the page states that mention reviews skip the approval card.
test('owners customize the bot identity and abilities', async ({ page }) => {
	const patches: unknown[] = [];
	await mockApi(page, 'owner', patches);
	await page.goto('/test/settings/github');

	const section = page.getByRole('region', { name: 'Kuayle bot' });
	await expect(section).toBeVisible();
	const mention = section.getByRole('switch', { name: 'Review when mentioned' });
	await expect(mention).not.toBeChecked();
	await expect(section.getByRole('switch', { name: 'Merge pull requests' })).toBeChecked();
	await expect(section).toContainText('Comment "@kuayle-acme review" on a pull request a Kuayle task opened.');
	await expect(section).toContainText('Reviews started by a mention run without an approval card');

	const name = section.getByLabel('Display name');
	await name.fill('@everyone');
	await expect(section).toContainText('The name cannot contain line breaks');
	const saveName = section.getByRole('button', { name: 'Save' }).first();
	await expect(saveName).toBeDisabled();
	await name.fill('Acme Reviewer');
	await saveName.click();
	await expect.poll(() => patches).toContainEqual({ display_name: 'Acme Reviewer' });

	await mention.click();
	await expect.poll(() => patches).toContainEqual({ abilities: { mention_reviews: true } });
	await expect(mention).toBeChecked();
	await expect(section).toContainText('The GitHub App must receive the Issue comment event');

	await section.getByLabel('Review harness').click();
	await page.getByRole('option', { name: 'claude-code' }).click();
	await section.getByLabel('Model').fill('claude-sonnet-4-5');
	await section.getByRole('button', { name: 'Save' }).last().click();
	await expect.poll(() => patches).toContainEqual({ mention_harness: 'claude-code', mention_model: 'claude-sonnet-4-5' });

	await section.getByLabel('Profile picture').fill('http://example.com/bot.png');
	await expect(section).toContainText('Use an https:// image address.');
	await expect(section.getByRole('button', { name: 'Use link' })).toBeDisabled();
});

test('members see the bot settings read-only', async ({ page }) => {
	await mockApi(page, 'member', []);
	await page.goto('/test/settings/github');
	const section = page.getByRole('region', { name: 'Kuayle bot' });
	await expect(section.getByRole('switch', { name: 'Merge pull requests' })).toBeDisabled();
	await expect(section.getByLabel('Display name')).toBeDisabled();
	await expect(section).toContainText('Only workspace owners can change these settings.');
});
