import { api } from './client';
import type { User, LoginRequest, RegisterRequest, UpdateProfileRequest } from '$lib/types/auth';
import { uploadWithProgress } from './upload';

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

export function forgotPassword(email: string): Promise<void> {
	return api.post<void>('/api/auth/forgot-password', { email }, { redirectOnUnauthorized: false, retryUnauthorized: false });
}

export function resetPassword(token: string, password: string): Promise<void> {
	return api.post<void>('/api/auth/reset-password', { token, password }, { redirectOnUnauthorized: false, retryUnauthorized: false });
}

export function uploadAvatar(file: File, onProgress?: (fraction: number) => void): Promise<User> {
	return uploadWithProgress<User>('/api/auth/me/avatar', file, onProgress);
}
