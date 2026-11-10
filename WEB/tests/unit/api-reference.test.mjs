import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

import { buildApiReference, describeConstraints, describeType, schemaAnchor } from '../../src/lib/data/api-reference.ts';

const spec = JSON.parse(readFileSync(new URL('../../src/lib/data/openapi.json', import.meta.url), 'utf8'));
const reference = buildApiReference(spec);
const operations = reference.groups.flatMap((group) => group.operations);
const find = (id) => operations.find((operation) => operation.id === id);

test('the page shows every stable operation of the document, each with a scope', () => {
	const documented = Object.values(spec.paths).flatMap((item) => Object.values(item));
	assert.equal(operations.length, documented.length);
	assert.equal(reference.operationCount, documented.length);
	assert.ok(operations.length > 50);
	for (const operation of operations) {
		assert.ok(operation.scopes.length > 0, `${operation.id} names no scope`);
		assert.ok(!operation.description.includes('Required scope'), `${operation.id} repeats the scope sentence`);
	}
	assert.equal(new Set(operations.map((operation) => operation.id)).size, operations.length);
});

test('creating an issue and commenting on it are documented with their schemas', () => {
	const create = find('createIssue');
	assert.equal(create.method, 'POST');
	assert.equal(create.path, '/api/workspaces/{slug}/issues');
	assert.deepEqual(create.scopes, ['issue:create']);
	assert.deepEqual(create.request, [{ text: 'CreateIssueRequest', ref: 'CreateIssueRequest' }]);
	assert.equal(create.status, '201');
	assert.deepEqual(create.response, [{ text: 'Issue', ref: 'Issue' }]);
	assert.ok(create.errors.includes('400') && create.errors.includes('403'));

	const comment = find('createIssueComment');
	assert.equal(comment.path, '/api/workspaces/{slug}/issues/{identifier}/comments');
	assert.deepEqual(comment.parameters.map((parameter) => parameter.name), ['slug', 'identifier']);

	const request = reference.schemas.find((schema) => schema.name === 'CreateIssueRequest');
	const title = request.fields.find((field) => field.name === 'title');
	assert.equal(title.required, true);
	assert.equal(title.notes, 'length 1 to 500');
	assert.equal(request.fields.find((field) => field.name === 'description').required, false);
});

test('internal routes are not on the page', () => {
	for (const operation of operations) {
		assert.doesNotMatch(operation.path, /dev-machine|agent-|github|analytics|ai-settings/);
	}
	const withInternal = structuredClone(spec);
	withInternal.paths['/api/internal'] = { get: { operationId: 'internalThing', tags: ['Issues'], responses: { 200: {} } } };
	assert.equal(buildApiReference(withInternal).operationCount, reference.operationCount);
});

test('every schema link on the page points at a rendered schema', () => {
	const names = new Set(reference.schemas.map((schema) => schema.name));
	const parts = [
		...operations.flatMap((operation) => [...(operation.request ?? []), ...operation.response, ...operation.parameters.flatMap((parameter) => parameter.type)]),
		...reference.schemas.flatMap((schema) => schema.fields.flatMap((field) => field.type)),
		...reference.webhook.body
	];
	for (const part of parts) {
		if (part.ref) assert.ok(names.has(part.ref), `unknown schema ${part.ref}`);
	}
	assert.equal(schemaAnchor('Issue'), 'schema-Issue');
});

test('the guide covers auth, scopes, pagination, errors, rate limits and webhook signatures', () => {
	assert.deepEqual(
		reference.guide.map((section) => section.id),
		['authentication', 'scopes', 'pagination', 'errors', 'rate-limits', 'webhooks', 'stability']
	);
	assert.equal(reference.webhook.signatureHeader, 'X-Webhook-Signature');
	assert.match(reference.guide.find((section) => section.id === 'webhooks').code.text, /timingSafeEqual/);
});

test('types and constraints read naturally', () => {
	const text = (schema) => describeType(schema).map((part) => part.text).join('');
	assert.equal(text({ type: ['string', 'null'], format: 'uuid' }), 'string (uuid) or null');
	assert.equal(text({ type: 'array', items: { $ref: '#/components/schemas/Label' } }), 'array of Label');
	assert.equal(text({ anyOf: [{ $ref: '#/components/schemas/User' }, { type: 'null' }] }), 'User or null');
	assert.equal(text({}), 'any JSON value');
	assert.equal(describeConstraints({ type: 'string', enum: ['asc', 'desc'] }), 'one of asc, desc');
	assert.equal(describeConstraints({ type: 'integer', minimum: 0, maximum: 4 }), '0 to 4');
	assert.equal(describeConstraints({ type: 'array', minItems: 1, items: { type: 'string', format: 'uuid' } }), 'at least 1 item');
});
