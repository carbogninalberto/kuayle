import { ORIGIN } from '$lib/config/site';
import { allRoutes } from '$lib/data/routes';

export const prerender = true;

export function GET() {
	const routes = allRoutes();

	const urls = routes
		.map(
			(r) => `  <url>
    <loc>${ORIGIN}${r.path}</loc>
    <priority>${r.priority}</priority>
    <changefreq>${r.changefreq}</changefreq>
  </url>`
		)
		.join('\n');

	const xml = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${urls}
</urlset>
`;

	return new Response(xml.trim() + '\n', {
		headers: {
			'Content-Type': 'application/xml; charset=utf-8'
		}
	});
}
