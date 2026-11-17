import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import { defineConfig } from 'vite';
import { paraglideVitePlugin } from '@inlang/paraglide-js';

// Playwright sets this for both its build and preview. It lets the test build
// stay quiet without changing production builds or the dev server.
const browserTests = process.env.KUAYLE_BROWSER_TESTS === '1';

/** Only dependency notices from these libraries are hidden in browser-test builds. */
function isDependencyNotice(location: unknown): boolean {
	return (
		typeof location === 'string' &&
		(location.includes('/node_modules/@xyflow/') || location.includes('/node_modules/layerchart/'))
	);
}

export default defineConfig({
	envDir: '..',
	plugins: [
		tailwindcss(),
		paraglideVitePlugin({
			project: './project.inlang',
			outdir: './src/lib/paraglide',
			strategy: ['localStorage', 'cookie', 'baseLocale']
		}),
		sveltekit({
			preprocess: vitePreprocess(),
			// Test builds hide only Svelte notices from the two noisy dependencies;
			// warnings from our own src/ files are still handed to the default handler.
			onwarn: browserTests
				? (warning, defaultHandler) => {
						if (isDependencyNotice(warning.filename)) return;
						defaultHandler(warning);
					}
				: undefined,
			// SvelteKit 3 no longer provides `$lib`; keep the existing imports working.
			alias: { $lib: 'src/lib' },
			adapter: adapter({
				pages: 'build',
				assets: 'build',
				fallback: 'index.html',
				precompress: false,
				strict: true
			}),
			// Registered by $lib/features/pwa in production only, never in dev or
			// browser tests unless a test opts in.
			serviceWorker: { register: false }
		})
	],
	server: {
		proxy: {
			'/api': {
				target: 'http://localhost:8080',
				changeOrigin: true,
				// No backend runs during browser tests; without this the preview
				// forwards websocket upgrades to :8080 and logs ECONNREFUSED on retries.
				ws: !browserTests
			},
			// Exact path only: a plain key is a prefix match and would also catch
			// a workspace whose slug starts with "mcp".
			'^/mcp(\\?.*)?$': {
				target: 'http://localhost:8080',
				changeOrigin: true,
				// A browser opening /mcp wants the app (a workspace may have that
				// slug); MCP clients do not ask for HTML.
				bypass: (req) => (req.headers.accept?.includes('text/html') ? req.url : undefined)
			},
			'/uploads': {
				target: 'http://localhost:8080',
				changeOrigin: true
			}
		}
	},
	build: browserTests ? { chunkSizeWarningLimit: 100_000 } : undefined
});
