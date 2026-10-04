import { api } from './client';
import type { User, LoginRequest, RegisterRequest, UpdateProfileRequest } from '$lib/types/auth';

export function login(req: LoginRequest): Promise<User> {
	return api.post<User>('/api/auth/login', req, { redirectOnUnauthorized: false, retryUnauthorized: false });
}

export function register(req: RegisterRequest): Promise<User> {
	return api.post<User>('/api/auth/register', req, { redirectOnUnauthorized: false, retryUnauthorized: false });
}

export function logout(): Promise<void> {
	return api.post<void>('/api/auth/logout');
}

export function getMe(redirectOnUnauthorized = true): Promise<User> {
	return api.get<User>('/api/auth/me', { redirectOnUnauthorized });
}

export function updateProfile(req: UpdateProfileRequest): Promise<User> {
	return api.patch<User>('/api/auth/me', req);
}
