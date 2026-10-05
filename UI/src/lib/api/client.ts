import { goto } from '$app/navigation';

export interface ApiRequestOptions extends RequestInit {
	/** Let public onboarding routes preserve their own destination on a missing session. */
	redirectOnUnauthorized?: boolean;
	/** Credential submissions must report their error without attempting a session refresh. */
	retryUnauthorized?: boolean;
}

class ApiClient {
	private baseUrl = '';
	private refreshing: Promise<void> | null = null;

	async fetch<T>(path: string, options: ApiRequestOptions = {}): Promise<T> {
		const res = await this.fetchResponse(path, options);

		if (!res.ok) {
			const error = await res.json().catch(() => ({
				error: { code: 'UNKNOWN', message: res.statusText }
			}));
			throw error;
		}

		if (res.status === 204) return undefined as T;
		const body = await res.text();
		if (!body) return undefined as T;
		return JSON.parse(body) as T;
	}

	private async fetchResponse(path: string, options: ApiRequestOptions = {}, refreshed = false): Promise<Response> {
		const { redirectOnUnauthorized = true, retryUnauthorized = true, ...requestOptions } = options;
		const headers = new Headers(options.headers);
		if (options.body && !(options.body instanceof FormData) && !headers.has('Content-Type')) {
			headers.set('Content-Type', 'application/json');
		}
		const res = await fetch(`${this.baseUrl}${path}`, {
			...requestOptions,
			credentials: 'include',
			headers
		});

		if (res.status === 401) {
			if (retryUnauthorized && !refreshed && !path.includes('/auth/refresh')) {
				try {
					await this.refresh();
				} catch (error) {
					if (redirectOnUnauthorized) void goto('/login');
					throw error;
				}
				return this.fetchResponse(path, options, true);
			}
			if (redirectOnUnauthorized) void goto('/login');
		}

		return res;
	}

	private async refresh(): Promise<void> {
		if (this.refreshing) return this.refreshing;
		this.refreshing = this.fetch<void>('/api/auth/refresh', { method: 'POST', redirectOnUnauthorized: false }).finally(
			() => {
				this.refreshing = null;
			}
		);
		return this.refreshing;
	}

	get<T>(path: string, options: ApiRequestOptions = {}): Promise<T> {
		return this.fetch<T>(path, options);
	}

	post<T>(path: string, body?: unknown, options: ApiRequestOptions = {}): Promise<T> {
		return this.fetch<T>(path, { ...options, method: 'POST', body: body ? JSON.stringify(body) : undefined });
	}

	patch<T>(path: string, body?: unknown): Promise<T> {
		return this.fetch<T>(path, { method: 'PATCH', body: body ? JSON.stringify(body) : undefined });
	}

	put<T>(path: string, body?: unknown): Promise<T> {
		return this.fetch<T>(path, { method: 'PUT', body: body ? JSON.stringify(body) : undefined });
	}

	delete<T>(path: string): Promise<T> {
		return this.fetch<T>(path, { method: 'DELETE' });
	}

	deleteWithBody<T>(path: string, body?: unknown): Promise<T> {
		return this.fetch<T>(path, { method: 'DELETE', body: body ? JSON.stringify(body) : undefined });
	}

	async blob(path: string): Promise<{ blob: Blob; filename: string | null }> {
		const res = await this.fetchResponse(path);
		if (!res.ok) throw await res.json().catch(() => ({ error: { code: 'UNKNOWN', message: res.statusText } }));
		const disposition = res.headers.get('Content-Disposition');
		const filename = disposition?.match(/filename="?([^";]+)"?/i)?.[1] ?? null;
		return { blob: await res.blob(), filename };
	}

	postForm<T>(path: string, form: FormData): Promise<T> {
		return this.fetch<T>(path, { method: 'POST', body: form });
	}
}

export const api = new ApiClient();
