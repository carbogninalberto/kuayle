import assert from 'node:assert/strict';
import test from 'node:test';
import {
	githubAppSetupView,
	githubEventLabel,
	githubPermissionLabel
} from '../../src/lib/features/github/app-setup.ts';

const base = {
	managed: false,
	app_slug: 'kuayle-acme',
	account_login: 'acme',
	missing_events: [],
	missing_permissions: [],
	app_settings_url: 'https://github.com/organizations/acme/settings/apps/kuayle-acme/permissions',
	installation_url: 'https://github.com/organizations/acme/settings/installations/77'
};

test('names events and permissions as GitHub shows them', () => {
	assert.equal(githubEventLabel('pull_request_review'), 'Pull request review');
	assert.equal(githubEventLabel('check_suite'), 'Check suite');
	assert.equal(githubEventLabel('some_new_event'), 'some_new_event');
	assert.equal(githubPermissionLabel({ name: 'checks', level: 'read' }), 'Checks: Read-only');
	assert.equal(githubPermissionLabel({ name: 'pull_requests', level: 'write' }), 'Pull requests: Read and write');
});

test('app missing: fix the app, tick each missing item, save and confirm', () => {
	const view = githubAppSetupView({
		...base,
		state: 'app_missing',
		missing_events: ['pull_request_review', 'check_suite'],
		missing_permissions: [{ name: 'checks', level: 'read' }]
	});
	assert.equal(view.tone, 'warning');
	assert.equal(view.message, 'app_missing');
	assert.deepEqual(view.action, { kind: 'fix', url: base.app_settings_url });
	assert.deepEqual(view.checklist, ['Checks: Read-only', 'Pull request review', 'Check suite']);
	assert.deepEqual(view.steps, ['save_and_confirm']);
	assert.equal(view.recheckOnFocus, true);
});

test('a shared app cannot be fixed by the workspace: no link, ask the operator', () => {
	const view = githubAppSetupView({ ...base, managed: true, state: 'app_missing', missing_events: ['check_suite'] });
	assert.equal(view.message, 'app_missing_managed');
	assert.equal(view.action, null);
	assert.deepEqual(view.checklist, ['Check suite']);
	assert.deepEqual(view.steps, []);
});

test('installation pending: accept on the installation page', () => {
	const view = githubAppSetupView({
		...base,
		state: 'installation_pending',
		missing_permissions: [{ name: 'checks', level: 'read' }]
	});
	assert.equal(view.message, 'installation_pending');
	assert.deepEqual(view.action, { kind: 'accept', url: base.installation_url });
	assert.deepEqual(view.steps, ['review_and_accept']);
	assert.equal(view.recheckOnFocus, true);
});

test('suspended and missing installations are explained, ok stops re-checking', () => {
	const suspended = githubAppSetupView({ ...base, state: 'installation_suspended' });
	assert.equal(suspended.tone, 'error');
	assert.deepEqual(suspended.action, { kind: 'open_installation', url: base.installation_url });
	const missing = githubAppSetupView({ ...base, state: 'installation_missing', installation_url: undefined });
	assert.equal(missing.tone, 'error');
	assert.equal(missing.message, 'installation_missing');
	assert.equal(missing.action, null);

	const ok = githubAppSetupView({ ...base, state: 'ok' });
	assert.equal(ok.tone, 'ok');
	assert.equal(ok.action, null);
	assert.equal(ok.recheckOnFocus, false);
	assert.equal(githubAppSetupView({ ...base, state: 'unavailable' }).tone, 'neutral');
});

test('only GitHub pages are offered as links', () => {
	const view = githubAppSetupView({
		...base,
		state: 'app_missing',
		missing_events: ['check_suite'],
		app_settings_url: 'https://github.com.attacker.example/settings/apps/x'
	});
	assert.equal(view.action, null);
	const pending = githubAppSetupView({ ...base, state: 'installation_pending', installation_url: 'javascript:alert(1)' });
	assert.equal(pending.action, null);
});
