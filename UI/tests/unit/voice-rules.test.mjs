import assert from 'node:assert/strict';
import test from 'node:test';
import {
	appendTranscript,
	downloadPercent,
	formatDuration,
	microphoneProblem,
	recordingType,
	transcriptionProblem
} from '../../src/lib/features/voice/voice-rules.ts';

test('prefers WebM/Opus and falls back to MP4 for iOS Safari', () => {
	assert.equal(recordingType(() => true), 'audio/webm;codecs=opus');
	assert.equal(recordingType((type) => type === 'audio/mp4'), 'audio/mp4');
	assert.equal(recordingType(() => false), '', 'the browser default is used when nothing matches');
});

test('formats the recording timer', () => {
	assert.equal(formatDuration(0), '0:00');
	assert.equal(formatDuration(9.9), '0:09');
	assert.equal(formatDuration(61), '1:01');
	assert.equal(formatDuration(300), '5:00');
	assert.equal(formatDuration(-3), '0:00');
});

test('adds the transcript after the draft without replacing it', () => {
	assert.equal(appendTranscript('', ' hello world '), 'hello world');
	assert.equal(appendTranscript('Look at', 'the login page'), 'Look at the login page');
	assert.equal(appendTranscript('First line\n', 'second'), 'First line\nsecond');
	assert.equal(appendTranscript('Keep me', '   '), 'Keep me', 'an empty transcript changes nothing');
});

test('classifies microphone and transcription failures', () => {
	assert.equal(microphoneProblem({ name: 'NotAllowedError' }), 'denied');
	assert.equal(microphoneProblem({ name: 'NotFoundError' }), 'no_microphone');
	assert.equal(microphoneProblem(new Error('x')), 'failed');
	assert.equal(transcriptionProblem({ status: 422, error: { code: 'VOICE_TOO_LONG' } }), 'too_long');
	assert.equal(transcriptionProblem({ error: { code: 'VOICE_BUSY' } }), 'busy');
	assert.equal(transcriptionProblem({ error: { code: 'VOICE_OFF' } }), 'off');
	assert.equal(transcriptionProblem({ status: 0 }), 'failed');
});

test('reports download progress as a bounded percentage', () => {
	assert.equal(downloadPercent(0, 0), 0);
	assert.equal(downloadPercent(50, 200), 25);
	assert.equal(downloadPercent(300, 200), 100);
});
