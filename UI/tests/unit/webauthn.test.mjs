import assert from 'node:assert/strict';
import test from 'node:test';
import {
	apiErrorCode,
	base64urlToBuffer,
	bufferToBase64url,
	conditionalMediationAvailable,
	credentialToJSON,
	isPasskeyCancellation,
	isPasskeyDuplicate,
	passkeysSupported,
	suggestPasskeyName,
	toCreationOptions,
	toRequestOptions
} from '../../src/lib/features/auth/webauthn.ts';

const bytes = (...values) => new Uint8Array(values).buffer;

test('passkeys are offered only where the browser implements WebAuthn', () => {
	assert.equal(passkeysSupported({}), false);
	assert.equal(passkeysSupported({ navigator: { credentials: {} } }), false);
	assert.equal(passkeysSupported({ PublicKeyCredential: function () {}, navigator: {} }), false);
	assert.equal(passkeysSupported({ PublicKeyCredential: function () {}, navigator: { credentials: {} } }), true);
});

test('autofill is offered only when the browser reports conditional mediation', async () => {
	assert.equal(await conditionalMediationAvailable({}), false);
	assert.equal(await conditionalMediationAvailable({ PublicKeyCredential: {} }), false);
	const reports = (value) => ({ PublicKeyCredential: { isConditionalMediationAvailable: async () => value } });
	assert.equal(await conditionalMediationAvailable(reports(true)), true);
	assert.equal(await conditionalMediationAvailable(reports(false)), false);
	const throws = { PublicKeyCredential: { isConditionalMediationAvailable: async () => { throw new Error('nope'); } } };
	assert.equal(await conditionalMediationAvailable(throws), false);
});

test('base64url round-trips binary data without padding or URL-unsafe characters', () => {
	const data = new Uint8Array([251, 255, 254, 0, 1, 62, 63]);
	const encoded = bufferToBase64url(data.buffer);
	assert.equal(encoded, '-__-AAE-Pw');
	assert.deepEqual(new Uint8Array(base64urlToBuffer(encoded)), data);
	// A view over part of a buffer encodes only its own bytes.
	assert.equal(bufferToBase64url(new Uint8Array(data.buffer, 3, 2)), 'AAE');
	for (const length of [0, 1, 2, 3, 4, 31, 32, 33]) {
		const value = Uint8Array.from({ length }, (_, i) => (i * 37) % 256);
		assert.deepEqual(new Uint8Array(base64urlToBuffer(bufferToBase64url(value))), value);
	}
});

test('creation options decode the challenge, user id and excluded credentials', () => {
	const options = toCreationOptions({
		publicKey: {
			rp: { id: 'kuayle.test', name: 'Kuayle' },
			user: { id: 'AQID', name: 'ada@example.test', displayName: 'Ada' },
			challenge: 'BAUG',
			pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
			authenticatorSelection: { residentKey: 'required', userVerification: 'required' },
			excludeCredentials: [{ type: 'public-key', id: 'BwgJ', transports: ['internal'] }]
		}
	});
	const publicKey = options.publicKey;
	assert.deepEqual(new Uint8Array(publicKey.challenge), new Uint8Array([4, 5, 6]));
	assert.deepEqual(new Uint8Array(publicKey.user.id), new Uint8Array([1, 2, 3]));
	assert.equal(publicKey.user.name, 'ada@example.test');
	assert.deepEqual(new Uint8Array(publicKey.excludeCredentials[0].id), new Uint8Array([7, 8, 9]));
	assert.deepEqual(publicKey.excludeCredentials[0].transports, ['internal']);
	assert.equal(publicKey.authenticatorSelection.residentKey, 'required');
});

test('request options decode the challenge and tolerate a missing allow list', () => {
	const discoverable = toRequestOptions({ publicKey: { challenge: 'BAUG', rpId: 'kuayle.test', userVerification: 'required' } });
	assert.deepEqual(new Uint8Array(discoverable.publicKey.challenge), new Uint8Array([4, 5, 6]));
	assert.equal(discoverable.publicKey.allowCredentials, undefined);
	assert.equal(discoverable.publicKey.rpId, 'kuayle.test');
	const listed = toRequestOptions({ publicKey: { challenge: 'BAUG', allowCredentials: [{ type: 'public-key', id: 'BwgJ' }] } });
	assert.deepEqual(new Uint8Array(listed.publicKey.allowCredentials[0].id), new Uint8Array([7, 8, 9]));
});

test('a new credential is serialized as the attestation JSON the server parses', () => {
	const json = credentialToJSON({
		id: 'AQID',
		rawId: bytes(1, 2, 3),
		type: 'public-key',
		authenticatorAttachment: 'platform',
		response: {
			clientDataJSON: bytes(10),
			attestationObject: bytes(11, 12),
			getTransports: () => ['internal', 'hybrid']
		},
		getClientExtensionResults: () => ({ credProps: { rk: true } })
	});
	assert.deepEqual(json, {
		id: 'AQID',
		rawId: 'AQID',
		type: 'public-key',
		authenticatorAttachment: 'platform',
		response: { clientDataJSON: 'Cg', attestationObject: 'Cww', transports: ['internal', 'hybrid'] },
		clientExtensionResults: { credProps: { rk: true } }
	});
});

test('an assertion is serialized with its user handle only when the authenticator returned one', () => {
	const assertion = (userHandle) =>
		credentialToJSON({
			id: 'AQID',
			rawId: bytes(1, 2, 3),
			type: 'public-key',
			response: { clientDataJSON: bytes(10), authenticatorData: bytes(20), signature: bytes(30), userHandle }
		});
	assert.deepEqual(assertion(bytes(9, 9)).response, {
		clientDataJSON: 'Cg',
		authenticatorData: 'FA',
		signature: 'Hg',
		userHandle: 'CQk'
	});
	for (const missing of [null, undefined, bytes()]) {
		assert.equal('userHandle' in assertion(missing).response, false);
	}
	assert.deepEqual(assertion(null).clientExtensionResults, {});
});

test('a dismissed browser prompt is not an error; a duplicate authenticator is its own case', () => {
	assert.equal(isPasskeyCancellation({ name: 'NotAllowedError' }), true);
	assert.equal(isPasskeyCancellation({ name: 'AbortError' }), true);
	assert.equal(isPasskeyCancellation({ name: 'SecurityError' }), false);
	assert.equal(isPasskeyCancellation(null), false);
	assert.equal(isPasskeyDuplicate({ name: 'InvalidStateError' }), true);
	assert.equal(isPasskeyDuplicate({ name: 'NotAllowedError' }), false);
	assert.equal(apiErrorCode({ error: { code: 'REAUTH_REQUIRED', message: 'x' }, status: 403 }), 'REAUTH_REQUIRED');
	assert.equal(apiErrorCode(new Error('offline')), '');
	assert.equal(apiErrorCode(undefined), '');
});

test('a new passkey gets a recognizable default name', () => {
	const ua = {
		chromeMac: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36',
		safariIphone: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1',
		edgeWindows: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0',
		firefoxLinux: 'Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0',
		chromeAndroid: 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36'
	};
	assert.deepEqual(suggestPasskeyName(ua.chromeMac), { browser: 'Chrome', os: 'macOS' });
	assert.deepEqual(suggestPasskeyName(ua.safariIphone), { browser: 'Safari', os: 'iOS' });
	assert.deepEqual(suggestPasskeyName(ua.edgeWindows), { browser: 'Edge', os: 'Windows' });
	assert.deepEqual(suggestPasskeyName(ua.firefoxLinux), { browser: 'Firefox', os: 'Linux' });
	assert.deepEqual(suggestPasskeyName(ua.chromeAndroid), { browser: 'Chrome', os: 'Android' });
	assert.equal(suggestPasskeyName('curl/8.0'), null);
});
