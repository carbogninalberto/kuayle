import { api } from './client';

export interface AnalyticsOverview {
	total_issues: number;
	open_issues: number;
	completed_issues: number;
	overdue_issues: number;
	total_projects: number;
	total_members: number;
}

export interface StatusCount {
	status: string;
	count: number;
}

export interface PriorityCount {
	priority: number;
	count: number;
}

export interface IssueDistribution {
	by_status: StatusCount[];
	by_priority: PriorityCount[];
}

export function getOverview(slug: string): Promise<AnalyticsOverview> {
	return api.get<AnalyticsOverview>(`/api/workspaces/${slug}/analytics/overview`);
}

export function getIssueDistribution(slug: string): Promise<IssueDistribution> {
	return api.get<IssueDistribution>(`/api/workspaces/${slug}/analytics/distribution`);
}
