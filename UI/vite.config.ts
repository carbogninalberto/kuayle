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
		/(?:^|\/)node_modules\/(?:@xyflow\/|layerchart\/)/.test(location)
	);
}

/** A Rolldown warning is a dependency notice only when one of its locations is in those libraries. */
function isDependencyWarning(warning: {
	id?: string;
	exporter?: string;
	ids?: string[];
	loc?: { file?: string };
}): boolean {
	return (
		isDependencyNotice(warning.id) ||
		isDependencyNotice(warning.exporter) ||
		(warning.ids?.some(isDependencyNotice) ?? false) ||
		isDependencyNotice(warning.loc?.file)
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
			// Svelte notices from node_modules are already suppressed by vite-plugin-svelte;
			// the dependency notices are bundler warnings filtered below.
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
	build: browserTests
		? {
				chunkSizeWarningLimit: 100_000,
				// Vite 8 passes build.rolldownOptions.onwarn to Rolldown. Filter only
				// warnings tied to the noisy dependencies; every other warning,
				// including those from our own src/, still reaches the default handler.
				rolldownOptions: {
					onwarn(warning, defaultHandler) {
						if (isDependencyWarning(warning)) return;
						defaultHandler(warning);
					}
				}
			}
		: undefined
});
