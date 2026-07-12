import { api } from './client';

// Types

export interface AnalyticsOverview {
	total_issues: number;
	open_issues: number;
	completed_issues: number;
	overdue_issues: number;
	total_projects: number;
	total_members: number;
	started_issues?: number;
	unassigned_issues?: number;
	completion_rate?: number;
	avg_lead_time_hours?: number | null;
	avg_cycle_time_hours?: number | null;
}

export interface DistributionItem {
	status_id: string;
	name: string;
	color: string | null;
	category: string;
	count: number;
}

export interface PriorityDistributionItem {
	priority: number;
	count: number;
}

export interface AnalyticsDistribution {
	by_status?: DistributionItem[];
	by_priority?: PriorityDistributionItem[];
}

export type AnalyticsMeasure = 'issue_count' | 'issue_age' | 'lead_time' | 'cycle_time' | 'triage_time';
export type AnalyticsSlice =
	| 'none'
	| 'status_type'
	| 'status'
	| 'priority'
	| 'assignee'
	| 'team'
	| 'project'
	| 'cycle'
	| 'label'
	| 'creator';
export type AnalyticsSegment = AnalyticsSlice;

export interface AnalyticsFilterParams {
	team_id?: string;
	project_id?: string;
	cycle_id?: string;
	assignee_id?: string;
	creator_id?: string;
	status_id?: string;
	status_type?: string;
	priority?: string;
	label_id?: string;
	include_sub_issues?: boolean;
	include_triage?: boolean;
}

export interface AnalyticsScopeParams {
	team_id?: string;
}

export interface InsightsParams extends AnalyticsFilterParams {
	measure: AnalyticsMeasure;
	slice?: AnalyticsSlice;
	segment?: AnalyticsSegment;
	from?: string;
	to?: string;
}

export interface InsightsGroup {
	key: string;
	label: string;
	color?: string | null;
	count?: number;
	value?: number | null;
	p50?: number | null;
	p75?: number | null;
	p95?: number | null;
	segments?: InsightsGroup[];
}

export interface InsightsPoint {
	issue_id: string;
	identifier: string;
	title: string;
	value?: number | null;
	slice_key?: string;
	segment_key?: string;
}

export interface AnalyticsInsights {
	measure: AnalyticsMeasure;
	slice?: AnalyticsSlice;
	segment?: AnalyticsSegment;
	unit?: string;
	total_count?: number;
	aggregate?: number | null;
	groups?: InsightsGroup[];
	points?: InsightsPoint[];
}

export interface BurnupParams extends AnalyticsFilterParams {
	from: string;
	to: string;
	interval?: 'day' | 'week' | 'month';
}

export interface BurnupPoint {
	date: string;
	created?: number;
	completed?: number;
	total_created?: number;
	total_completed?: number;
	scope?: number;
}

export interface AnalyticsBurnup {
	interval: string;
	from: string;
	to: string;
	points?: BurnupPoint[];
}

// Helpers

function dateString(d: Date): string {
	const y = d.getFullYear();
	const m = String(d.getMonth() + 1).padStart(2, '0');
	const day = String(d.getDate()).padStart(2, '0');
	return `${y}-${m}-${day}`;
}

export function defaultDateRange(dayCount = 90): { from: string; to: string } {
	const today = new Date();
	today.setHours(12, 0, 0, 0);
	const to = dateString(today);
	const from = new Date(today);
	from.setDate(from.getDate() - Math.max(0, dayCount - 1));
	return { from: dateString(from), to };
}

function buildQuery(params: AnalyticsScopeParams | InsightsParams | BurnupParams): string {
	const query = new URLSearchParams();
	for (const [key, value] of Object.entries(params)) {
		if (typeof value === 'boolean') query.set(key, value ? 'true' : 'false');
		if (typeof value === 'string' && value !== '' && value !== 'none') query.set(key, value);
	}
	const qs = query.toString();
	return qs ? `?${qs}` : '';
}

// API functions

export function getAnalyticsOverview(slug: string, params: AnalyticsScopeParams = {}): Promise<AnalyticsOverview> {
	return api.get<AnalyticsOverview>(`/api/workspaces/${slug}/analytics/overview${buildQuery(params)}`);
}

export function getAnalyticsDistribution(
	slug: string,
	params: AnalyticsScopeParams = {}
): Promise<AnalyticsDistribution> {
	return api.get<AnalyticsDistribution>(`/api/workspaces/${slug}/analytics/distribution${buildQuery(params)}`);
}

export function getAnalyticsInsights(slug: string, params: InsightsParams): Promise<AnalyticsInsights> {
	const query = buildQuery(params);
	return api.get<AnalyticsInsights>(`/api/workspaces/${slug}/analytics/insights${query}`);
}

export function getAnalyticsBurnup(slug: string, params: BurnupParams): Promise<AnalyticsBurnup> {
	const query = buildQuery(params);
	return api.get<AnalyticsBurnup>(`/api/workspaces/${slug}/analytics/burnup${query}`);
}
