export const DEV_MACHINE_EVENT_RETENTION = 200;
export const DEV_MACHINE_LOG_RETENTION = 500;

export function appendRecentTelemetry<T extends { id: number }>(current: T[], incoming: T[], limit: number) {
	const existingIds = new Set(current.map((item) => item.id));
	const additions = incoming.filter((item) => {
		if (existingIds.has(item.id)) return false;
		existingIds.add(item.id);
		return true;
	});
	const merged = [...current, ...additions];
	const dropped = Math.max(0, merged.length - limit);
	return {
		items: dropped > 0 ? merged.slice(dropped) : merged,
		dropped
	};
}
