import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
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
		sveltekit()
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
