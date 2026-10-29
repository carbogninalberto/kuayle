// The parameter builder of a custom tool. It produces the restricted JSON
// Schema the backend validates (domain.ParseToolSchema) and refuses, locally,
// what the backend would refuse; the server still validates every write.
import type { AssistantToolParameterSchema, AssistantToolParameters } from '$lib/api/assistant-tools';

export type ParamType = 'string' | 'number' | 'integer' | 'boolean' | 'enum';

/** One parameter as edited in the builder; bounds are the text of their inputs. */
export interface ParamDraft {
	name: string;
	type: ParamType;
	description: string;
	required: boolean;
	minLength: string;
	maxLength: string;
	minimum: string;
	maximum: string;
	/** One enum value per line. */
	enumValues: string;
}

export type SchemaIssueCode =
	| 'name'
	| 'duplicate'
	| 'too_many'
	| 'description'
	| 'enum'
	| 'number'
	| 'bounds'
	| 'length'
	| 'required';
export interface SchemaIssue {
	code: SchemaIssueCode;
	param: string;
}

// Limits mirrored from the backend.
export const maxParameters = 20;
const maxStringLength = 8192;
const maxEnumValues = 50;
const maxSafeInteger = 2 ** 53;
const argumentName = /^[a-z][a-z0-9_]{0,39}$/;

export const blankParam = (): ParamDraft => ({
	name: '',
	type: 'string',
	description: '',
	required: false,
	minLength: '',
	maxLength: '',
	minimum: '',
	maximum: '',
	enumValues: ''
});

const enumList = (text: string) =>
	text
		.split('\n')
		.map((value) => value.trim())
		.filter((value) => value !== '');

// A bound left empty is absent; anything else must be a finite number.
function bound(text: string): number | undefined | null {
	const value = text.trim();
	if (value === '') return undefined;
	const parsed = Number(value);
	return Number.isFinite(parsed) ? parsed : null;
}

function property(param: ParamDraft): AssistantToolParameterSchema | SchemaIssueCode {
	const description = param.description.trim();
	if ([...description].length > 500) return 'description';
	const base = description ? { description } : {};
	switch (param.type) {
		case 'enum': {
			const values = enumList(param.enumValues);
			if (
				!values.length ||
				values.length > maxEnumValues ||
				new Set(values).size !== values.length ||
				values.some((value) => [...value].length > 200)
			)
				return 'enum';
			return { type: 'string', ...base, enum: values };
		}
		case 'string': {
			const min = bound(param.minLength);
			const max = bound(param.maxLength);
			const whole = (n: number | undefined | null) =>
				n === undefined || (n !== null && Number.isInteger(n) && n >= 0 && n <= maxStringLength);
			if (!whole(min) || !whole(max) || max === 0 || (min != null && max != null && min > max)) return 'length';
			return {
				type: 'string',
				...base,
				...(min != null ? { minLength: min } : {}),
				...(max != null ? { maxLength: max } : {})
			};
		}
		case 'number':
		case 'integer': {
			const min = bound(param.minimum);
			const max = bound(param.maximum);
			const valid = (n: number | undefined | null) =>
				n === undefined ||
				(n !== null && (param.type === 'number' || (Number.isInteger(n) && Math.abs(n) <= maxSafeInteger)));
			if (!valid(min) || !valid(max)) return 'number';
			if (min != null && max != null && min > max) return 'bounds';
			return {
				type: param.type,
				...base,
				...(min != null ? { minimum: min } : {}),
				...(max != null ? { maximum: max } : {})
			};
		}
		default:
			return { type: 'boolean', ...base };
	}
}

/** The schema for the builder's parameters, or the first problem in them. */
export function buildSchema(
	params: ParamDraft[]
): { schema: AssistantToolParameters; error?: undefined } | { error: SchemaIssue; schema?: undefined } {
	if (params.length > maxParameters) return { error: { code: 'too_many', param: '' } };
	const properties: Record<string, AssistantToolParameterSchema> = {};
	const required: string[] = [];
	for (const param of params) {
		const name = param.name.trim();
		if (!argumentName.test(name)) return { error: { code: 'name', param: name } };
		if (name in properties) return { error: { code: 'duplicate', param: name } };
		const built = property(param);
		if (typeof built === 'string') return { error: { code: built, param: name } };
		properties[name] = built;
		if (param.required) required.push(name);
	}
	return {
		schema: {
			type: 'object',
			properties,
			...(required.length ? { required } : {}),
			additionalProperties: false
		}
	};
}

const text = (value: number | undefined) => (value === undefined ? '' : String(value));

/** The builder's rows for a stored schema, in its property order. */
export function paramsFromSchema(schema: AssistantToolParameters | null | undefined): ParamDraft[] {
	const required = new Set(schema?.required ?? []);
	return Object.entries(schema?.properties ?? {}).map(([name, p]) => ({
		name,
		type: p.enum ? 'enum' : p.type,
		description: p.description ?? '',
		required: required.has(name),
		minLength: text(p.minLength),
		maxLength: text(p.maxLength),
		minimum: text(p.minimum),
		maximum: text(p.maximum),
		enumValues: (p.enum ?? []).join('\n')
	}));
}

export type InputValue = string | number | boolean;

/**
 * Typed inputs for a test run from its form values (text, or a checkbox's
 * boolean). Empty optional fields are left out; text is never interpreted.
 */
export function coerceInputs(
	schema: AssistantToolParameters,
	values: Record<string, string | boolean | undefined>
): { inputs: Record<string, InputValue>; error?: undefined } | { error: SchemaIssue; inputs?: undefined } {
	const required = new Set(schema.required ?? []);
	const inputs: Record<string, InputValue> = {};
	for (const [name, p] of Object.entries(schema.properties)) {
		const value = values[name];
		if (p.type === 'boolean') {
			inputs[name] = value === true;
			continue;
		}
		const raw = typeof value === 'string' ? value : '';
		if (raw === '' || (p.type !== 'string' && raw.trim() === '')) {
			if (required.has(name)) return { error: { code: 'required', param: name } };
			continue;
		}
		if (p.type === 'number' || p.type === 'integer') {
			const n = Number(raw.trim());
			if (!Number.isFinite(n) || (p.type === 'integer' && !Number.isInteger(n)))
				return { error: { code: 'number', param: name } };
			inputs[name] = n;
		} else inputs[name] = raw;
	}
	return { inputs };
}

/** One line naming each parameter and its type; `*` marks required ones. */
export function schemaSummary(schema: AssistantToolParameters | null | undefined) {
	const required = new Set(schema?.required ?? []);
	return Object.entries(schema?.properties ?? {})
		.map(([name, p]) => `${name}: ${p.enum ? p.enum.join(' | ') : p.type}${required.has(name) ? '*' : ''}`)
		.join(', ');
}
