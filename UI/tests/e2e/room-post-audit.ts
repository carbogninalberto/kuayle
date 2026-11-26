import { expect, type Locator, type Page } from '@playwright/test';

export const audit = [
	'Which of these accessibility fixes should go first?',
	'',
	'| Screen | Before | Proposed fix |',
	'|---|---|---|',
	'| Issue list | Icon alone | Add a visible label |',
	'| Task room | Long lines | Scroll inside the card |',
	'| Settings | Missing focus | Restore focus to `NotificationPreferencesDialogTrigger` |',
	'',
	'### Verification',
	'- Check **focus** with the keyboard.',
	'- Review the [audit notes](https://example.com/audit).',
	'',
	'```sh',
	'npm run test:e2e -- tests/e2e/task-room.test.ts tests/e2e/agent-conversation.test.ts',
	'```'
].join('\n');

export async function verifyAudit(page: Page, post: Locator, surface: string) {
	const table = post.getByRole('table');
	await expect(table.getByRole('columnheader')).toHaveText(['Screen', 'Before', 'Proposed fix']);
	const more = post.getByRole('button', { name: 'Show more', exact: true });
	await expect(more).toHaveAttribute('aria-expanded', 'false');
	async function capture(theme: 'light' | 'dark') {
		if (!process.env.KUAYLE_SCREENSHOT_DIR) return;
		await page.evaluate((value) => {
			document.documentElement.classList.toggle('dark', value === 'dark');
			document.documentElement.setAttribute('data-theme', value);
		}, theme);
		await table.scrollIntoViewIfNeeded();
		const width = page.viewportSize()!.width;
		await page.screenshot({ path: `${process.env.KUAYLE_SCREENSHOT_DIR}/${surface}-${width}-${theme}.png`, animations: 'disabled' });
	}
	await capture('light');
	const previewHeight = (await post.boundingBox())!.height;
	await more.press('Enter');
	const less = post.getByRole('button', { name: 'Show less', exact: true });
	await expect(less).toHaveAttribute('aria-expanded', 'true');
	expect((await post.boundingBox())!.height).toBeGreaterThan(previewHeight);
	await expect(post.getByRole('heading', { name: 'Verification' })).toBeVisible();
	if (page.viewportSize()!.width === 390) {
		for (const block of [table, post.locator('pre')]) {
			expect(await block.evaluate((el) => {
				el.scrollLeft = 48;
				const card = el.closest('[data-slot="bubble-content"]')!.getBoundingClientRect();
				const { left, right } = el.getBoundingClientRect();
				return el.scrollLeft > 0 && left >= card.left && right <= card.right && right <= innerWidth;
			})).toBe(true);
		}
		expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
	}
	await capture('dark');
	await less.press('Space');
	await expect(more).toHaveAttribute('aria-expanded', 'false');
	await expect(more).toBeFocused();
}
