import { expect, type Locator, type Page } from '@playwright/test';

// The task page has one panel toggle in its top bar (KUA-331); the side panel's own tab
// bar is the only place that switches between Details, Agents, Workflow, Files, Notebook
// and Checks. On a phone the same panel is a sheet that the toggle opens.
export const panelToggle = (page: Page): Locator => page.getByTestId('panel-toggle');
export const sidePanel = (page: Page): Locator => page.getByTestId('side-panel');
export const panelTabs = (page: Page): Locator => sidePanel(page).getByRole('toolbar', { name: 'Side panels' });

// Opens the side panel (when it is closed) on the named tab. The panel may open by itself on
// a wide screen and its tab may flip while the conversation loads, so the whole step retries
// until the panel shows the tab. A very narrow panel lists its tabs under "More panels".
export async function openPanel(page: Page, name: string) {
	const toggle = panelToggle(page);
	const side = sidePanel(page);
	await expect(toggle).toBeVisible();
	await expect(async () => {
		if (!(await side.isVisible())) await toggle.click();
		const tabs = panelTabs(page);
		const tab = tabs.getByRole('button', { name, exact: true });
		await expect(tab.or(tabs.getByRole('button', { name: 'More panels', exact: true })).first()).toBeVisible({ timeout: 5000 });
		if (await tab.isVisible()) {
			if ((await tab.getAttribute('aria-pressed')) !== 'true') await tab.click();
		} else {
			await tabs.getByRole('button', { name: 'More panels', exact: true }).click();
			await page.getByRole('menuitemradio', { name, exact: true }).click();
		}
		await expect(tab).toHaveAttribute('aria-pressed', 'true', { timeout: 5000 });
	}).toPass();
}

// Closes the side panel when it is open, from its own header.
export async function closePanel(page: Page) {
	await expect(panelToggle(page)).toBeVisible();
	await expect(async () => {
		if (await sidePanel(page).isVisible()) await sidePanel(page).getByRole('button', { name: 'Close panel' }).click();
		await expect(sidePanel(page)).toHaveCount(0, { timeout: 5000 });
	}).toPass();
}
