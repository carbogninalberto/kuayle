import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	blankParam,
	buildSchema,
	paramsFromSchema,
	coerceInputs,
	schemaSummary
} from '../../src/lib/features/assistant/tool-schema.ts';
import { diffLines } from '../../src/lib/features/assistant/skill-diff.ts';

const param = (fields) => ({ ...blankParam(), ...fields });

test('builds the restricted JSON Schema the backend validates', () => {
	const result = buildSchema([
		param({ name: 'spec', type: 'string', description: 'Spec file', required: true, maxLength: '200' }),
		param({ name: 'retries', type: 'integer', minimum: '0', maximum: '3' }),
		param({ name: 'ratio', type: 'number', minimum: '0.5' }),
		param({ name: 'headed', type: 'boolean' }),
		param({ name: 'browser', type: 'enum', enumValues: 'chromium\nfirefox\n\n webkit ', required: true })
	]);
	assert.deepEqual(result, {
		schema: {
			type: 'object',
			properties: {
				spec: { type: 'string', description: 'Spec file', maxLength: 200 },
				retries: { type: 'integer', minimum: 0, maximum: 3 },
				ratio: { type: 'number', minimum: 0.5 },
				headed: { type: 'boolean' },
				browser: { type: 'string', enum: ['chromium', 'firefox', 'webkit'] }
			},
			required: ['spec', 'browser'],
			additionalProperties: false
		}
	});
	// Keywords that do not apply to a type are never sent.
	assert.deepEqual(buildSchema([param({ name: 'flag', type: 'boolean', maxLength: '9', minimum: '1' })]).schema, {
		type: 'object',
		properties: { flag: { type: 'boolean' } },
		additionalProperties: false
	});
	assert.deepEqual(buildSchema([]).schema, { type: 'object', properties: {}, additionalProperties: false });
});

test('refuses parameters the backend would refuse, naming the parameter', () => {
	assert.deepEqual(buildSchema([param({ name: 'Spec' })]).error, { code: 'name', param: 'Spec' });
	assert.deepEqual(buildSchema([param({ name: '' })]).error, { code: 'name', param: '' });
	assert.deepEqual(buildSchema([param({ name: 'a' }), param({ name: 'a' })]).error, { code: 'duplicate', param: 'a' });
	assert.deepEqual(
		buildSchema(Array.from({ length: 21 }, (_, i) => param({ name: `p${i}` }))).error,
		{ code: 'too_many', param: '' }
	);
	assert.deepEqual(buildSchema([param({ name: 'e', type: 'enum', enumValues: '' })]).error, { code: 'enum', param: 'e' });
	assert.deepEqual(buildSchema([param({ name: 'e', type: 'enum', enumValues: 'a\na' })]).error, { code: 'enum', param: 'e' });
	assert.deepEqual(buildSchema([param({ name: 'n', type: 'integer', minimum: '1.5' })]).error, { code: 'number', param: 'n' });
	assert.deepEqual(buildSchema([param({ name: 'n', type: 'number', maximum: 'x' })]).error, { code: 'number', param: 'n' });
	assert.deepEqual(buildSchema([param({ name: 'n', type: 'number', minimum: '5', maximum: '1' })]).error, {
		code: 'bounds',
		param: 'n'
	});
	assert.deepEqual(buildSchema([param({ name: 's', maxLength: '9000' })]).error, { code: 'length', param: 's' });
	assert.deepEqual(buildSchema([param({ name: 's', minLength: '5', maxLength: '2' })]).error, { code: 'length', param: 's' });
	assert.deepEqual(buildSchema([param({ name: 's', description: 'x'.repeat(501) })]).error, {
		code: 'description',
		param: 's'
	});
});

test('round-trips a stored schema through the builder', () => {
	const schema = {
		type: 'object',
		properties: {
			browser: { type: 'string', enum: ['chromium', 'webkit'] },
			retries: { type: 'integer', minimum: 0 },
			spec: { type: 'string', description: 'Spec', maxLength: 1024 }
		},
		required: ['spec'],
		additionalProperties: false
	};
	const params = paramsFromSchema(schema);
	assert.deepEqual(
		params.map((p) => [p.name, p.type, p.required]),
		[
			['browser', 'enum', false],
			['retries', 'integer', false],
			['spec', 'string', true]
		]
	);
	assert.equal(params[0].enumValues, 'chromium\nwebkit');
	assert.deepEqual(buildSchema(params).schema, schema);
});

test('turns a test-run form into typed inputs, keeping hostile text as plain strings', () => {
	const schema = buildSchema([
		param({ name: 'spec', required: true }),
		param({ name: 'retries', type: 'integer' }),
		param({ name: 'headed', type: 'boolean' }),
		param({ name: 'browser', type: 'enum', enumValues: 'chromium\nwebkit' })
	]).schema;
	assert.deepEqual(coerceInputs(schema, { spec: '; rm -rf /', retries: '2', headed: true, browser: '' }), {
		inputs: { spec: '; rm -rf /', retries: 2, headed: true }
	});
	assert.deepEqual(coerceInputs(schema, { spec: '', headed: false }).error, { code: 'required', param: 'spec' });
	assert.deepEqual(coerceInputs(schema, { spec: 'a', retries: '1.5' }).error, { code: 'number', param: 'retries' });
	assert.deepEqual(coerceInputs(schema, { spec: 'a', headed: false }), { inputs: { spec: 'a', headed: false } });
});

test('summarizes a schema in one line', () => {
	const schema = buildSchema([
		param({ name: 'spec', required: true }),
		param({ name: 'browser', type: 'enum', enumValues: 'chromium\nwebkit' })
	]).schema;
	assert.equal(schemaSummary(schema), 'spec: string*, browser: chromium | webkit');
	assert.equal(schemaSummary({ type: 'object', properties: {} }), '');
});

test('diffs two scripts line by line', () => {
	assert.deepEqual(diffLines('a\nb\nc', 'a\nx\nc\nd'), [
		{ kind: 'same', text: 'a' },
		{ kind: 'del', text: 'b' },
		{ kind: 'add', text: 'x' },
		{ kind: 'same', text: 'c' },
		{ kind: 'add', text: 'd' }
	]);
	assert.deepEqual(diffLines('', 'a'), [{ kind: 'add', text: 'a' }]);
});
