#!/usr/bin/env node

import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const BUILD_DIR = resolve(process.argv[2] || join(ROOT, 'build'));
const ORIGIN = 'https://kuayle.com';
const ROUTES = [
	'/',
	'/features',
	'/features/issue-tracking',
	'/features/cycles',
	'/features/projects',
	'/features/github-integration',
	'/features/real-time-sync',
	'/features/keyboard-shortcuts',
	'/features/views-and-triage',
	'/features/teams-and-access-control',
	'/features/analytics-insights',
	'/features/dev-machines',
	'/self-hosting',
	'/self-hosting/docker-compose',
	'/self-hosting/requirements',
	'/self-hosting/configuration',
	'/self-hosting/updating',
	'/self-hosting/storage',
	'/self-hosting/github-app',
	'/self-hosting/dev-machines',
	'/open-source',
	'/license',
	'/security',
	'/about',
	'/roadmap',
	'/privacy',
	'/compare',
	'/compare/kuayle-vs-linear',
	'/compare/kuayle-vs-plane',
	'/alternatives',
	'/alternatives/open-source-issue-trackers',
	'/alternatives/self-hosted-issue-trackers'
];

let errors = 0;
let warnings = 0;
const titles = new Map();
const descriptions = new Map();

function fail(message) {
	console.error(`ERROR: ${message}`);
	errors += 1;
}

function warn(message) {
	console.warn(`WARN: ${message}`);
	warnings += 1;
}

let contentModifiedDates = {};
try {
	contentModifiedDates = JSON.parse(readFileSync(join(ROOT, 'src/lib/data/content-modified.json'), 'utf8'));
} catch (error) {
	fail(`content modification registry is invalid (${error.message})`);
}

function routeFile(route) {
	if (route === '/') return join(BUILD_DIR, 'index.html');
	const relative = route.slice(1);
	const candidates = [join(BUILD_DIR, `${relative}.html`), join(BUILD_DIR, relative, 'index.html')];
	return candidates.find(existsSync);
}

function htmlFor(route) {
	const file = routeFile(route);
	if (!file) {
		fail(`${route}: build output is missing`);
		return '';
	}
	return readFileSync(file, 'utf8');
}

function attribute(tag, name) {
	return tag.match(new RegExp(`\\b${name}=["']([^"']*)["']`, 'i'))?.[1];
}

function count(html, pattern) {
	return [...html.matchAll(pattern)].length;
}

const routeHtml = new Map(ROUTES.map((route) => [route, htmlFor(route)]));
const DEV_MACHINE_STATUS_ROUTES = [
	'/',
	'/features',
	'/features/dev-machines',
	'/self-hosting',
	'/self-hosting/dev-machines',
	'/about',
	'/roadmap',
	'/compare/kuayle-vs-linear',
	'/compare/kuayle-vs-plane'
];

for (const route of Object.keys(contentModifiedDates)) {
	if (!ROUTES.includes(route)) fail(`content modification registry contains unknown route ${route}`);
}

for (const [route, html] of routeHtml) {
	if (!html) continue;
	const text = html.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');
	if (/\bapp[- ]preview\b/i.test(text)) fail(`${route}: unsupported app-preview claim`);
	if (/durable lifecycle events/i.test(text)) fail(`${route}: analytics provenance is overstated`);
	if (/\bf8k2m9\b/i.test(text)) fail(`${route}: invalid short machine-routing example`);

	const title = html.match(/<title>([\s\S]*?)<\/title>/i)?.[1]?.trim();
	const description = html.match(/<meta\s+[^>]*name=["']description["'][^>]*>/i)?.[0];
	const canonical = html.match(/<link\s+[^>]*rel=["']canonical["'][^>]*>/i)?.[0];
	const robots = html.match(/<meta\s+[^>]*name=["']robots["'][^>]*>/i)?.[0];
	const h1Count = count(html, /<h1\b[^>]*>/gi);

	if (!title) fail(`${route}: missing title`);
	if (!description || !attribute(description, 'content')) fail(`${route}: missing meta description`);
	if (!canonical || attribute(canonical, 'href') !== `${ORIGIN}${route}`) {
		fail(`${route}: canonical must be ${ORIGIN}${route}`);
	}
	if (!robots || !attribute(robots, 'content')?.includes('index')) fail(`${route}: missing index robots directive`);
	if (h1Count !== 1) fail(`${route}: expected one H1, found ${h1Count}`);

	const descriptionText = description ? attribute(description, 'content') : undefined;
	if (title) {
		if (titles.has(title)) fail(`${route}: duplicate title also used by ${titles.get(title)}`);
		titles.set(title, route);
	}
	if (descriptionText) {
		if (descriptions.has(descriptionText))
			fail(`${route}: duplicate description also used by ${descriptions.get(descriptionText)}`);
		descriptions.set(descriptionText, route);
	}

	for (const required of ['og:title', 'og:description', 'og:url', 'og:image', 'og:image:alt']) {
		if (!new RegExp(`<meta\\s+[^>]*property=["']${required}["']`, 'i').test(html))
			fail(`${route}: missing ${required}`);
	}
	for (const required of [
		'twitter:card',
		'twitter:title',
		'twitter:description',
		'twitter:image',
		'twitter:image:alt'
	]) {
		if (!new RegExp(`<meta\\s+[^>]*name=["']${required}["']`, 'i').test(html)) fail(`${route}: missing ${required}`);
	}

	const jsonLdBlocks = [...html.matchAll(/<script\s+type=["']application\/ld\+json["']>([\s\S]*?)<\/script>/gi)];
	const jsonLdItems = [];
	if (jsonLdBlocks.length === 0) fail(`${route}: missing JSON-LD`);
	for (const block of jsonLdBlocks) {
		try {
			const parsed = JSON.parse(block[1]);
			jsonLdItems.push(...(Array.isArray(parsed) ? parsed : [parsed]));
		} catch (error) {
			fail(`${route}: invalid JSON-LD (${error.message})`);
		}
	}

	const ogTypeTag = html.match(/<meta\s+[^>]*property=["']og:type["'][^>]*>/i)?.[0];
	const isArticle = ogTypeTag && attribute(ogTypeTag, 'content') === 'article';
	const expectedModifiedAt = contentModifiedDates[route];
	if (isArticle) {
		if (!expectedModifiedAt) {
			fail(`${route}: article is missing from the content modification registry`);
		} else {
			if (!/^\d{4}-\d{2}-\d{2}$/.test(expectedModifiedAt)) {
				fail(`${route}: modification date must use YYYY-MM-DD`);
			}
			const modifiedTag = html.match(/<meta\s+[^>]*property=["']article:modified_time["'][^>]*>/i)?.[0];
			if (!modifiedTag || attribute(modifiedTag, 'content') !== expectedModifiedAt) {
				fail(`${route}: article modification metadata must be ${expectedModifiedAt}`);
			}
			if (!jsonLdItems.some((item) => item && typeof item === 'object' && item.dateModified === expectedModifiedAt)) {
				fail(`${route}: JSON-LD dateModified must be ${expectedModifiedAt}`);
			}
		}
	} else if (expectedModifiedAt) {
		fail(`${route}: modification registry entry is not rendered as an article`);
	}

	for (const match of html.matchAll(/<img\b[^>]*>/gi)) {
		const tag = match[0];
		if (attribute(tag, 'alt') === undefined) fail(`${route}: image missing alt attribute`);
		if (!attribute(tag, 'width') || !attribute(tag, 'height')) fail(`${route}: image missing intrinsic dimensions`);
	}

	for (const match of html.matchAll(/<a\b[^>]*href=["']([^"']+)["'][^>]*>/gi)) {
		const href = match[1];
		if (href.startsWith('#')) {
			const id = href.slice(1);
			if (!new RegExp(`\\bid=["']${id}["']`).test(html)) fail(`${route}: broken fragment ${href}`);
			continue;
		}
		if (!href.startsWith('/') && !href.startsWith(ORIGIN)) continue;
		const parsed = new URL(href, ORIGIN);
		if (parsed.origin !== ORIGIN) continue;
		const target = parsed.pathname === '' ? '/' : parsed.pathname.replace(/\/$/, '') || '/';
		if (!ROUTES.includes(target) && !existsSync(join(BUILD_DIR, target.slice(1)))) {
			fail(`${route}: broken internal link ${href}`);
		}
	}
}

for (const route of DEV_MACHINE_STATUS_ROUTES) {
	const html = routeHtml.get(route);
	if (!html) continue;
	const text = html.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');
	if (!/Dev Machines/i.test(text)) fail(`${route}: missing Dev Machines status context`);
	if (!/unreleased/i.test(text)) fail(`${route}: Dev Machines must be labeled unreleased`);
}

const savedViewsHtml = routeHtml.get('/features/views-and-triage');
if (savedViewsHtml) {
	const text = savedViewsHtml.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');
	if (!/list\/board layout selection is not stored/i.test(text)) {
		fail('/features/views-and-triage: saved-view layout persistence must be described accurately');
	}
}

const homepageHtml = routeHtml.get('/');
if (homepageHtml) {
	const text = homepageHtml.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');
	if (!/manual IDE and terminal commits are not automatically attached to issues/i.test(text)) {
		fail('/: manual commit tracking must not be presented as automatic issue activity');
	}
	if (!/0123456789abcdef0123\s*\.kuayle-machines\.example\.net/i.test(text)) {
		fail('/: machine-routing example must use the generated 20-character hexadecimal form');
	}
}

const analyticsHtml = routeHtml.get('/features/analytics-insights');
if (analyticsHtml) {
	const text = analyticsHtml.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');
	if (!/current issue data and stored lifecycle timestamps/i.test(text)) {
		fail('/features/analytics-insights: insight data provenance must be explicit');
	}
	if (!/remaining-work line: total created minus net completed/i.test(text)) {
		fail('/features/analytics-insights: burn-up scope semantics must be explicit');
	}
	if (!/P50, P75 and P95/i.test(text)) {
		fail('/features/analytics-insights: duration aggregation semantics must be explicit');
	}
}

const devMachinesHtml = routeHtml.get('/features/dev-machines');
if (devMachinesHtml) {
	const text = devMachinesHtml.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');
	if (/interactive and autonomous modes|either interactively or as bounded/i.test(text)) {
		fail('/features/dev-machines: an interactive agent attachment flow must not be advertised');
	}
	if (!/direct interactive terminal use/i.test(text) || !/bounded autonomous runs/i.test(text)) {
		fail('/features/dev-machines: terminal and dashboard agent modes must be distinguished');
	}
}

const devMachinesSetupHtml = routeHtml.get('/self-hosting/dev-machines');
if (devMachinesSetupHtml) {
	const text = devMachinesSetupHtml.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');
	if (!/Compose provisioning prerequisite separately requires the gateway database password/i.test(text)) {
		fail('/self-hosting/dev-machines: gateway-password validation boundary must be explicit');
	}
	if (!/optional system updater also mounts the socket/i.test(text)) {
		fail('/self-hosting/dev-machines: Docker socket holders must be described completely');
	}
}

const sitemapFile = join(BUILD_DIR, 'sitemap.xml');
if (!existsSync(sitemapFile)) {
	fail('sitemap.xml is missing');
} else {
	const sitemap = readFileSync(sitemapFile, 'utf8');
	const locations = [...sitemap.matchAll(/<loc>([^<]+)<\/loc>/g)].map((match) => match[1]);
	const expected = ROUTES.map((route) => `${ORIGIN}${route}`);
	if (new Set(locations).size !== locations.length) fail('sitemap.xml contains duplicate URLs');
	for (const location of expected) {
		if (!locations.includes(location)) fail(`sitemap.xml is missing ${location}`);
	}
	for (const location of locations) {
		if (!expected.includes(location)) fail(`sitemap.xml contains unexpected URL ${location}`);
	}
}

const robotsFile = join(BUILD_DIR, 'robots.txt');
if (!existsSync(robotsFile)) fail('robots.txt is missing');
else if (!readFileSync(robotsFile, 'utf8').includes(`Sitemap: ${ORIGIN}/sitemap.xml`))
	fail('robots.txt has no canonical sitemap declaration');

const screenshot = join(BUILD_DIR, 'product-screenshot.png');
if (existsSync(screenshot) && readFileSync(screenshot).byteLength > 700_000)
	warn('product-screenshot.png exceeds 700 KB');

const assetsDirectory = join(BUILD_DIR, '_app/immutable/assets');
const builtCss = existsSync(assetsDirectory)
	? readdirSync(assetsDirectory)
			.filter((file) => file.endsWith('.css'))
			.map((file) => readFileSync(join(assetsDirectory, file), 'utf8'))
			.join('\n')
	: '';
if (!/@media\s*\(forced-colors:\s*active\)/i.test(builtCss)) {
	fail('production CSS is missing the forced-colors media query');
}
if (!/\.gradient-text\{[^}]*color:\s*CanvasText[^}]*-webkit-text-fill-color:\s*CanvasText[^}]*\}/i.test(builtCss)) {
	fail('production CSS is missing the readable forced-colors gradient-text fallback');
}

console.log(`SEO validation: ${errors} error(s), ${warnings} warning(s), ${ROUTES.length} routes checked.`);
if (errors > 0) process.exit(1);
