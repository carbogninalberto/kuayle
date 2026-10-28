import { api } from './client';

export type VoiceState = 'off' | 'unavailable' | 'absent' | 'queued' | 'downloading' | 'ready' | 'failed';

export interface VoiceModel {
	id: string;
	engine: 'faster-whisper' | 'vosk';
	/** Empty for multilingual models that detect the language. */
	language: string;
	size_mb: number;
}

export interface VoiceStatus {
	model: string;
	owner: boolean;
	configured: boolean;
	state: VoiceState;
	ready: boolean;
	downloaded_bytes?: number;
	total_bytes?: number;
	error?: string;
	max_seconds: number;
	max_bytes: number;
	models: VoiceModel[];
}

const base = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}`;

export const voice = {
	status: (slug: string) => api.get<VoiceStatus>(`${base(slug)}/voice`),
	select: (slug: string, model: string) => api.put<VoiceStatus>(`${base(slug)}/voice`, { model }),
	/** Sends one recording; the transcript is returned for the composer and not stored. */
	transcribe: (slug: string, conversation: string, audio: Blob) =>
		api.fetch<{ text: string }>(`${base(slug)}/conversations/${encodeURIComponent(conversation)}/transcriptions`, {
			method: 'POST',
			body: audio,
			headers: { 'Content-Type': audio.type || 'application/octet-stream' }
		})
};
