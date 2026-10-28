// Pure rules for the composer microphone, shared by the recorder and tests.

/** Recording formats in order of preference. Chrome and Firefox record
 * WebM/Opus; iOS Safari only records MP4/AAC. */
export const RECORDING_TYPES = ['audio/webm;codecs=opus', 'audio/webm', 'audio/mp4', 'audio/ogg;codecs=opus'];

export function recordingType(isSupported: (type: string) => boolean): string {
	return RECORDING_TYPES.find((type) => isSupported(type)) ?? '';
}

/** m:ss for the recording timer. */
export function formatDuration(totalSeconds: number): string {
	const seconds = Math.max(0, Math.floor(totalSeconds));
	return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
}

/** The transcript is added after what is already typed, never replacing it. */
export function appendTranscript(existing: string, transcript: string): string {
	const text = transcript.trim();
	if (!text) return existing;
	if (!existing.trim()) return text;
	return /\s$/.test(existing) ? existing + text : `${existing} ${text}`;
}

export type VoiceProblem =
	| 'denied'
	| 'no_microphone'
	| 'unsupported'
	| 'too_long'
	| 'too_large'
	| 'busy'
	| 'not_ready'
	| 'off'
	| 'empty'
	| 'failed';

/** Classifies getUserMedia errors. */
export function microphoneProblem(error: unknown): VoiceProblem {
	const name = (error as { name?: string } | null)?.name;
	if (name === 'NotAllowedError' || name === 'SecurityError') return 'denied';
	if (name === 'NotFoundError' || name === 'OverconstrainedError') return 'no_microphone';
	if (name === 'NotReadableError') return 'no_microphone';
	return 'failed';
}

/** Classifies transcription API errors by their code. */
export function transcriptionProblem(error: unknown): VoiceProblem {
	const code = (error as { error?: { code?: string } } | null)?.error?.code;
	switch (code) {
		case 'VOICE_TOO_LONG':
			return 'too_long';
		case 'VOICE_TOO_LARGE':
			return 'too_large';
		case 'VOICE_BUSY':
			return 'busy';
		case 'VOICE_NOT_READY':
			return 'not_ready';
		case 'VOICE_OFF':
		case 'VOICE_UNAVAILABLE':
			return 'off';
		case 'VOICE_UNSUPPORTED':
			return 'unsupported';
	}
	return 'failed';
}

/** Download progress as a whole percentage. */
export function downloadPercent(downloaded = 0, total = 0): number {
	if (!total) return 0;
	return Math.min(100, Math.floor((downloaded / total) * 100));
}
