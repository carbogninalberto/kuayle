import { api } from './client';
import { emitAppRefresh } from './refresh';
import { uploadWithProgress } from './upload';
import type { Workspace } from '$lib/types/workspace';

export function listWorkspaces(): Promise<Workspace[]> {
	return api.get<Workspace[]>('/api/workspaces');
}

export function getWorkspace(slug: string): Promise<Workspace> {
	return api.get<Workspace>(`/api/workspaces/${slug}`);
}

export function createWorkspace(name: string, slug: string): Promise<Workspace> {
	return api.post<Workspace>('/api/workspaces', { name, slug });
}

export async function updateWorkspace(
	slug: string,
	data: {
		name?: string;
		logo_url?: string | null;
		share_link_min_role?: string;
	}
): Promise<Workspace> {
	const workspace = await api.patch<Workspace>(`/api/workspaces/${slug}`, data);
	emitAppRefresh(['workspace'], slug);
	return workspace;
}

export async function uploadWorkspaceLogo(
	slug: string,
	file: File,
	onProgress?: (fraction: number) => void
): Promise<Workspace> {
	const workspace = await uploadWithProgress<Workspace>(
		`/api/workspaces/${slug}/logo`,
		file,
		onProgress
	);
	emitAppRefresh(['workspace'], slug);
	return workspace;
}

export async function deleteWorkspace(slug: string): Promise<{ status: string }> {
	const result = await api.delete<{ status: string }>(`/api/workspaces/${slug}`);
	emitAppRefresh(['workspace'], slug);
	return result;
}

export interface WorkspaceExportManifest {
	format: string;
	version: number;
	exported_at: string;
	source_workspace_id: string;
	source_workspace_name: string;
	source_workspace_slug: string;
	counts: Record<string, number>;
	omitted: string[];
	warnings: string[];
	requires_reconfiguration: string[];
}

export interface WorkspaceImportPreview {
	manifest: WorkspaceExportManifest;
	name: string;
	slug: string;
	missing_users: string[];
}

export interface WorkspaceImportResult {
	id: string;
	name: string;
	slug: string;
	counts: Record<string, number>;
	warnings: string[];
	requires_reconfiguration: string[];
}

export async function downloadWorkspaceExport(slug: string): Promise<void> {
	const { blob, filename } = await api.blob(`/api/workspaces/${slug}/export`);
	const url = URL.createObjectURL(blob);
	try {
		const link = document.createElement('a');
		link.href = url;
		link.download = filename ?? `${slug}.kuayle.zip`;
		link.click();
	} finally {
		URL.revokeObjectURL(url);
	}
}

function importForm(file: File, name?: string, slug?: string): FormData {
	const form = new FormData();
	form.append('file', file);
	if (name !== undefined) form.append('name', name);
	if (slug !== undefined) form.append('slug', slug);
	return form;
}

export function previewWorkspaceImport(file: File): Promise<WorkspaceImportPreview> {
	return api.postForm<WorkspaceImportPreview>('/api/workspaces/import/preview', importForm(file));
}

export async function importWorkspace(file: File, name: string, slug: string): Promise<WorkspaceImportResult> {
	const result = await api.postForm<WorkspaceImportResult>('/api/workspaces/import', importForm(file, name, slug));
	emitAppRefresh(['workspace']);
	return result;
}
