export function formatDate(date: string): string {
	return new Date(date).toLocaleDateString('en-US', {
		month: 'short',
		day: 'numeric',
		year: 'numeric'
	});
}

export function formatRelativeTime(date: string): string {
	const now = Date.now();
	const then = new Date(date).getTime();
	const diff = now - then;
	const seconds = Math.floor(diff / 1000);
	const minutes = Math.floor(seconds / 60);
	const hours = Math.floor(minutes / 60);
	const days = Math.floor(hours / 24);

	if (days > 7) return formatDate(date);
	if (days > 0) return `${days}d ago`;
	if (hours > 0) return `${hours}h ago`;
	if (minutes > 0) return `${minutes}m ago`;
	return 'just now';
}

export function truncate(str: string, length: number): string {
	return str.length > length ? str.slice(0, length) + '...' : str;
}
