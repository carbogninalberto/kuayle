import assert from 'node:assert/strict';
import test from 'node:test';
import {
	normalizeWorkspaceSlug,
	ssoAccessAction,
	ssoErrorKey,
	ssoLinkWorkspace,
	ssoRequiredSlug,
	ssoSessionScoped,
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
	assert.equal(ssoErrorKey('link_required'), 'login.sso.error_link_required');
	assert.equal(ssoErrorKey('not_member'), 'login.sso.error_not_member');
	assert.equal(ssoErrorKey('identity_taken'), 'login.sso.error_identity_taken');
	assert.equal(ssoErrorKey('SSO_SESSION_SCOPED'), 'login.sso.error_session_scoped');
	assert.equal(ssoErrorKey('unknown'), 'login.sso.error_generic');
	assert.equal(ssoErrorKey(null), 'login.sso.error_generic');
});

test('only link_required resumes the link flow after sign-in', () => {
	assert.equal(ssoLinkWorkspace('link_required', ' Acme '), 'acme');
	assert.equal(ssoLinkWorkspace('link_required', null), null);
	assert.equal(ssoLinkWorkspace('not_member', 'acme'), null);
	assert.equal(ssoLinkWorkspace(null, 'acme'), null);
});

test('an SSO-scoped API error is recognised', () => {
	assert.equal(ssoSessionScoped({ status: 403, error: { code: 'SSO_SESSION_SCOPED' } }), true);
	assert.equal(ssoSessionScoped({ status: 403, error: { code: 'SSO_REQUIRED' } }), false);
	assert.equal(ssoSessionScoped({ status: 401, error: { code: 'SSO_SESSION_SCOPED' } }), false);
	assert.equal(ssoSessionScoped(null), false);
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

test('a workspace denial becomes the notice to show, or no notice at all', () => {
	assert.deepEqual(
		ssoAccessAction({ status: 403, error: { code: 'SSO_REQUIRED', workspace_slug: 'Acme' } }),
		{ kind: 'sso_required', slug: 'acme' }
	);
	assert.deepEqual(ssoAccessAction({ status: 403, error: { code: 'SSO_SESSION_SCOPED' } }), {
		kind: 'session_scoped'
	});
	assert.equal(ssoAccessAction({ status: 403, error: { code: 'FORBIDDEN' } }), null);
	assert.equal(ssoAccessAction({ status: 404, error: { code: 'NOT_FOUND' } }), null);
	assert.equal(ssoAccessAction(null), null);
});
