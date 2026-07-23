import { expect, test } from '@playwright/test';

test('all indexable routes fit a 320px viewport', async ({ page, request }) => {
	const sitemap = await request.get('/sitemap.xml');
	expect(sitemap.ok()).toBe(true);
	const xml = await sitemap.text();
	const routes = [...xml.matchAll(/<loc>([^<]+)<\/loc>/g)].map((match) => new URL(match[1]).pathname);
	expect(routes).toHaveLength(32);
	const overflowingRoutes: Array<{ route: string; overflow: number; offenders: unknown[] }> = [];

	for (const route of routes) {
		await page.goto(route, { waitUntil: 'networkidle' });
		const layout = await page.evaluate(() => {
			const viewportWidth = document.documentElement.clientWidth;
			const scrollWidth = Math.max(document.documentElement.scrollWidth, document.body.scrollWidth);
			const offenders = [...document.body.querySelectorAll<HTMLElement>('*')]
				.map((element) => ({ element, rect: element.getBoundingClientRect() }))
				.filter(({ rect }) => rect.right > viewportWidth + 0.5 || rect.left < -0.5)
				.slice(0, 5)
				.map(({ element, rect }) => ({
					tag: element.tagName.toLowerCase(),
					className: element.className,
					left: Math.round(rect.left),
					right: Math.round(rect.right)
				}));
			return { viewportWidth, scrollWidth, offenders };
		});

		if (layout.scrollWidth > layout.viewportWidth) {
			overflowingRoutes.push({
				route,
				overflow: layout.scrollWidth - layout.viewportWidth,
				offenders: layout.offenders
			});
		}
	}

	expect(overflowingRoutes).toEqual([]);
});
