export const AVATAR_MAX_BYTES = 2 * 1024 * 1024;
export const AVATAR_TYPES = ['image/png', 'image/jpeg', 'image/webp', 'image/gif'];

export type AvatarIssue = 'type' | 'size' | 'url';

export function validateAvatarFile(file: { type: string; size: number }): AvatarIssue | null {
	if (!AVATAR_TYPES.includes(file.type)) return 'type';
	if (file.size > AVATAR_MAX_BYTES) return 'size';
	return null;
}

export function validateAvatarUrl(raw: string): AvatarIssue | null {
	try {
		const url = new URL(raw.trim());
		return url.protocol === 'http:' || url.protocol === 'https:' ? null : 'url';
	} catch {
		return 'url';
	}
}
