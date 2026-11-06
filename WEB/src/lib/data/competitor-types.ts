/**
 * Types and pure helpers for the competitor inventory.
 * Kept free of `$lib` imports so Node's unit tests can import the registry directly.
 */

/** Date the external facts in this registry were last checked against first-party sources. */
export const COMPETITORS_CHECKED_AT = '2026-11-06';

export type Mark = 'yes' | 'partial' | 'no';

export interface Source {
	label: string;
	url: string;
	checkedAt: string;
}

export interface Pricing {
	model: 'per-seat' | 'flat' | 'free' | 'quote';
	freePlan: string | null;
	entryPlan: string | null;
	/** USD per user per month on yearly billing; null when not per-seat or unpublished. */
	monthlyPerSeatAnnualBillingUsd: number | null;
	businessPlan: string | null;
	businessMonthlyPerSeatAnnualBillingUsd: number | null;
	/** Flat yearly licence price in USD for flat-priced products, when published. */
	flatAnnualUsd?: number | null;
	/** USD per user per month on monthly billing, for vendors that publish only monthly prices. */
	monthlyBillingPerSeatUsd?: number | null;
	businessMonthlyBillingPerSeatUsd?: number | null;
	notes: string | null;
}

export interface Competitor {
	slug: string;
	name: string;
	/** Name used in headings when the brand word is ambiguous (e.g. "Plane.so"). */
	displayName?: string;
	website: string;
	status: 'active' | 'maintenance' | 'discontinued';
	statusNote?: string | null;
	category: 'hosted' | 'open-source';
	tier: 'A' | 'B';
	tierJustification: string;
	license: string;
	hosting: 'cloud' | 'self-hosted' | 'both' | 'unknown';
	hostingNote: string | null;
	pricing: Pricing;
	sso: string | null;
	ai: string | null;
	mcp: string | null;
	api: string | null;
	importers: string | null;
	gitForges: string | null;
	exports: string | null;
	sources: Source[];
}

export function alternativeSlug(competitor: Pick<Competitor, 'slug'>): string {
	return `${competitor.slug}-alternative`;
}

export function alternativePath(competitor: Pick<Competitor, 'slug'>): string {
	return `/${alternativeSlug(competitor)}`;
}

/**
 * Yearly licence cost for a team on the entry paid plan, at published annual-billing list prices.
 * Returns null when the product is not priced per seat (flat, quote-only or unpublished),
 * so callers can show those prices separately instead of inventing a per-seat figure.
 */
export function annualLicenseCost(
	competitor: Pick<Competitor, 'pricing'>,
	people: number,
	plan: 'entry' | 'business' = 'entry'
): number | null {
	const { pricing } = competitor;
	if (pricing.model !== 'per-seat') return null;
	const perSeat =
		plan === 'entry' ? pricing.monthlyPerSeatAnnualBillingUsd : pricing.businessMonthlyPerSeatAnnualBillingUsd;
	if (perSeat === null || perSeat === undefined) return null;
	if (people <= 0) return 0;
	return people * perSeat * 12;
}

/**
 * Yearly cost at published monthly-billing prices (price × 12), for vendors that publish no
 * annual per-seat price. Kept separate from annualLicenseCost so pages can label it.
 */
export function annualCostAtMonthlyBilling(
	competitor: Pick<Competitor, 'pricing'>,
	people: number,
	plan: 'entry' | 'business' = 'entry'
): number | null {
	const { pricing } = competitor;
	if (pricing.model !== 'per-seat') return null;
	const perSeat = plan === 'entry' ? pricing.monthlyBillingPerSeatUsd : pricing.businessMonthlyBillingPerSeatUsd;
	if (perSeat === null || perSeat === undefined) return null;
	if (people <= 0) return 0;
	return Math.round(people * perSeat * 12 * 100) / 100;
}

export function formatUsd(amount: number): string {
	return new Intl.NumberFormat('en-US', {
		style: 'currency',
		currency: 'USD',
		maximumFractionDigits: amount % 1 === 0 ? 0 : 2
	}).format(amount);
}

/** One row of an alternative page's side-by-side table. The Kuayle side comes from kuayle-facts.ts. */
export interface ComparisonRow {
	dimension: string;
	/** Key into KUAYLE in kuayle-facts.ts. */
	kuayle:
		| 'license'
		| 'hosting'
		| 'pricing'
		| 'sso'
		| 'ai'
		| 'mcp'
		| 'api'
		| 'importers'
		| 'gitForges'
		| 'data'
		| 'views'
		| 'planning'
		| 'issues'
		| 'access'
		| 'customFields';
	mark: Mark;
	text: string;
}

export interface FaqItem {
	question: string;
	answer: string;
}

/** Content for a Tier A `/<slug>-alternative` page. External facts must be backed by the competitor's sources. */
export interface AlternativePageContent {
	/** Competitor slug in the inventory. */
	competitor: string;
	title: string;
	description: string;
	h1: string;
	intro: string;
	/** One-line main difference shown in the verdict card. */
	difference: string;
	whyLeave: { heading: string; body: string }[];
	rows: ComparisonRow[];
	/** Which plan the cost table uses, by label, e.g. "Standard" and "Premium". */
	costPlans: { entry: string | null; business: string | null };
	costNote: string;
	migration: { heading: string; body: string }[];
	stayIf: string[];
	switchIf: string[];
	/** Competitor slugs to show under "Other options". */
	others: string[];
	faq: FaqItem[];
}

/** Short price summary for matrices and cards, derived only from recorded pricing data. */
export function pricingSummary(competitor: Pick<Competitor, 'pricing'>): string {
	const p = competitor.pricing;
	if (p.model === 'free') return 'Free';
	if (p.model === 'flat' && p.flatAnnualUsd) return `${formatUsd(p.flatAnnualUsd)} a year, flat`;
	if (p.model === 'per-seat' && p.monthlyPerSeatAnnualBillingUsd != null)
		return `From ${formatUsd(p.monthlyPerSeatAnnualBillingUsd)} per user a month, billed yearly`;
	if (p.model === 'per-seat' && p.monthlyBillingPerSeatUsd != null)
		return `From ${formatUsd(p.monthlyBillingPerSeatUsd)} per user a month, billed monthly`;
	if (p.model === 'quote') return 'Price on request';
	const firstClause = p.notes?.split(/[;.]/)[0]?.trim();
	return firstClause || 'See vendor pricing';
}

/** MCP availability from the recorded `mcp` note: 'yes', 'no', or null when undocumented. */
export function mcpMark(competitor: Pick<Competitor, 'mcp'>): Mark | null {
	const value = competitor.mcp?.toLowerCase().trim();
	if (!value) return null;
	if (value.startsWith('yes')) return 'yes';
	if (value.startsWith('community')) return 'partial';
	if (value.startsWith('no')) return 'no';
	return 'partial';
}

export function hostingLabel(hosting: Competitor['hosting']): string {
	return { cloud: 'Vendor cloud only', 'self-hosted': 'Self-hosted', both: 'Cloud or self-hosted', unknown: 'Not documented' }[hosting];
}
