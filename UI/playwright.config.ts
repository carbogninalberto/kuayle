import { defineConfig } from '@playwright/test';

export default defineConfig({
	testDir: 'tests/e2e',
	webServer: {
		command: 'npm run build && npm run preview -- --port 4174',
		port: 4174,
		reuseExistingServer: false,
		timeout: 480_000
	},
	use: {
		baseURL: 'http://localhost:4174'
	}
});
