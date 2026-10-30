import { api } from './client';

export interface UploadError {
	status: number;
	error?: { code?: string; message?: string };
}

// fetch has no upload progress, so images upload through XHR with the same
// session cookie; an expired session is refreshed once, as the client does.
export function uploadWithProgress<T>(
	path: string,
	file: File,
	onProgress?: (fraction: number) => void,
	retried = false
): Promise<T> {
	return new Promise<T>((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		xhr.open('POST', path);
		xhr.withCredentials = true;
		xhr.responseType = 'json';
		xhr.upload.onprogress = (event) => {
			if (event.lengthComputable) onProgress?.(event.loaded / event.total);
		};
		xhr.onerror = () => reject({ status: 0 } satisfies UploadError);
		xhr.onload = () => {
			if (xhr.status === 401 && !retried) {
				api.post<void>('/api/auth/refresh', undefined, { redirectOnUnauthorized: false }).then(
					() => uploadWithProgress<T>(path, file, onProgress, true).then(resolve, reject),
					() => reject({ status: 401 } satisfies UploadError)
				);
				return;
			}
			if (xhr.status >= 200 && xhr.status < 300) resolve(xhr.response as T);
			else reject({ status: xhr.status, ...(xhr.response ?? {}) } satisfies UploadError);
		};
		const form = new FormData();
		form.append('file', file, file.name);
		xhr.send(form);
	});
}
