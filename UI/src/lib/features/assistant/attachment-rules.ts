// Client-side checks mirror the server limits so the composer can explain a
// refusal before uploading; the server still sniffs and enforces everything.
export const MAX_ATTACHMENTS = 6;
export const MAX_ATTACHMENT_BYTES = 10 * 1024 * 1024;
export const ATTACHMENT_TYPES = ['image/png', 'image/jpeg', 'image/webp', 'image/gif'];

export interface CandidateFile {
	name: string;
	type: string;
	size: number;
}
export interface RejectedFile {
	name: string;
	reason: 'type' | 'size' | 'count';
}

export function acceptImages<T extends CandidateFile>(files: Iterable<T>, existing: number) {
	const accepted: T[] = [];
	const rejected: RejectedFile[] = [];
	for (const file of files) {
		if (!ATTACHMENT_TYPES.includes(file.type)) rejected.push({ name: file.name, reason: 'type' });
		else if (file.size > MAX_ATTACHMENT_BYTES) rejected.push({ name: file.name, reason: 'size' });
		else if (existing + accepted.length >= MAX_ATTACHMENTS) rejected.push({ name: file.name, reason: 'count' });
		else accepted.push(file);
	}
	return { accepted, rejected };
}

export function formatBytes(size: number) {
	if (size < 1024) return `${size} B`;
	if (size < 1024 * 1024) return `${Math.round(size / 1024)} KB`;
	return `${(size / 1024 / 1024).toFixed(1)} MB`;
}
