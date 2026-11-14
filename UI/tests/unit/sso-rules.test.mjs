import assert from 'node:assert/strict';
import test from 'node:test';
import {
	normalizeWorkspaceSlug,
	ssoErrorKey,
	ssoRequiredSlug,
	ssoStartUrl
} from '../../src/lib/features/sso/sso-rules.ts';

test('workspace slugs are normalised for the start URL', () => {
	assert.equal(normalizeWorkspaceSlug('  Acme/  '), 'acme');
	assert.equal(normalizeWorkspaceSlug('/acme/inbox'), 'acme');
	assert.equal(normalizeWorkspaceSlug('acme'), 'acme');
});

test('the start URL encodes the slug and a relative return path only', () => {
	assert.equal(ssoStartUrl('acme'), '/api/auth/sso/acme/start');
	assert.equal(
		ssoStartUrl(' Acme ', '/acme/inbox?filter=mine'),
		'/api/auth/sso/acme/start?redirect_after=%2Facme%2Finbox%3Ffilter%3Dmine'
	);
	assert.equal(ssoStartUrl('acme', 'https://evil.example'), '/api/auth/sso/acme/start');
	assert.equal(ssoStartUrl('acme', '//evil.example'), '/api/auth/sso/acme/start');
	assert.equal(ssoStartUrl('a/b'), '/api/auth/sso/a/start');
});

test('callback error codes map to translated messages', () => {
	assert.equal(ssoErrorKey('invalid_state'), 'login.sso.error_invalid_state');
	assert.equal(ssoErrorKey('email_unverified'), 'login.sso.error_email_unverified');
	assert.equal(ssoErrorKey('unknown'), 'login.sso.error_generic');
	assert.equal(ssoErrorKey(null), 'login.sso.error_generic');
});

test('an SSO_REQUIRED API error names the workspace to start from', () => {
	assert.equal(
		ssoRequiredSlug({ status: 403, error: { code: 'SSO_REQUIRED', workspace_slug: 'Acme' } }),
		'acme'
	);
	assert.equal(ssoRequiredSlug({ status: 401, error: { code: 'SSO_REQUIRED', workspace_slug: 'acme' } }), null);
	assert.equal(ssoRequiredSlug({ status: 403, error: { code: 'FORBIDDEN' } }), null);
	assert.equal(ssoRequiredSlug(null), null);
});
