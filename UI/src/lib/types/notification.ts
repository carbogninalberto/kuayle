export interface Notification {
	id: string;
	issue_id: string | null;
	type: string;
	title: string;
	read_at: string | null;
	snoozed_until: string | null;
	archived_at: string | null;
	created_at: string;
}
