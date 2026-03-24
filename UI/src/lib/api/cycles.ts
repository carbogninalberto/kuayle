import { api } from './client';
import type { Cycle, CreateCycleRequest, UpdateCycleRequest, CycleBurndownPoint } from '$lib/types/cycle';

export function listCycles(slug: string, teamId: string): Promise<Cycle[]> {
	return api.get<Cycle[]>(`/api/workspaces/${slug}/teams/${teamId}/cycles`);
}

export function getCycle(slug: string, teamId: string, cycleId: string): Promise<Cycle> {
	return api.get<Cycle>(`/api/workspaces/${slug}/teams/${teamId}/cycles/${cycleId}`);
}

export function createCycle(slug: string, teamId: string, data: CreateCycleRequest): Promise<Cycle> {
	return api.post<Cycle>(`/api/workspaces/${slug}/teams/${teamId}/cycles`, data);
}

export function updateCycle(slug: string, teamId: string, cycleId: string, data: UpdateCycleRequest): Promise<Cycle> {
	return api.patch<Cycle>(`/api/workspaces/${slug}/teams/${teamId}/cycles/${cycleId}`, data);
}

export function completeCycle(slug: string, teamId: string, cycleId: string): Promise<Cycle> {
	return api.post<Cycle>(`/api/workspaces/${slug}/teams/${teamId}/cycles/${cycleId}/complete`);
}

export function deleteCycle(slug: string, teamId: string, cycleId: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/teams/${teamId}/cycles/${cycleId}`);
}

export function getCycleBurndown(slug: string, teamId: string, cycleId: string): Promise<CycleBurndownPoint[]> {
	return api.get<CycleBurndownPoint[]>(`/api/workspaces/${slug}/teams/${teamId}/cycles/${cycleId}/burndown`);
}
