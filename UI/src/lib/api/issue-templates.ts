import { api } from './client';
import type { IssueTemplate, CreateIssueTemplateRequest } from '$lib/types/issue';

export function listTemplates(slug: string): Promise<IssueTemplate[]> {
	return api.get<IssueTemplate[]>(`/api/workspaces/${slug}/issue-templates`);
}

export function createTemplate(
	slug: string,
	data: CreateIssueTemplateRequest
): Promise<IssueTemplate> {
	return api.post<IssueTemplate>(`/api/workspaces/${slug}/issue-templates`, data);
}

export function getTemplate(slug: string, id: string): Promise<IssueTemplate> {
	return api.get<IssueTemplate>(`/api/workspaces/${slug}/issue-templates/${id}`);
}

export function updateTemplate(
	slug: string,
	id: string,
	data: Partial<CreateIssueTemplateRequest>
): Promise<IssueTemplate> {
	return api.patch<IssueTemplate>(`/api/workspaces/${slug}/issue-templates/${id}`, data);
}

export function deleteTemplate(slug: string, id: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/issue-templates/${id}`);
}
