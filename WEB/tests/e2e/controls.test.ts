import { expect, test } from '@playwright/test';

for (const width of [320, 1280]) {
	test(`cookie choices support keyboard, dismissal and persistence at ${width}px`, async ({ page }, testInfo) => {
		await page.setViewportSize({ width, height: 900 });
		await page.goto('/');
		const trigger = page.getByRole('button', { name: 'Preferences', exact: true });
		await trigger.click();
		const dialog = page.getByRole('dialog', { name: 'Cookie preferences', exact: true });
		await expect(dialog).toBeVisible();
		await expect(dialog.getByRole('checkbox', { name: /Essential/ })).toBeChecked();
		await expect(dialog.getByRole('checkbox', { name: /Essential/ })).toBeDisabled();
		const analytics = dialog.getByRole('checkbox', { name: /^Analytics/ });
		await analytics.focus();
		await analytics.press('Space');
		await expect(analytics).toBeChecked();
		await page.screenshot({ path: testInfo.outputPath(`cookie-preferences-${width}.png`) });
		expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
		await page.keyboard.press('Escape');
		await expect(dialog).not.toBeVisible();
		await page.reload();
		await expect(page.getByRole('button', { name: 'Allow all', exact: true })).toHaveCount(0);
		await page.goto('/privacy');
		await expect(page.getByText(/Saved choice: analytics on/)).toBeVisible();
		await page.getByRole('button', { name: 'Reset cookie consent', exact: true }).click();
		await page.getByRole('button', { name: 'Reset', exact: true }).click();
		await expect(page.getByRole('button', { name: 'Preferences', exact: true })).toBeVisible();
	});
}

test('review regression: cookie preferences actions stay above the banner on short phones', async ({ page }, testInfo) => {
	await page.setViewportSize({ width: 375, height: 667 });
	await page.goto('/');
	await page.getByRole('button', { name: 'Preferences', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'Cookie preferences', exact: true });
	await expect(dialog).toBeVisible();
	const dialogLayer = await dialog.evaluate((el) => Number(getComputedStyle(el).zIndex));
	const bannerLayer = await page.locator('.cookie-banner').evaluate((el) => Number(getComputedStyle(el).zIndex));
	expect(dialogLayer).toBeGreaterThan(bannerLayer);
	const save = dialog.getByRole('button', { name: 'Save selection', exact: true });
	await save.scrollIntoViewIfNeeded();
	await page.screenshot({ path: testInfo.outputPath('cookie-short-phone.png'), animations: 'disabled' });
	await save.click({ trial: true, timeout: 3000 });
	await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
	await expect(dialog).not.toBeVisible();
	await expect(page.getByRole('button', { name: 'Allow all', exact: true })).toBeVisible();
});
