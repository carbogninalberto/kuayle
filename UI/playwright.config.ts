import { defineConfig } from '@playwright/test';

export default defineConfig({
	testDir: 'tests/e2e',
	forbidOnly: !!process.env.CI,
	retries: 0,
	// Keep Chromium's process and thread count bounded in agent containers.
	workers: 2,
	// On GitHub Actions the github reporter also turns each failure into an annotation on its
	// spec line; it stays out of local runs, CI=1 included.
	reporter: process.env.CI
		? [['dot'], ...(process.env.GITHUB_ACTIONS ? [['github'] as ['github']] : []), ['html', { open: 'never' }]]
		: 'list',
	webServer: {
		// Shared by the build and preview below so both know this is a browser-test run.
		env: { KUAYLE_BROWSER_TESTS: '1' },
		// PLAYWRIGHT_PORT lets parallel worktrees run browser tests side by side.
		command: `npm run build && npm run preview -- --port ${process.env.PLAYWRIGHT_PORT ?? 4174}`,
		port: Number(process.env.PLAYWRIGHT_PORT ?? 4174),
		reuseExistingServer: false,
		timeout: 480_000
	},
	use: {
		baseURL: `http://localhost:${process.env.PLAYWRIGHT_PORT ?? 4174}`,
		trace: 'retain-on-failure',
		screenshot: 'only-on-failure',
		video: 'retain-on-failure',
		// Headless Chromium has no WebGL2 by default; SwiftShader matches the GPU-backed
		// terminal renderer real browsers use.
		launchOptions: { args: ['--enable-unsafe-swiftshader'] }
	}
});
