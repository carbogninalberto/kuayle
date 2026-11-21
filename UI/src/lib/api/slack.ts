import { api } from './client';

export type SlackBinding = { team_id?: string; project_id?: string; channel_id: string };
export type SlackStatus = {
	configured: boolean;
	install: { name: string } | null;
	bindings: SlackBinding[];
};
export type SlackChannel = { id: string; name: string };
const path = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}/slack`;
export const slack = {
	status: (slug: string) => api.get<SlackStatus>(path(slug)),
	start: (slug: string) => api.post<{ url: string }>(path(slug) + '/install'),
	connect: (slug: string, state: string, code: string) =>
		api.post<void>(path(slug) + '/callback', { state, code }),
	disconnect: (slug: string) => api.delete<void>(path(slug)),
	channels: (slug: string) => api.get<SlackChannel[]>(path(slug) + '/channels'),
	setBinding: (slug: string, binding: SlackBinding) => api.put<void>(path(slug) + '/bindings', binding)
};
export const slackInstallKey = 'kuayle.slack.install';
