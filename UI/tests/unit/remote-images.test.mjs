import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import {
	REMOTE_IMAGE_ATTRIBUTE,
	allowRemoteImage,
	deferRemoteImage,
	isRemoteImageAllowed,
	onRemoteImageChoice,
	remoteImageHost,
	trustRemoteImageHost
} from '../../src/lib/security/remote-images.ts';

const origin = 'https://app.example';

// Shared with the Go sanitizer's tests: the documents the editor stores.
const contract = JSON.parse(
	readFileSync(new URL('../../../BE/pkg/sanitize/testdata/editor_html_contract.json', import.meta.url), 'utf8')
);

// The sanitizer hook only reads and writes attributes, so a map stands in for the element.
function image(attributes) {
	const values = new Map(Object.entries(attributes));
	return {
		getAttribute: (name) => values.get(name) ?? null,
		setAttribute: (name, value) => void values.set(name, value),
		removeAttribute: (name) => void values.delete(name),
		attributes: () => Object.fromEntries(values)
	};
}

function sanitized(attributes) {
	const node = image(attributes);
	deferRemoteImage(node, origin);
	return node.attributes();
}

test('images the app serves itself are not remote', () => {
	for (const src of [
		'/api/workspaces/acme/assets/0b0c3f5e-6f0f-4c0b-9d6c-0a4f4f1f8a11',
		'/api/public/assets/signed-token',
		'https://app.example/api/workspaces/acme/assets/1',
		'HTTPS://APP.EXAMPLE/api/public/assets/signed-token',
		// The browser resolves the page's own scheme without "//" against the page, so these stay on the app.
		'https:logo.png',
		'https:/tracker.example/p.png'
	]) {
		assert.equal(remoteImageHost(src, origin), null, src);
	}
});

test('any other host is remote, however the address is written', () => {
	const cases = [
		['https://tracker.example/pixel.png?who=reader', 'tracker.example'],
		['http://tracker.example/pixel.png', 'tracker.example'],
		['https://tracker.example:8443/p.png', 'tracker.example:8443'],
		['http://app.example/pixel.png', 'app.example'],
		['https://app.example.tracker.example/p.png', 'app.example.tracker.example'],
		['https://app.example@tracker.example/p.png', 'tracker.example'],
		['https:\\\\tracker.example\\p.png', 'tracker.example'],
		['http:/tracker.example/p.png', 'tracker.example'],
		['//tracker.example/p.png', 'tracker.example'],
		['http:tracker.example/p.png', 'tracker.example'],
		[' https://tracker.example/p.png', 'tracker.example'],
		['https://xn--e1afmkfd.example/p.png', 'xn--e1afmkfd.example']
	];
	for (const [src, host] of cases) assert.equal(remoteImageHost(src, origin), host, src);
});

test('the sanitizer leaves no src pointing at a remote host', () => {
	const src = 'https://tracker.example/pixel.png?who=reader';
	assert.deepEqual(sanitized({ src, alt: 'Chart', width: '50%' }), {
		[REMOTE_IMAGE_ATTRIBUTE]: src,
		alt: 'Chart',
		width: '50%'
	});
});

test('same-origin and asset images come out unchanged', () => {
	for (const src of ['/api/workspaces/acme/assets/1', '/api/public/assets/signed-token', 'https://app.example/a.png']) {
		assert.deepEqual(sanitized({ src, alt: 'Chart' }), { src, alt: 'Chart' });
	}
	assert.deepEqual(sanitized({ alt: 'No source' }), { alt: 'No source' });
});

test('an address that cannot be parsed is dropped, not offered for loading', () => {
	assert.equal(remoteImageHost('https://', origin), '');
	assert.deepEqual(sanitized({ src: 'https://', alt: 'Broken' }), { alt: 'Broken' });
});

test('content cannot mark its own image as waiting to be loaded', () => {
	// The attribute is set only by the sanitizer, from the src it removed.
	assert.deepEqual(sanitized({ [REMOTE_IMAGE_ATTRIBUTE]: 'https://tracker.example/a.png', alt: 'x' }), { alt: 'x' });
	assert.deepEqual(
		sanitized({ [REMOTE_IMAGE_ATTRIBUTE]: 'https://tracker.example/a.png', src: '/api/workspaces/acme/assets/1' }),
		{ src: '/api/workspaces/acme/assets/1' }
	);
});

test('the stored documents of the sanitizer contract hold no remote image', () => {
	// They must keep rendering exactly as stored, without a placeholder.
	assert.ok(contract.documents.length > 0);
	for (const { name, html } of contract.documents) {
		for (const [, src] of html.matchAll(/<img\b[^>]*\ssrc="([^"]*)"/g)) {
			assert.equal(remoteImageHost(src, origin), null, name);
		}
	}
});

test('a reader allows one image for the session or a whole host', () => {
	const first = 'https://images.example/one.png';
	const second = 'https://images.example/two.png';
	const elsewhere = 'https://other.example/one.png';
	let notified = 0;
	const stop = onRemoteImageChoice(() => notified++);

	assert.equal(isRemoteImageAllowed(first, origin), false);
	allowRemoteImage(first);
	assert.equal(isRemoteImageAllowed(first, origin), true);
	assert.equal(isRemoteImageAllowed(second, origin), false);
	assert.equal(notified, 1);

	trustRemoteImageHost('images.example');
	assert.equal(isRemoteImageAllowed(second, origin), true);
	assert.equal(isRemoteImageAllowed(elsewhere, origin), false);
	assert.equal(isRemoteImageAllowed('https://images.example.other.example/one.png', origin), false);
	assert.equal(notified, 2);

	stop();
	allowRemoteImage(elsewhere);
	assert.equal(notified, 2);
});
