import { defineConfig } from '@playwright/test';
import base from './playwright.config';

// Optional browser-engine review of the editor on the same production build as CI.
// Install engines first: npx playwright install chromium firefox webkit
export default defineConfig({
	...base,
	testMatch: 'editor-bubble-menu.test.ts',
	workers: 2,
	projects: [
		{ name: 'chromium', use: { browserName: 'chromium' } },
		{ name: 'firefox', use: { browserName: 'firefox' } },
		{ name: 'webkit', use: { browserName: 'webkit' } }
	]
});
