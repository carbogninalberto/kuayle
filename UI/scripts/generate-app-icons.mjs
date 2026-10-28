// Renders the installable-app icons in static/icons from static/favicon.svg
// with the Playwright Chromium the browser tests already use:
//   npx playwright install chromium && node scripts/generate-app-icons.mjs
// The PNGs are committed; rerun after changing the favicon.
import { readFile, mkdir } from 'node:fs/promises';
import { chromium } from '@playwright/test';

const root = new URL('../static/', import.meta.url);
const svg = await readFile(new URL('favicon.svg', root), 'utf8');
const source = `data:image/svg+xml;base64,${Buffer.from(svg).toString('base64')}`;
// The default dark theme's background (--color-bg in src/app.css).
const background = '#111111';
// "any" icons are the transparent logo; maskable and Apple icons are
// full-bleed, with the logo inside the 80% safe zone launchers may crop to.
const icons = [
	{ file: 'icon-192.png', size: 192, scale: 1, fill: null },
	{ file: 'icon-512.png', size: 512, scale: 1, fill: null },
	{ file: 'icon-maskable-512.png', size: 512, scale: 0.6, fill: background },
	{ file: 'apple-touch-icon.png', size: 180, scale: 0.72, fill: background }
];

await mkdir(new URL('icons/', root), { recursive: true });
const browser = await chromium.launch();
try {
	const page = await browser.newPage({ deviceScaleFactor: 1 });
	for (const icon of icons) {
		const logo = Math.round(icon.size * icon.scale);
		await page.setViewportSize({ width: icon.size, height: icon.size });
		await page.setContent(
			`<html><body style="margin:0;width:${icon.size}px;height:${icon.size}px;display:grid;place-items:center;background:${icon.fill ?? 'transparent'}"><img src="${source}" width="${logo}" height="${logo}"></body></html>`
		);
		await page.waitForFunction(() => document.images[0]?.complete);
		await page.screenshot({
			path: new URL(`icons/${icon.file}`, root).pathname,
			omitBackground: icon.fill === null
		});
		console.log(`static/icons/${icon.file}`);
	}
} finally {
	await browser.close();
}
