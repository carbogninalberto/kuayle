import assert from 'node:assert/strict';
import test from 'node:test';
import { browserGuide } from '../../src/lib/features/pwa/browser-guide.ts';

const safari = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 Version/18.6 Mobile/15E148 Safari/604.1';

test('identifies branded browsers and Telegram clients that expose the bridge', () => {
	assert.deepEqual(browserGuide(safari + ' Telegram', false, false, 1), { app: 'Telegram', ios: true });
	assert.deepEqual(browserGuide(safari, false, true, 1), { app: 'Telegram', ios: true });
	assert.deepEqual(browserGuide(safari + ' Instagram', false, false, 1), { app: 'Instagram', ios: true });
	assert.deepEqual(browserGuide('Android [FB_IAB/FB4A;FBAV/1.0;]', false, false, 1), { app: 'Facebook', ios: false });
});

test('offers Home Screen guidance in iOS Safari tabs and stays hidden in standalone', () => {
	assert.equal(browserGuide(safari, false, false, 1), 'home-screen');
	assert.equal(browserGuide(safari.replace('(iPhone; CPU iPhone OS 18_6 like Mac OS X)', '(Macintosh; Intel Mac OS X 10_15_7)'), false, false, 5), 'home-screen');
	assert.equal(browserGuide(safari, true, false, 1), null);
	assert.equal(browserGuide(safari.replace('Version/18.6', 'CriOS/145.0'), false, false, 1), null);
	assert.equal(browserGuide('Firefox/145.0', false, false, 0), null);
});
