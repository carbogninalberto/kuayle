import { microphoneProblem, recordingType, transcriptionProblem, type VoiceProblem } from './voice-rules';

export type RecorderStatus = 'idle' | 'requesting' | 'recording' | 'transcribing';

/** Whether this browser can record from a microphone (secure context with
 * getUserMedia and MediaRecorder). */
export function canRecord(): boolean {
	return (
		typeof navigator !== 'undefined' &&
		typeof navigator.mediaDevices?.getUserMedia === 'function' &&
		typeof MediaRecorder !== 'undefined'
	);
}

// One microphone recording at a time: it stops by itself at the limit, can
// be cancelled (nothing is sent), and on stop the audio is handed to the
// transcription callback. The microphone is released as soon as recording ends.
export class VoiceRecorder {
	status = $state<RecorderStatus>('idle');
	elapsed = $state(0);
	private recorder: MediaRecorder | null = null;
	private stream: MediaStream | null = null;
	private chunks: Blob[] = [];
	private timer: ReturnType<typeof setInterval> | undefined;
	private startedAt = 0;
	private cancelled = false;
	private disposed = false;

	constructor(
		readonly maxSeconds: number,
		private transcribe: (audio: Blob) => Promise<void>,
		private report: (problem: VoiceProblem) => void
	) {}

	async start() {
		if (this.status !== 'idle') return;
		this.status = 'requesting';
		try {
			this.stream = await navigator.mediaDevices.getUserMedia({ audio: true });
		} catch (error) {
			this.status = 'idle';
			this.report(microphoneProblem(error));
			return;
		}
		if (this.disposed) return this.release();
		const type = recordingType((candidate) => MediaRecorder.isTypeSupported?.(candidate) ?? false);
		try {
			this.recorder = new MediaRecorder(this.stream, type ? { mimeType: type } : undefined);
		} catch {
			this.release();
			this.status = 'idle';
			this.report('unsupported');
			return;
		}
		this.chunks = [];
		this.cancelled = false;
		this.recorder.ondataavailable = (event) => {
			if (event.data.size) this.chunks.push(event.data);
		};
		this.recorder.onstop = () => void this.finish(type);
		this.recorder.start(1000);
		this.startedAt = Date.now();
		this.elapsed = 0;
		this.status = 'recording';
		this.timer = setInterval(() => {
			this.elapsed = Math.min(this.maxSeconds, (Date.now() - this.startedAt) / 1000);
			if (this.elapsed >= this.maxSeconds) this.stop();
		}, 250);
	}

	/** Stops and transcribes. */
	stop() {
		if (this.status !== 'recording' || !this.recorder) return;
		clearInterval(this.timer);
		if (this.recorder.state !== 'inactive') this.recorder.stop();
	}

	/** Stops and discards the recording. */
	cancel() {
		this.cancelled = true;
		this.stop();
	}

	dispose() {
		this.disposed = true;
		this.cancel();
		this.release();
	}

	private release() {
		for (const track of this.stream?.getTracks() ?? []) track.stop();
		this.stream = null;
	}

	private async finish(type: string) {
		clearInterval(this.timer);
		this.release();
		const chunks = this.chunks;
		this.chunks = [];
		const mimeType = this.recorder?.mimeType || type || 'audio/webm';
		this.recorder = null;
		if (this.cancelled || this.disposed) {
			this.status = 'idle';
			return;
		}
		const audio = new Blob(chunks, { type: mimeType });
		if (!audio.size) {
			this.status = 'idle';
			this.report('empty');
			return;
		}
		this.status = 'transcribing';
		try {
			await this.transcribe(audio);
		} catch (error) {
			this.report(transcriptionProblem(error));
		} finally {
			this.status = 'idle';
		}
	}
}
