import assert from 'node:assert/strict';
import test from 'node:test';
import { existsSync, readFileSync } from 'node:fs';
import {
	RESOURCE_ROWS,
	PRESET_SCOPES,
	applyLevel,
	levelForScopes,
	matchPreset
} from '../../src/lib/features/tokens/scope-model.ts';

const sorted = (scopes) => [...scopes].sort();

// Compare the actual backend contract when the dependent backend PR is present.
// Standalone UI branches cannot validate a contract they do not contain.
const backendPath = new URL('../../../BE/internal/domain/personal_access_token.go', import.meta.url);
let backendScopes;
if (existsSync(backendPath)) {
	const domain = readFileSync(backendPath, 'utf8');
	const permissions = readFileSync(new URL('../../../BE/internal/domain/permission.go', import.meta.url), 'utf8');
	const constants = new Map(
		[...permissions.matchAll(/(Perm\w+)\s*=\s*"([^"]+)"/g)].map((match) => [match[1], match[2]])
	);
	const contract = domain.match(/var validTokenScopes = map\[string\]bool\{([\s\S]*?)\n\}/)?.[1];
	assert.ok(contract, 'backend validTokenScopes declaration must be readable');
	backendScopes = [...contract.matchAll(/(?:"([^"]+)"|(Perm\w+)):\s*true/g)]
		.map((match) => {
			const scope = match[1] ?? constants.get(match[2]);
			assert.ok(scope, `missing constant ${match[2]}`);
			return scope;
		})
		.sort();
	assert.ok(backendScopes.length > 0, 'backend contract must not be empty');
}

test(
	'resource rows match the actual backend valid scope set',
	{ skip: !backendScopes && 'dependent backend contract is absent' },
	() => {
		const all = RESOURCE_ROWS.flatMap((row) => [...row.readScopes, ...row.writeScopes]);
		assert.deepEqual(sorted(all), backendScopes);
	}
);

test('read_only preset expands to all read scopes including dev_machine:read', () => {
	const expected = RESOURCE_ROWS.flatMap((row) => row.readScopes);
	assert.deepEqual(sorted(PRESET_SCOPES.read_only), sorted(expected));
	assert.equal(PRESET_SCOPES.read_only.length, 15);
});

test('developer preset adds issue create/update and label manage, but not delete', () => {
	const scopes = PRESET_SCOPES.developer;
	for (const s of ['issue:create', 'issue:update', 'label:manage']) {
		assert.ok(scopes.includes(s), `missing ${s}`);
	}
	assert.ok(!scopes.includes('issue:delete_own'));
	assert.ok(!scopes.includes('workspace:manage'));
	assert.ok(!scopes.includes('member:invite'));
});

test('triage bot preset matches the documented minimal set', () => {
	assert.deepEqual(
		sorted(PRESET_SCOPES.triage_bot),
		sorted([
			'issues:read',
			'comments:read',
			'labels:read',
			'teams:read',
			'projects:read',
			'cycles:read',
			'issue:create',
			'issue:update'
		])
	);
});

test('full access includes every selectable scope including workspace transfer', () => {
	assert.deepEqual(
		sorted(PRESET_SCOPES.full_access),
		sorted(RESOURCE_ROWS.flatMap((row) => [...row.readScopes, ...row.writeScopes]))
	);
	assert.ok(PRESET_SCOPES.full_access.includes('workspace:transfer'));
});

test('issue:delete_own is only reachable via full access or explicit Issues write', () => {
	for (const preset of ['read_only', 'developer', 'triage_bot']) {
		assert.ok(!PRESET_SCOPES[preset].includes('issue:delete_own'), preset);
	}
});

test('levelForScopes derives none/read/write from a scope set', () => {
	const issues = RESOURCE_ROWS.find((row) => row.key === 'issues');
	assert.equal(levelForScopes(issues, new Set()), 'none');
	assert.equal(levelForScopes(issues, new Set(['issues:read'])), 'read');
	assert.equal(levelForScopes(issues, new Set(['issues:read', 'issue:create'])), 'write');
	// partial write (developer preset) still displays as write
	assert.equal(levelForScopes(issues, new Set(['issue:update'])), 'write');
});

test('applyLevel moves a row between levels without touching other scopes', () => {
	const issues = RESOURCE_ROWS.find((row) => row.key === 'issues');
	const start = new Set(['labels:read']);

	const read = applyLevel(start, issues, 'read');
	assert.deepEqual(sorted(read), sorted(['labels:read', 'issues:read']));

	const write = applyLevel(read, issues, 'write');
	assert.deepEqual(
		sorted(write),
		sorted(['labels:read', 'issues:read', 'issue:create', 'issue:update', 'issue:delete_own'])
	);

	const none = applyLevel(write, issues, 'none');
	assert.deepEqual(sorted(none), ['labels:read']);
	// does not mutate the input set
	assert.deepEqual(sorted(start), ['labels:read']);
});

test('read-only rows ignore the write level', () => {
	const comments = RESOURCE_ROWS.find((row) => row.key === 'comments');
	const scopes = applyLevel(new Set(), comments, 'write');
	assert.deepEqual(sorted(scopes), ['comments:read']);
});

test('matchPreset detects presets and custom combinations', () => {
	assert.equal(matchPreset(new Set(PRESET_SCOPES.read_only)), 'read_only');
	assert.equal(matchPreset(new Set(PRESET_SCOPES.developer)), 'developer');
	assert.equal(matchPreset(new Set(PRESET_SCOPES.triage_bot)), 'triage_bot');
	assert.equal(matchPreset(new Set(PRESET_SCOPES.full_access)), 'full_access');

	// any manual deviation from a preset becomes custom
	const tweaked = new Set(PRESET_SCOPES.read_only);
	tweaked.delete('assets:read');
	assert.equal(matchPreset(tweaked), 'custom');
	assert.equal(matchPreset(new Set()), 'custom');
});
