import type { RunUsage } from '$lib/types/dev-machine';

// What an agent run (or a task, summed over its runs) spent, as its harness
// reported it: the parts that were reported, in a fixed order, formatted for
// the reader's locale. Token counts are compact (12.3K); the cost is the
// harness's estimate in US dollars, with the digits of a cost below a cent.
export type UsageKind = 'input' | 'output' | 'cache_read' | 'cache_write' | 'cost';

export interface UsagePart {
	kind: UsageKind;
	value: string;
}

type Reported = { [K in keyof RunUsage]?: RunUsage[K] | null };

const counts: [UsageKind, keyof RunUsage][] = [
	['input', 'input_tokens'],
	['output', 'output_tokens'],
	['cache_read', 'cache_read_tokens'],
	['cache_write', 'cache_write_tokens']
];

export function usageParts(usage: Reported | null | undefined, locale: string): UsagePart[] {
	if (!usage) return [];
	const compact = new Intl.NumberFormat(locale, { notation: 'compact', maximumFractionDigits: 1 });
	const parts: UsagePart[] = [];
	for (const [kind, field] of counts) {
		const value = usage[field];
		if (typeof value === 'number') parts.push({ kind, value: compact.format(value) });
	}
	const cost = usage.cost_usd;
	if (typeof cost === 'number') {
		const digits = cost > 0 && cost < 0.01 ? 4 : 2;
		parts.push({
			kind: 'cost',
			value: new Intl.NumberFormat(locale, {
				style: 'currency',
				currency: 'USD',
				minimumFractionDigits: 2,
				maximumFractionDigits: digits
			}).format(cost)
		});
	}
	return parts;
}
