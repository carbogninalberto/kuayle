import assert from 'node:assert/strict';
import test from 'node:test';
import {
	acceptImages,
	formatBytes,
	MAX_ATTACHMENTS,
	MAX_ATTACHMENT_BYTES
} from '../../src/lib/features/assistant/attachment-rules.ts';

const file = (name, type, size = 100) => ({ name, type, size });

test('accepts supported images up to the per-message limit', () => {
	const result = acceptImages([file('a.png', 'image/png'), file('b.jpg', 'image/jpeg'), file('c.webp', 'image/webp')], 4);
	assert.deepEqual(
		result.accepted.map((f) => f.name),
		['a.png', 'b.jpg']
	);
	assert.deepEqual(result.rejected, [{ name: 'c.webp', reason: 'count' }]);
	assert.equal(MAX_ATTACHMENTS, 6);
});

test('rejects unsupported types and oversized files with a reason', () => {
	const result = acceptImages(
		[
			file('icon.svg', 'image/svg+xml'),
			file('doc.pdf', 'application/pdf'),
			file('huge.gif', 'image/gif', MAX_ATTACHMENT_BYTES + 1),
			file('ok.gif', 'image/gif', MAX_ATTACHMENT_BYTES)
		],
		0
	);
	assert.deepEqual(
		result.accepted.map((f) => f.name),
		['ok.gif']
	);
	assert.deepEqual(result.rejected, [
		{ name: 'icon.svg', reason: 'type' },
		{ name: 'doc.pdf', reason: 'type' },
		{ name: 'huge.gif', reason: 'size' }
	]);
});

test('formats sizes for thumbnails', () => {
	assert.equal(formatBytes(900), '900 B');
	assert.equal(formatBytes(2048), '2 KB');
	assert.equal(formatBytes(5 * 1024 * 1024), '5.0 MB');
});
