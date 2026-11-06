import { expect, test } from '@playwright/test';

test('desktop navbar menus open and close from the keyboard and lead to their pages', async ({ page }) => {
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.goto('/compare');
	const nav = page.getByRole('navigation', { name: 'Main' });
	const trigger = nav.getByRole('button', { name: 'Alternatives' });
	await trigger.focus();
	await page.keyboard.press('Enter');
	await expect(trigger).toHaveAttribute('aria-expanded', 'true');
	const linear = nav.getByRole('link', { name: /^Linear/ });
	await expect(linear).toBeVisible();
	await page.keyboard.press('Escape');
	await expect(trigger).toHaveAttribute('aria-expanded', 'false');

	const docs = nav.getByRole('button', { name: 'Docs' });
	await docs.focus();
	await page.keyboard.press('Enter');
	await nav.getByRole('link', { name: /^Security/ }).click();
	await expect(page).toHaveURL(/\/security$/);
});

test('mobile menu sheet exposes every navigation group and closes after navigating', async ({ page }) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await page.goto('/');
	await page.getByRole('button', { name: 'Open menu' }).click();
	const sheet = page.getByRole('dialog', { name: 'Menu' });
	await expect(sheet).toBeVisible();
	const group = sheet.getByRole('button', { name: 'Alternatives' });
	await group.focus();
	await page.keyboard.press('Enter');
	await sheet.getByRole('link', { name: 'Jira', exact: true }).click();
	await expect(page).toHaveURL(/\/jira-alternative$/);
	await expect(sheet).not.toBeVisible();
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('pricing calculator recomputes list-price costs for the chosen team size', async ({ page }) => {
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.goto('/#pricing');
	const pricing = page.locator('#pricing');
	await pricing.getByRole('radio', { name: '100 people' }).click();
	// Asana Starter: $10.99 × 12 months × 100 people.
	await expect(pricing.getByText('$13,188')).toBeVisible();
	await pricing.getByRole('radio', { name: '10 people' }).click();
	await expect(pricing.getByText('$1,319')).toBeVisible();
	await expect(pricing.getByText(/Entry paid plans at list price for 10 people/)).toBeVisible();
});
