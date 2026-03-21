import { test, expect } from '@playwright/test';

test('login page loads', async ({ page }) => {
	await page.goto('/login');
	await expect(page.getByText('Kuayle')).toBeVisible();
	await expect(page.getByText('Sign in to your account')).toBeVisible();
});

test('login form has required fields', async ({ page }) => {
	await page.goto('/login');
	await expect(page.getByLabel('Email')).toBeVisible();
	await expect(page.getByLabel('Password')).toBeVisible();
});
