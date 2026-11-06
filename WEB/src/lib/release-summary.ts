/** First bold headline of a release body, without issue keys, as a one-line summary. */
export function releaseSummary(body: unknown): string | undefined {
	if (typeof body !== 'string') return undefined;
	const headline = body.match(/\*\*(.+?)\*\*/)?.[1];
	if (!headline) return undefined;
	const cleaned = headline.replace(/\s*\((?:[A-Z]+-\d+(?:,\s*)?)+\)/g, '').replace(/[.:]\s*$/, '').trim();
	return cleaned.length > 0 && cleaned.length <= 140 ? cleaned : undefined;
}
