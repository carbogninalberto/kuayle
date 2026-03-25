import { api } from './client';
import type { Cycle, CreateCycleRequest, UpdateCycleRequest, CompleteCycleRequest, CycleBurndownPoint, VelocityPoint } from '$lib/types/cycle';

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

export function completeCycle(slug: string, teamId: string, cycleId: string, data?: CompleteCycleRequest): Promise<{ cycle: Cycle; carried_over_count: number }> {
	return api.post<{ cycle: Cycle; carried_over_count: number }>(`/api/workspaces/${slug}/teams/${teamId}/cycles/${cycleId}/complete`, data);
}

export function deleteCycle(slug: string, teamId: string, cycleId: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/teams/${teamId}/cycles/${cycleId}`);
}

export function getCycleBurndown(slug: string, teamId: string, cycleId: string): Promise<CycleBurndownPoint[]> {
	return api.get<CycleBurndownPoint[]>(`/api/workspaces/${slug}/teams/${teamId}/cycles/${cycleId}/burndown`);
}

export function getCycleVelocity(slug: string, teamId: string): Promise<VelocityPoint[]> {
	return api.get<VelocityPoint[]>(`/api/workspaces/${slug}/teams/${teamId}/cycles/velocity`);
}
