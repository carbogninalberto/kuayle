import { goto } from '$app/navigation';

class ApiClient {
	private baseUrl = '';
	private refreshing: Promise<void> | null = null;

	async fetch<T>(path: string, options: RequestInit = {}): Promise<T> {
		const res = await fetch(`${this.baseUrl}${path}`, {
			...options,
			credentials: 'include',
			headers: {
				'Content-Type': 'application/json',
				...options.headers
			}
		});

		if (res.status === 401) {
			if (!path.includes('/auth/refresh')) {
				await this.refresh();
				return this.fetch<T>(path, options);
			}
			goto('/login');
			throw new Error('Unauthorized');
		}

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

	private async refresh(): Promise<void> {
		if (this.refreshing) return this.refreshing;
		this.refreshing = this.fetch<void>('/api/auth/refresh', { method: 'POST' }).finally(() => {
			this.refreshing = null;
		});
		return this.refreshing;
	}

	get<T>(path: string): Promise<T> {
		return this.fetch<T>(path);
	}

	post<T>(path: string, body?: unknown): Promise<T> {
		return this.fetch<T>(path, { method: 'POST', body: body ? JSON.stringify(body) : undefined });
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
}

export const api = new ApiClient();
