import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import { defineConfig } from 'vite';
import { paraglideVitePlugin } from '@inlang/paraglide-js';

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
				ws: true
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
	}
});
