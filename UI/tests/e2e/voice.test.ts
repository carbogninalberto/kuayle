import { test, expect, type Page } from '@playwright/test';

const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
const conversation = {
	id: 'chat-1',
	title: 'Voice',
	workspace_id: workspace.id,
	creator_id: 'user-1',
	created_at: '2026-10-26T10:00:00Z',
	updated_at: '2026-10-26T10:00:00Z'
};
const models = [
	{ id: 'faster-whisper-base', engine: 'faster-whisper', language: '', size_mb: 148 },
	{ id: 'vosk-small-it-0.22', engine: 'vosk', language: 'it', size_mb: 50 }
];

type VoiceFixture = {
	status: Record<string, unknown>;
	selects: string[];
	transcriptions: Array<{ type: string; size: number }>;
	transcript: string;
	failure: { status: number; code: string } | null;
	polls: number;
};

async function fixture(page: Page, status: Record<string, unknown>) {
	const state: VoiceFixture = { status, selects: [], transcriptions: [], transcript: 'fix the login page', failure: null, polls: 0 };
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.routeWebSocket('**/conversations/chat-1/stream*', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		const method = route.request().method();
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user-1', name: 'Owner', display_name: 'Owner', email: 'owner@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path.endsWith('/assistant/status'))
			return route.fulfill({ json: { configured: true, provider: 'openai_compatible', model: 'chat-model' } });
		if (path.endsWith('/voice')) {
			if (method === 'PUT') {
				const model = route.request().postDataJSON().model;
				state.selects.push(model);
				Object.assign(state.status, model === 'off' ? { model, state: 'off', ready: false } : { model, state: 'downloading', ready: false, downloaded_bytes: 10, total_bytes: 40 });
			} else if (state.status.state === 'downloading' && ++state.polls >= 2) {
				Object.assign(state.status, { state: 'ready', ready: true, downloaded_bytes: 40 });
			}
			return route.fulfill({ json: state.status });
		}
		if (path.endsWith('/conversations/chat-1/transcriptions')) {
			const body = route.request().postDataBuffer();
			state.transcriptions.push({ type: route.request().headers()['content-type'], size: body?.length ?? 0 });
			if (state.failure)
				return route.fulfill({ status: state.failure.status, json: { error: { code: state.failure.code, message: 'x' } } });
			return route.fulfill({ json: { text: state.transcript } });
		}
		if (path.endsWith('/conversations')) return route.fulfill({ json: method === 'POST' ? conversation : [conversation] });
		if (path.endsWith('/conversations/chat-1'))
			return route.fulfill({ json: { conversation, messages: [], tools: [], approvals: [], cursor: 0, has_more: false } });
		return route.fulfill({ json: [] });
	});
	return state;
}

// A microphone without hardware: getUserMedia resolves (or is denied) and
// MediaRecorder produces one small WebM chunk when stopped.
async function fakeMicrophone(page: Page) {
	await page.addInitScript(() => {
		const w = window as any;
		w.__micDenied = false;
		w.__tracksStopped = 0;
		Object.defineProperty(navigator, 'mediaDevices', {
			configurable: true,
			value: {
				getUserMedia: async () => {
					if (w.__micDenied) throw new DOMException('Permission denied', 'NotAllowedError');
					return { getTracks: () => [{ stop: () => w.__tracksStopped++ }] };
				}
			}
		});
		class FakeRecorder {
			state = 'inactive';
			mimeType: string;
			ondataavailable: ((event: { data: Blob }) => void) | null = null;
			onstop: (() => void) | null = null;
			constructor(_stream: unknown, options?: { mimeType?: string }) {
				this.mimeType = options?.mimeType ?? 'audio/webm';
			}
			static isTypeSupported(type: string) {
				return type.startsWith('audio/webm');
			}
			start() {
				this.state = 'recording';
			}
			stop() {
				this.state = 'inactive';
				this.ondataavailable?.({ data: new Blob([new Uint8Array([0x1a, 0x45, 0xdf, 0xa3, 1, 2, 3])], { type: this.mimeType }) });
				setTimeout(() => this.onstop?.(), 0);
			}
		}
		w.MediaRecorder = FakeRecorder;
	});
}

const ready = () => ({ model: 'faster-whisper-base', owner: true, configured: true, state: 'ready', ready: true, max_seconds: 300, max_bytes: 20971520 });

test('dictates into the composer after review, keeping the draft', async ({ page }) => {
	await fakeMicrophone(page);
	const state = await fixture(page, ready());
	await page.goto('/test/assistant?conversation=chat-1');
	const composer = page.getByRole('textbox', { name: 'Message' });
	await composer.fill('Please');
	await page.getByRole('button', { name: 'Dictate a message' }).click();
	await expect(page.getByRole('timer')).toContainText('0:00 / 5:00');
	await page.locator('form').last().screenshot({ path: test.info().outputPath('voice-recording.png') });
	await page.getByRole('button', { name: 'Stop and transcribe' }).click();
	await expect(composer).toHaveValue('Please fix the login page');
	expect(state.transcriptions).toEqual([{ type: 'audio/webm;codecs=opus', size: 7 }]);
	expect(await page.evaluate(() => (window as any).__tracksStopped)).toBe(1);
	// Nothing is sent until the user sends it.
	await expect(page.getByRole('button', { name: 'Send message' })).toBeEnabled();

	// Cancelling discards the recording without a request.
	await page.getByRole('button', { name: 'Dictate a message' }).click();
	await page.getByRole('button', { name: 'Cancel recording' }).click();
	await expect(page.getByRole('button', { name: 'Dictate a message' })).toBeVisible();
	expect(state.transcriptions).toHaveLength(1);
	await expect(composer).toHaveValue('Please fix the login page');
});

test('stops by itself at five minutes', async ({ page }) => {
	await page.clock.install();
	await fakeMicrophone(page);
	const state = await fixture(page, ready());
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByRole('button', { name: 'Dictate a message' }).click();
	await expect(page.getByRole('timer')).toBeVisible();
	await page.clock.fastForward('04:59');
	await expect(page.getByRole('timer')).toContainText('4:59 / 5:00');
	expect(state.transcriptions).toHaveLength(0);
	await page.clock.fastForward('00:02');
	await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('fix the login page');
	expect(state.transcriptions).toHaveLength(1);
});

test('explains a denied microphone and server refusals', async ({ page }) => {
	await fakeMicrophone(page);
	const state = await fixture(page, ready());
	await page.goto('/test/assistant?conversation=chat-1');
	await page.evaluate(() => ((window as any).__micDenied = true));
	await page.getByRole('button', { name: 'Dictate a message' }).click();
	await expect(page.getByText('Microphone access was denied.', { exact: false })).toBeVisible();
	expect(state.transcriptions).toHaveLength(0);

	await page.evaluate(() => ((window as any).__micDenied = false));
	state.failure = { status: 422, code: 'VOICE_TOO_LONG' };
	await page.getByRole('button', { name: 'Dictate a message' }).click();
	await page.getByRole('button', { name: 'Stop and transcribe' }).click();
	await expect(page.getByText('Recordings can be at most 5 minutes.')).toBeVisible();
	await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('');
});

test('hides the microphone when voice is off', async ({ page }) => {
	await fakeMicrophone(page);
	await fixture(page, { model: 'off', owner: false, configured: true, state: 'off', ready: false, max_seconds: 300, max_bytes: 1 });
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByRole('button', { name: 'Attach images' })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Dictate a message' })).toHaveCount(0);
});

test('owner chooses a model, follows the download and can turn voice off', async ({ page }) => {
	const state = await fixture(page, { model: 'off', owner: true, configured: true, state: 'off', ready: false, max_seconds: 300, max_bytes: 1, models });
	await page.goto('/test/settings/voice');
	await expect(page.getByRole('heading', { name: 'Voice', exact: true })).toBeVisible();
	await expect(page.getByRole('status').filter({ hasText: 'Voice is off.' })).toBeVisible();
	await page.getByRole('combobox', { name: 'Transcription model' }).click();
	await page.getByRole('option', { name: 'Vosk small · Italian · 50 MB' }).click();
	expect(state.selects).toEqual(['vosk-small-it-0.22']);
	await expect(page.getByText('Downloading the model: 25%')).toBeVisible();
	await expect(page.getByRole('progressbar', { name: 'Model download progress' })).toBeVisible();
	await expect(page.getByText('Ready. Members can dictate', { exact: false })).toBeVisible({ timeout: 10_000 });
	await page.screenshot({ path: test.info().outputPath('voice-settings.png') });
	await page.getByRole('combobox', { name: 'Transcription model' }).click();
	await page.getByRole('option', { name: 'Off' }).click();
	expect(state.selects).toEqual(['vosk-small-it-0.22', 'off']);
	await expect(page.getByRole('status').filter({ hasText: 'Voice is off.' })).toBeVisible();
	await page.setViewportSize({ width: 390, height: 844 });
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
	await page.screenshot({ path: test.info().outputPath('voice-settings-mobile.png'), fullPage: true });
});

test('members see the voice model without changing it, and an unreachable service is reported', async ({ page }) => {
	await fixture(page, { model: 'faster-whisper-base', owner: false, configured: false, state: 'unavailable', ready: false, max_seconds: 300, max_bytes: 1, models });
	await page.goto('/test/settings/voice');
	await expect(page.getByText('Only the workspace owner can change the voice model.')).toBeVisible();
	await expect(page.getByRole('combobox', { name: 'Transcription model' })).toHaveCount(0);
	await expect(page.getByText('faster-whisper base · multilingual · 148 MB')).toBeVisible();
	await expect(page.getByText('The voice service is not reachable.', { exact: false })).toBeVisible();
});
