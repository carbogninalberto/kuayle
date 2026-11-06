import assert from 'node:assert/strict';
import test from 'node:test';

import {
	alternativeSlug,
	annualLicenseCost,
	competitors,
	tierACompetitors
} from '../../src/lib/data/competitors.ts';

const expectedSlugs = [
	'linear',
	'jira',
	'asana',
	'github-projects',
	'gitlab',
	'shortcut',
	'clickup',
	'monday-dev',
	'youtrack',
	'trello',
	'notion-projects',
	'basecamp',
	'zenhub',
	'azure-devops-boards',
	'plane',
	'itsaplan',
	'huly',
	'openproject',
	'taiga',
	'vikunja',
	'leantime',
	'redmine',
	'focalboard',
	'gitea',
	'forgejo',
	'gogs',
	'kanboard',
	'wekan'
];

const expectedTierA = [
	'linear',
	'jira',
	'plane',
	'asana',
	'clickup',
	'youtrack',
	'openproject',
	'taiga',
	'gitlab',
	'trello',
	'itsaplan'
];

test('inventory covers every listed competitor once with current first-party sources', () => {
	assert.deepEqual(competitors.map((competitor) => competitor.slug).sort(), [...expectedSlugs].sort());
	assert.equal(new Set(competitors.map((competitor) => competitor.slug)).size, competitors.length);
	for (const competitor of competitors) {
		assert.ok(competitor.sources.length > 0, `${competitor.slug} has sources`);
		for (const source of competitor.sources) {
			assert.match(source.url, /^https:\/\//, `${competitor.slug} source uses HTTPS`);
			assert.equal(source.checkedAt, '2026-11-06');
			assert.ok(source.label.length > 0);
		}
	}
});

test('Tier A follows relevance without unapproved keyword-volume estimates', () => {
	assert.deepEqual(tierACompetitors.map((competitor) => competitor.slug).sort(), [...expectedTierA].sort());
	for (const competitor of tierACompetitors) {
		assert.equal(alternativeSlug(competitor), `${competitor.slug}-alternative`);
		assert.ok(competitor.tierJustification.length > 0);
	}
});

test('annual licence costs use annual-billing monthly list prices without hiding flat-price products', () => {
	const linear = competitors.find((competitor) => competitor.slug === 'linear');
	assert.ok(linear);
	assert.equal(annualLicenseCost(linear, 20), 20 * linear.pricing.monthlyPerSeatAnnualBillingUsd * 12);
	assert.equal(annualLicenseCost(linear, 0), 0);
	const basecamp = competitors.find((competitor) => competitor.slug === 'basecamp');
	assert.ok(basecamp);
	assert.equal(annualLicenseCost(basecamp, 20), null);
});

test('price summaries come from recorded pricing and never invent a per-seat figure', async () => {
	const { pricingSummary, mcpMark } = await import('../../src/lib/data/competitor-types.ts');
	const byslug = (slug) => competitors.find((competitor) => competitor.slug === slug);
	assert.equal(pricingSummary(byslug('linear')), 'From $10 per user a month, billed yearly');
	assert.equal(pricingSummary(byslug('jira')), 'From $8.48 per user a month, billed monthly');
	assert.equal(pricingSummary(byslug('itsaplan')), '$240 a year, flat');
	assert.equal(pricingSummary(byslug('redmine')), 'Free');
	assert.equal(mcpMark(byslug('linear')), 'yes');
	assert.equal(mcpMark({ mcp: null }), null);
	assert.equal(mcpMark({ mcp: 'no' }), 'no');
});

test('every Tier A competitor has exactly one alternative page with sourced, keyword-led content', async () => {
	const { alternativePages } = await import('../../src/lib/data/alternative-pages.ts');
	assert.deepEqual(alternativePages.map((page) => page.competitor).sort(), [...expectedTierA].sort());
	for (const page of alternativePages) {
		const competitor = competitors.find((item) => item.slug === page.competitor);
		assert.ok(competitor.sources.length >= 2 || page.competitor === 'itsaplan', `${page.competitor} has sources`);
		assert.match(page.title, /alternative/i, `${page.competitor} title targets "alternative"`);
		assert.ok(page.title.length <= 90, `${page.competitor} title length`);
		assert.ok(page.description.length >= 110 && page.description.length <= 170, `${page.competitor} description length ${page.description.length}`);
		assert.ok(page.faq.length >= 4, `${page.competitor} FAQ`);
		assert.ok(page.rows.length >= 8, `${page.competitor} table rows`);
		for (const other of page.others) {
			assert.ok(expectedTierA.includes(other) && other !== page.competitor, `${page.competitor} links ${other}`);
		}
	}
});

test('vendors that publish only monthly prices are costed at monthly billing, never as yearly list prices', async () => {
	const { annualCostAtMonthlyBilling } = await import('../../src/lib/data/competitor-types.ts');
	const jira = competitors.find((competitor) => competitor.slug === 'jira');
	assert.equal(annualLicenseCost(jira, 25), null);
	assert.equal(annualCostAtMonthlyBilling(jira, 25), 2544);
	assert.equal(annualCostAtMonthlyBilling(jira, 25, 'business'), 4668);
	const linear = competitors.find((competitor) => competitor.slug === 'linear');
	assert.equal(annualCostAtMonthlyBilling(linear, 25), null);
});
