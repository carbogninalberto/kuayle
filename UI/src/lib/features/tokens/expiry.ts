export type ExpiryChoice = '30d' | '90d' | '1y' | 'custom' | 'never';

export function localDateString(date: Date): string {
	return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
}

/** null means invalid; undefined means no expiration. Custom dates end in local time. */
export function expiresAt(choice: ExpiryChoice, customDate: string, now = new Date()): string | null | undefined {
	if (choice === 'never') return undefined;
	if (choice === 'custom') {
		if (!/^\d{4}-\d{2}-\d{2}$/.test(customDate)) return null;
		const expiration = new Date(`${customDate}T23:59:59`);
		if (!Number.isFinite(expiration.getTime()) || localDateString(expiration) !== customDate || expiration <= now)
			return null;
		return expiration.toISOString();
	}
	const days = { '30d': 30, '90d': 90, '1y': 365 }[choice];
	return new Date(now.getTime() + days * 24 * 60 * 60 * 1000).toISOString();
}
