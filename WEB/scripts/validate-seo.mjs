#!/usr/bin/env node

import { existsSync, readFileSync } from 'node:fs';
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

for (const [route, html] of routeHtml) {
	if (!html) continue;

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
		if (descriptions.has(descriptionText)) fail(`${route}: duplicate description also used by ${descriptions.get(descriptionText)}`);
		descriptions.set(descriptionText, route);
	}

	for (const required of ['og:title', 'og:description', 'og:url', 'og:image', 'og:image:alt']) {
		if (!new RegExp(`<meta\\s+[^>]*property=["']${required}["']`, 'i').test(html)) fail(`${route}: missing ${required}`);
	}
	for (const required of ['twitter:card', 'twitter:title', 'twitter:description', 'twitter:image', 'twitter:image:alt']) {
		if (!new RegExp(`<meta\\s+[^>]*name=["']${required}["']`, 'i').test(html)) fail(`${route}: missing ${required}`);
	}

	const jsonLdBlocks = [...html.matchAll(/<script\s+type=["']application\/ld\+json["']>([\s\S]*?)<\/script>/gi)];
	if (jsonLdBlocks.length === 0) fail(`${route}: missing JSON-LD`);
	for (const block of jsonLdBlocks) {
		try {
			JSON.parse(block[1]);
		} catch (error) {
			fail(`${route}: invalid JSON-LD (${error.message})`);
		}
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
else if (!readFileSync(robotsFile, 'utf8').includes(`Sitemap: ${ORIGIN}/sitemap.xml`)) fail('robots.txt has no canonical sitemap declaration');

const screenshot = join(BUILD_DIR, 'product-screenshot.png');
if (existsSync(screenshot) && readFileSync(screenshot).byteLength > 700_000) warn('product-screenshot.png exceeds 700 KB');

console.log(`SEO validation: ${errors} error(s), ${warnings} warning(s), ${ROUTES.length} routes checked.`);
if (errors > 0) process.exit(1);
