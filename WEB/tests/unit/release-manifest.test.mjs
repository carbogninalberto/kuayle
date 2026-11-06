import assert from 'node:assert/strict';
import test from 'node:test';

import { stableReleases } from '../../src/lib/release-manifest.ts';

test('stable releases are newest-first by semantic version, skip prereleases and invalid tags, and keep only GitHub release links', () => {
	const manifest = [
		{ tag_name: 'v0.2.9', html_url: 'https://github.com/carbogninalberto/kuayle/releases/tag/v0.2.9', published_at: '2026-10-01T00:00:00Z', body: '- **Older thing.**' },
		{ tag_name: 'v0.2.16', html_url: 'https://evil.example/releases/tag/v0.2.16', body: '- **Newest thing (KUA-1).**' },
		{ tag_name: 'v0.3.0-rc.1', prerelease: true },
		{ tag_name: 'latest' },
		{ tag_name: 'v0.2.10' }
	];
	const releases = stableReleases(manifest, 3);
	assert.deepEqual(releases.map((release) => release.version), ['v0.2.16', 'v0.2.10', 'v0.2.9']);
	assert.equal(releases[0].url, 'https://github.com/carbogninalberto/kuayle/releases/tag/v0.2.16');
	assert.equal(releases[0].summary, 'Newest thing');
	assert.equal(releases[2].publishedAt, '2026-10-01T00:00:00Z');
	assert.deepEqual(stableReleases({ releases: [] }), []);
	assert.deepEqual(stableReleases('nonsense'), []);
});
