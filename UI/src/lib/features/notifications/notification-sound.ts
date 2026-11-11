// Notification sounds synthesised with Web Audio: no audio files to ship or
// cache. Everything here is best effort and silent on failure, because browsers
// block audio until the page has had a user gesture.

import type { NotificationSound } from './notification-policy.ts';

export type SoundPreset = Exclude<NotificationSound, 'off'>;

export interface Tone {
	wave: 'sine' | 'triangle' | 'square' | 'sawtooth';
	/** Hz at the start of the tone. */
	frequency: number;
	/** Hz reached at the end, for a glide. */
	endFrequency?: number;
	/** Seconds after the sound begins. */
	start: number;
	/** Seconds. */
	duration: number;
	/** Peak gain, 0..1. */
	gain: number;
}

export const SOUND_PRESETS: Record<SoundPreset, Tone[]> = {
	// Two rising notes.
	chime: [
		{ wave: 'sine', frequency: 880, start: 0, duration: 0.35, gain: 0.18 },
		{ wave: 'sine', frequency: 1318.5, start: 0.12, duration: 0.5, gain: 0.16 }
	],
	// One struck note with inharmonic overtones and a long tail.
	bell: [
		{ wave: 'sine', frequency: 659.25, start: 0, duration: 1.1, gain: 0.2 },
		{ wave: 'sine', frequency: 1318.5, start: 0, duration: 0.7, gain: 0.08 },
		{ wave: 'sine', frequency: 1760, start: 0, duration: 0.45, gain: 0.05 }
	],
	// A short upward blip.
	pop: [{ wave: 'sine', frequency: 420, endFrequency: 880, start: 0, duration: 0.09, gain: 0.22 }],
	// A falling water-drop glide.
	drop: [{ wave: 'sine', frequency: 1400, endFrequency: 480, start: 0, duration: 0.22, gain: 0.2 }],
	// Two soft even beats.
	pulse: [
		{ wave: 'triangle', frequency: 587.33, start: 0, duration: 0.11, gain: 0.2 },
		{ wave: 'triangle', frequency: 587.33, start: 0.18, duration: 0.11, gain: 0.2 }
	]
};

export const SOUND_THROTTLE_MS = 5_000;
/** A context that only resumes later than this would play a stale sound. */
const RESUME_GRACE_MS = 1_000;
const ATTACK_SECONDS = 0.008;
const SILENCE = 0.0001;

interface AudioParamLike {
	setValueAtTime(value: number, time: number): unknown;
	linearRampToValueAtTime(value: number, time: number): unknown;
	exponentialRampToValueAtTime(value: number, time: number): unknown;
}
interface AudioNodeLike {
	connect(destination: unknown): unknown;
}
export interface AudioContextLike {
	state: string;
	currentTime: number;
	destination: unknown;
	resume(): Promise<void>;
	createOscillator(): AudioNodeLike & {
		type: string;
		frequency: AudioParamLike;
		start(when?: number): void;
		stop(when?: number): void;
	};
	createGain(): AudioNodeLike & { gain: AudioParamLike };
}

export interface NotificationSoundOptions {
	/** Returns a new context, or null when Web Audio is unavailable. */
	createContext?: () => AudioContextLike | null;
	now?: () => number;
	throttleMs?: number;
	/** Whether audio may start now, i.e. the page has had a user gesture. */
	canStart?: () => boolean;
}

function defaultCreateContext(): AudioContextLike | null {
	const scope = globalThis as { AudioContext?: new () => AudioContextLike; webkitAudioContext?: new () => AudioContextLike };
	const Context = scope.AudioContext ?? scope.webkitAudioContext;
	return Context ? new Context() : null;
}

function defaultCanStart(): boolean {
	// Creating or resuming a context before the first gesture makes browsers log
	// an autoplay warning, so wait for one where the browser can tell us.
	const activation = (globalThis as { navigator?: { userActivation?: { hasBeenActive?: boolean } } }).navigator
		?.userActivation;
	return activation?.hasBeenActive ?? true;
}

function schedule(context: AudioContextLike, tones: Tone[]) {
	const begin = context.currentTime + 0.01;
	for (const tone of tones) {
		const at = begin + tone.start;
		const end = at + tone.duration;
		const oscillator = context.createOscillator();
		const envelope = context.createGain();
		oscillator.type = tone.wave;
		oscillator.frequency.setValueAtTime(tone.frequency, at);
		if (tone.endFrequency) oscillator.frequency.exponentialRampToValueAtTime(tone.endFrequency, end);
		envelope.gain.setValueAtTime(SILENCE, at);
		envelope.gain.linearRampToValueAtTime(tone.gain, at + Math.min(ATTACK_SECONDS, tone.duration / 2));
		envelope.gain.exponentialRampToValueAtTime(SILENCE, end);
		oscillator.connect(envelope);
		envelope.connect(context.destination);
		oscillator.start(at);
		oscillator.stop(end + 0.02);
	}
}

export function createNotificationSound(options: NotificationSoundOptions = {}) {
	const createContext = options.createContext ?? defaultCreateContext;
	const now = options.now ?? Date.now;
	const throttleMs = options.throttleMs ?? SOUND_THROTTLE_MS;
	const canStart = options.canStart ?? defaultCanStart;
	const lastPlayed = new Map<string, number>();
	let context: AudioContextLike | null = null;

	/** Resolves to whether the sound was actually scheduled. */
	async function emit(preset: string): Promise<boolean> {
		const tones = Object.hasOwn(SOUND_PRESETS, preset) ? SOUND_PRESETS[preset as SoundPreset] : undefined;
		if (!tones) return false;
		try {
			context ??= createContext();
			if (!context) return false;
			if (context.state !== 'running') {
				const requested = now();
				await context.resume();
				if (now() - requested > RESUME_GRACE_MS) return false;
			}
			schedule(context, tones);
			return true;
		} catch {
			// Unsupported, suspended or blocked: stay silent.
			return false;
		}
	}

	return {
		/** Play for an incoming event: at most one sound per event key per throttle window. */
		async play(preset: NotificationSound | string, eventKey: string): Promise<boolean> {
			if (preset === 'off' || !canStart()) return false;
			const at = now();
			const previous = lastPlayed.get(eventKey);
			if (previous !== undefined && at - previous < throttleMs) return false;
			// Claim the window before awaiting so a burst cannot slip through.
			lastPlayed.set(eventKey, at);
			const played = await emit(preset);
			if (!played && lastPlayed.get(eventKey) === at) {
				if (previous === undefined) lastPlayed.delete(eventKey);
				else lastPlayed.set(eventKey, previous);
			}
			return played;
		},
		/** Play from the settings page: a user gesture, so never throttled. */
		preview(preset: NotificationSound | string): Promise<boolean> {
			return emit(preset);
		}
	};
}

export type NotificationSoundPlayer = ReturnType<typeof createNotificationSound>;
