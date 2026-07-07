import { api } from './client';
import type { User, LoginRequest, RegisterRequest, UpdateProfileRequest } from '$lib/types/auth';

export function login(req: LoginRequest): Promise<User> {
	return api.post<User>('/api/auth/login', req);
}

export function register(req: RegisterRequest): Promise<User> {
	return api.post<User>('/api/auth/register', req);
}

export function logout(): Promise<void> {
	return api.post<void>('/api/auth/logout');
}

export function getMe(): Promise<User> {
	return api.get<User>('/api/auth/me');
}

export function updateProfile(req: UpdateProfileRequest): Promise<User> {
	return api.patch<User>('/api/auth/me', req);
}
