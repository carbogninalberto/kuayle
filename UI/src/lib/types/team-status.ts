import { m } from '$lib/features/i18n/messages';

export type StatusCategory = 'backlog' | 'unstarted' | 'started' | 'completed' | 'cancelled';

export interface TeamStatus {
	id: string;
	team_id: string;
	name: string;
	slug: string;
	category: StatusCategory;
	color: string | null;
	position: number;
	is_default: boolean;
	project_ids?: string[];
	created_at: string;
	updated_at: string;
}

export const CATEGORY_ORDER: StatusCategory[] = ['backlog', 'unstarted', 'started', 'completed', 'cancelled'];

export function getCategoryLabel(category: StatusCategory): string {
	const dynamicMessage = m as unknown as Record<string, () => string>;
	return dynamicMessage[`common.category.${category}`]();
}

export function getCategoryLabels(): Record<StatusCategory, string> {
	return {
		backlog: m['common.category.backlog'](),
		unstarted: m['common.category.unstarted'](),
		started: m['common.category.started'](),
		completed: m['common.category.completed'](),
		cancelled: m['common.category.cancelled']()
	};
}
