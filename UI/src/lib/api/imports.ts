import { api } from './client';

export type ImportStatus =
    | 'draft'
    | 'previewed'
    | 'queued'
    | 'running'
    | 'completed'
    | 'failed'
    | 'cancelled';

export interface ImportJob {
    id: string;
    source: string;
    source_site: string;
    status: ImportStatus;
    config: Record<string, unknown>;
    mapping: ImportMapping;
    preview: ImportPreview | null;
    total: number;
    processed: number;
    created_count: number;
    updated_count: number;
    skipped_count: number;
    failed_count: number;
    error: string | null;
    cancel_requested: boolean;
    created_at: string;
    updated_at: string;
    started_at: string | null;
    finished_at: string | null;
}

export interface ProjectTarget {
    team_id: string;
    project_id: string | null;
}
export interface StatusTarget {
    team_id: string;
    status_id: string;
}
export interface UserTarget {
    user_id: string | null;
}
export interface ImportMapping {
    projects: Record<string, ProjectTarget>;
    statuses: Record<string, StatusTarget>;
    users: Record<string, UserTarget>;
    priorities: Record<string, number>;
    labels: Record<string, { label_id: string | null }>;
    epic_mode: 'project' | 'parent';
    sprint_mode: 'cycle' | 'ignore';
}

export interface ImportPreview {
    counts: Record<string, number>;
    statuses: string[];
    priorities: string[];
    labels: string[];
    users: Array<{ external_id: string; email: string; display_name: string }>;
    projects: Array<{ external_id: string; name: string; key: string }>;
    unmapped_fields: string[];
}

export interface ImportItem {
    external_type: string;
    external_id: string;
    external_key: string;
    outcome: 'failed' | 'skipped';
    reason: string;
    created_at: string;
}

export interface ImportOptions {
    teams: Array<{ id: string; name: string; key: string; is_private: boolean }>;
    statuses: Record<string, Array<{ id: string; name: string; category: string }>>;
    members: Array<{ user_id: string; email: string; display_name: string }>;
}

export interface CreateImportBody {
    source: 'jira';
    config: { cloud: boolean; jql?: string };
    credentials: {
        base_url: string;
        email?: string;
        token?: string;
        bearer_token?: string;
    };
}

const path = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}/imports`;

export const imports = {
    list: (slug: string): Promise<{ imports: ImportJob[] }> => api.get(`${path(slug)}`),
    get: (slug: string, id: string): Promise<ImportJob> => api.get(`${path(slug)}/${encodeURIComponent(id)}`),
    create: (slug: string, body: CreateImportBody): Promise<ImportJob> => api.post(`${path(slug)}`, body),
    mapping: (slug: string, id: string, mapping: ImportMapping): Promise<ImportJob> =>
        api.put(`${path(slug)}/${encodeURIComponent(id)}/mapping`, { mapping }),
    preview: (slug: string, id: string): Promise<ImportPreview> =>
        api.post(`${path(slug)}/${encodeURIComponent(id)}/preview`),
    start: (slug: string, id: string): Promise<ImportJob> =>
        api.post(`${path(slug)}/${encodeURIComponent(id)}/start`),
    cancel: (slug: string, id: string): Promise<ImportJob> =>
        api.post(`${path(slug)}/${encodeURIComponent(id)}/cancel`),
    items: (slug: string, id: string): Promise<{ items: ImportItem[] }> =>
        api.get(`${path(slug)}/${encodeURIComponent(id)}/items`),
    options: (slug: string): Promise<ImportOptions> => api.get(`${path(slug)}/options`)
};
