// Pure helpers of the "New skill" and "New custom tool" wizards: the request
// sent to the AI writer from the user's answers, the template used when AI is
// off, and the reading of an AI-drafted tool. Every save is still validated by
// the API; nothing here is trusted as valid.
import type { AssistantToolInterpreter } from '$lib/api/assistant-tools';
import { SKILL_DESCRIPTION_MAX, SKILL_NAME_MAX, formatSkill, parseSkill } from '../workflows/skill-md.ts';
import { paramsFromSchema, type ParamDraft } from './tool-schema.ts';

/** The API's limit on an AI writing request, shared out so every answer fits. */
const INSTRUCTION_MAX = 2000;
const MAIN_MAX = 1100;
const SIDE_MAX = 400;

export interface SkillAnswers {
	/** What the skill is for. */
	purpose: string;
	/** When the agent should use it. */
	whenToUse: string;
	/** Optional examples, or a pasted or imported document. */
	examples: string;
}

export interface ToolAnswers {
	/** What the tool does. */
	what: string;
	/** The service or URL it talks to, if any. */
	service: string;
	/** The inputs it takes, in the user's words. */
	inputs: string;
}

const oneLine = (text: string) => text.replace(/\s+/g, ' ').trim();
const clip = (text: string, max: number) =>
	[...text].length > max ? [...text].slice(0, max - 1).join('') + '…' : text;

/** A SKILL.md name from the purpose: its first words as a slug. */
export function skillSlug(purpose: string): string {
	const slug = purpose
		.normalize('NFD')
		.replace(/\p{Diacritic}/gu, '')
		.toLowerCase()
		.replace(/[^a-z0-9]+/g, '-')
		.replace(/^-+|-+$/g, '')
		.split('-')
		.slice(0, 6)
		.join('-')
		.slice(0, SKILL_NAME_MAX)
		.replace(/-+$/, '');
	return slug || 'my-skill';
}

/** The SKILL.md written from the answers when AI is off, ready to edit. */
export function skillTemplate(answers: SkillAnswers): string {
	const purpose = answers.purpose.trim();
	const when = oneLine(answers.whenToUse);
	const title = oneLine(purpose).split(/[.!?\n]/)[0] || 'My skill';
	const sections = [
		`# ${clip(title, 80)}`,
		purpose,
		when ? `## When to use\n\n${answers.whenToUse.trim()}` : '',
		'## Steps\n\n1. ',
		answers.examples.trim() ? `## Examples\n\n${answers.examples.trim()}` : ''
	];
	return formatSkill({
		name: skillSlug(purpose),
		description: clip(when || oneLine(purpose), SKILL_DESCRIPTION_MAX),
		body: sections.filter(Boolean).join('\n\n')
	});
}

/** The AI writer's request for a skill; the template carries the long material. */
export function skillInstruction(answers: SkillAnswers): string {
	const lines = [
		`Purpose: ${clip(oneLine(answers.purpose), MAIN_MAX)}`,
		answers.whenToUse.trim() ? `When to use: ${clip(oneLine(answers.whenToUse), SIDE_MAX)}` : '',
		answers.examples.trim() ? 'Use the examples and reference material in the draft, then replace the draft.' : ''
	];
	return clip(lines.filter(Boolean).join('\n'), INSTRUCTION_MAX);
}

/** An imported Markdown file, if it already is a SKILL.md with front matter. */
export function importedSkill(text: string): string | null {
	return parseSkill(text).problems.includes('front_matter') ? null : text;
}

/** The AI writer's request for a custom tool. */
export function toolInstruction(answers: ToolAnswers): string {
	const lines = [
		`What it does: ${clip(oneLine(answers.what), MAIN_MAX)}`,
		answers.service.trim() ? `Service or URL: ${clip(oneLine(answers.service), SIDE_MAX)}` : '',
		answers.inputs.trim() ? `Inputs: ${clip(oneLine(answers.inputs), SIDE_MAX)}` : ''
	];
	return clip(lines.filter(Boolean).join('\n'), INSTRUCTION_MAX);
}

/** A custom tool name from what it does, when AI is off: snake_case, or empty. */
export function toolNameFrom(what: string): string {
	const words = what
		.normalize('NFD')
		.replace(/\p{Diacritic}/gu, '')
		.toLowerCase()
		.split(/[^a-z0-9]+/)
		.filter(Boolean)
		.slice(0, 5);
	if (!words.length) return '';
	const name = (/^[a-z]/.test(words[0]) ? '' : 'tool_') + words.join('_');
	return name.slice(0, 48).replace(/_+$/, '');
}

/** The editable fields of a drafted tool; the run mode is never drafted. */
export interface ToolDraft {
	name: string;
	description: string;
	interpreter: AssistantToolInterpreter;
	script: string;
	params: ParamDraft[];
	readOnly: boolean;
	timeout: string;
}

const isObject = (value: unknown): value is Record<string, unknown> =>
	typeof value === 'object' && value !== null && !Array.isArray(value);

/**
 * Reads the AI's tool definition (JSON, possibly fenced). Only the tool's own
 * fields are taken, so nothing else the model adds (a credential, a run mode)
 * reaches the form. Returns null when the answer is not a tool definition.
 */
export function parseToolDraft(text: string, interpreters: AssistantToolInterpreter[]): ToolDraft | null {
	const start = text.indexOf('{');
	const end = text.lastIndexOf('}');
	if (start < 0 || end < start) return null;
	let value: unknown;
	try {
		value = JSON.parse(text.slice(start, end + 1));
	} catch {
		return null;
	}
	if (!isObject(value)) return null;
	const { name, description, interpreter, script, parameters, read_only, timeout_seconds } = value;
	if (typeof name !== 'string' || typeof description !== 'string' || typeof script !== 'string') return null;
	const timeout = typeof timeout_seconds === 'number' && Number.isInteger(timeout_seconds) ? timeout_seconds : 600;
	let params: ParamDraft[] = [];
	if (isObject(parameters) && isObject(parameters.properties)) {
		try {
			params = paramsFromSchema(parameters as never).filter((param) =>
				['string', 'number', 'integer', 'boolean', 'enum'].includes(param.type)
			);
		} catch {
			params = [];
		}
	}
	return {
		name: name.trim(),
		description: description.trim(),
		interpreter: interpreters.includes(interpreter as AssistantToolInterpreter)
			? (interpreter as AssistantToolInterpreter)
			: (interpreters[0] ?? 'sh'),
		script,
		params,
		readOnly: read_only === true,
		timeout: String(Math.min(1800, Math.max(10, timeout)))
	};
}
