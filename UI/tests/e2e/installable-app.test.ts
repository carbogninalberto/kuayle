import { expect, test, type Page } from '@playwright/test';

// Production-build checks for the installable app. The service worker stays
// off under automation unless a test opts in, so other browser tests keep
// their request mocks.
const OPT_IN_KEY = 'kuayle-service-worker-tests';

async function signedOut(page: Page) {
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', (route) =>
		route.fulfill({ status: 401, json: { error: { code: 'UNAUTHORIZED', message: 'Sign in' } } })
	);
}

test('publishes a manifest, icons and iOS home-screen tags', async ({ page, request }) => {
	const response = await request.get('/manifest.webmanifest');
	expect(response.ok()).toBe(true);
	const manifest = await response.json();
	expect(manifest).toMatchObject({ name: 'Kuayle', short_name: 'Kuayle', start_url: '/', scope: '/', display: 'standalone' });
	expect(manifest.theme_color).toBe(manifest.background_color);
	const sizes = manifest.icons.map((icon: { sizes: string; purpose: string }) => `${icon.sizes} ${icon.purpose}`);
	expect(sizes).toEqual(expect.arrayContaining(['192x192 any', '512x512 any', '512x512 maskable']));
	for (const icon of [...manifest.icons.map((item: { src: string }) => item.src), '/icons/apple-touch-icon.png']) {
		const image = await request.get(icon);
		expect(image.ok(), icon).toBe(true);
		expect(image.headers()['content-type']).toBe('image/png');
	}

	await signedOut(page);
	await page.goto('/login');
	await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href', /manifest\.webmanifest$/);
	await expect(page.locator('link[rel="apple-touch-icon"]')).toHaveAttribute('href', /apple-touch-icon\.png$/);
	await expect(page.locator('meta[name="apple-mobile-web-app-capable"]')).toHaveAttribute('content', 'yes');
	await expect(page.locator('meta[name="apple-mobile-web-app-title"]')).toHaveAttribute('content', 'Kuayle');
	// The title bar follows the active theme (the default dark theme here).
	await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute('content', '#111111');
});

test('does not register the service worker under automation without opting in', async ({ page }) => {
	await signedOut(page);
	await page.goto('/login');
	await expect(page.getByRole('button', { name: /sign in|log in/i }).first()).toBeVisible();
	await page.waitForTimeout(500);
	expect(await page.evaluate(async () => (await navigator.serviceWorker.getRegistrations()).length)).toBe(0);
});

test('caches only the app shell and serves an offline page for navigations', async ({ page, context }) => {
	await page.addInitScript((key) => localStorage.setItem(key, '1'), OPT_IN_KEY);
	await signedOut(page);
	await page.goto('/login');
	await page.evaluate(async () => {
		await navigator.serviceWorker.ready;
	});
	const cached = await page.evaluate(async () => {
		const keys = await caches.keys();
		const urls: string[] = [];
		for (const key of keys) {
			for (const request of await (await caches.open(key)).keys()) urls.push(new URL(request.url).pathname);
		}
		return { keys, urls };
	});
	expect(cached.keys).toHaveLength(1);
	expect(cached.keys[0]).toMatch(/^kuayle-shell-/);
	expect(cached.urls).toEqual(expect.arrayContaining(['/offline.html', '/manifest.webmanifest', '/icons/icon-192.png']));
	expect(cached.urls.some((path) => path.startsWith('/_app/immutable/'))).toBe(true);
	expect(cached.urls.filter((path) => path.startsWith('/api/') || path.endsWith('.json') || path === '/' || path === '/login')).toEqual([]);

	// Pages and API responses never enter the cache, even after use.
	await page.reload();
	await expect(page.getByRole('button', { name: /sign in|log in/i }).first()).toBeVisible();
	const afterReload = await page.evaluate(async () => {
		const urls: string[] = [];
		for (const key of await caches.keys())
			for (const request of await (await caches.open(key)).keys()) urls.push(new URL(request.url).pathname);
		return urls;
	});
	expect(afterReload.filter((path) => path.startsWith('/api/') || path === '/login')).toEqual([]);

	await context.setOffline(true);
	await page.goto('/test/my-issues');
	await expect(page.getByRole('heading', { name: "You're offline" })).toBeVisible();
	await context.setOffline(false);
});
