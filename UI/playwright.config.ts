import { defineConfig } from '@playwright/test';

export default defineConfig({
	testDir: 'tests/e2e',
	forbidOnly: !!process.env.CI,
	retries: 0,
	workers: process.env.CI ? 2 : undefined,
	reporter: process.env.CI ? [['dot'], ['html', { open: 'never' }]] : 'list',
	webServer: {
		command: 'npm run build && npm run preview -- --port 4174',
		port: 4174,
		reuseExistingServer: false,
		timeout: 480_000
	},
	use: {
		baseURL: 'http://localhost:4174',
		trace: 'retain-on-failure',
		screenshot: 'only-on-failure',
		video: 'retain-on-failure'
	}
});
