// Calendar helpers for the registry feed and overviews. Dates are the
// registry's YYYY-MM-DD days; "today" is the viewer's local day. Free of
// Svelte and $lib imports for the Node unit tests.

/** The local calendar day of `date` as YYYY-MM-DD. */
export function isoDay(date: Date): string {
	const pad = (n: number) => String(n).padStart(2, '0');
	return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/** `day` moved by `days` calendar days. */
export function addDays(day: string, days: number): string {
	const [y, m, d] = day.split('-').map(Number);
	return isoDay(new Date(y, m - 1, d + days));
}

/** Whether a registry day is today or yesterday for the viewer. */
export function relativeDay(day: string, now: Date): 'today' | 'yesterday' | null {
	const today = isoDay(now);
	if (day === today) return 'today';
	if (day === addDays(today, -1)) return 'yesterday';
	return null;
}

/** The last `days` days up to and including today. */
export function recentPeriod(days: number, now: Date): { period_start: string; period_end: string } {
	const end = isoDay(now);
	return { period_start: addDays(end, -(Math.max(1, days) - 1)), period_end: end };
}

/** Whether a custom range is ordered and at most `maxDays` long. */
export function validPeriod(start: string, end: string, maxDays = 31): boolean {
	if (!/^\d{4}-\d{2}-\d{2}$/.test(start) || !/^\d{4}-\d{2}-\d{2}$/.test(end) || start > end) return false;
	return addDays(start, maxDays - 1) >= end;
}
