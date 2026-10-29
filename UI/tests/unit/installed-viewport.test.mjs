import assert from 'node:assert/strict';
import test from 'node:test';
import { installedAppViewport, isInstalledApp } from '../../src/lib/features/pwa/installed-viewport.ts';

const media = (matching) => (query) => ({ matches: matching.some((mode) => query.includes(`display-mode: ${mode}`)) });

test('locks zoom on top of the page viewport without dropping its other settings', () => {
	assert.equal(
		installedAppViewport('width=device-width, initial-scale=1'),
		'width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no'
	);
	assert.equal(
		installedAppViewport('width=device-width,initial-scale=1,maximum-scale=5,user-scalable=yes,viewport-fit=cover'),
		'width=device-width, initial-scale=1, viewport-fit=cover, maximum-scale=1, user-scalable=no'
	);
	assert.equal(installedAppViewport(''), 'maximum-scale=1, user-scalable=no');
});

test('treats only installed display modes and iOS home-screen launches as the installed app', () => {
	assert.equal(isInstalledApp({ matchMedia: media(['standalone']) }), true);
	assert.equal(isInstalledApp({ matchMedia: media(['fullscreen']) }), true);
	assert.equal(isInstalledApp({ matchMedia: media(['window-controls-overlay']) }), true);
	assert.equal(isInstalledApp({ matchMedia: media(['browser']) }), false);
	assert.equal(isInstalledApp({ matchMedia: media(['minimal-ui']) }), false);
	assert.equal(isInstalledApp({ matchMedia: media([]), navigator: { standalone: true } }), true);
	assert.equal(isInstalledApp({ matchMedia: media([]), navigator: { standalone: false } }), false);
	assert.equal(isInstalledApp({}), false);
});
