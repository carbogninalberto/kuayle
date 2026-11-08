// SKILL.md, the Agent Skills format coding agents load skills from: YAML
// front matter with a name and a description, then the Markdown body.
//
//   ---
//   name: write-migrations
//   description: How to write and test a database migration in this repo.
//   ---
//
//   # Writing migrations
//   ...
//
// Kuayle stores the three parts in the workspace skill library and writes the
// file back when a coding agent runs. The API remains the authority on limits.

export const SKILL_NAME_MAX = 64;
export const SKILL_DESCRIPTION_MAX = 500;
export const SKILL_BODY_MAX_BYTES = 64 * 1024;
const NAME = /^[a-z0-9]+(-[a-z0-9]+)*$/;

export type SkillProblem = 'front_matter' | 'name' | 'description' | 'body';

export interface ParsedSkill {
	name: string;
	description: string;
	body: string;
	problems: SkillProblem[];
}

export const SKILL_TEMPLATE = `---
name: my-skill
description: What this skill does and when the agent should use it.
---

# My skill

Steps, conventions and examples the agent should follow.
`;

function unquote(value: string): string {
	const v = value.trim();
	if (v.length >= 2 && ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'")))) {
		return v.slice(1, -1);
	}
	return v;
}

/** Splits a SKILL.md into its name, description and body, with what is wrong. */
export function parseSkill(source: string): ParsedSkill {
	const text = source.replace(/^\uFEFF/, '').replace(/\r\n/g, '\n');
	const match = /^---\n([\s\S]*?)\n---[ \t]*(?:\n|$)/.exec(text);
	if (!match)
		return { name: '', description: '', body: text.trim(), problems: ['front_matter', 'name', 'description'] };
	const fields: Record<string, string> = {};
	for (const line of match[1].split('\n')) {
		const field = /^([A-Za-z_][\w-]*):(.*)$/.exec(line);
		if (field) fields[field[1].toLowerCase()] = unquote(field[2]);
	}
	const name = fields.name ?? '';
	const description = (fields.description ?? '').replace(/\s+/g, ' ').trim();
	const body = text.slice(match[0].length).trim();
	const problems: SkillProblem[] = [];
	if (!NAME.test(name) || name.length > SKILL_NAME_MAX) problems.push('name');
	if (!description || description.length > SKILL_DESCRIPTION_MAX) problems.push('description');
	if (!body || new TextEncoder().encode(body).length > SKILL_BODY_MAX_BYTES) problems.push('body');
	return { name, description, body, problems };
}

/** The SKILL.md for a stored skill. */
export function formatSkill(skill: { name: string; description: string; body: string }): string {
	const quote = (v: string) => (/^[\w .,()/-]*$/.test(v) ? v : JSON.stringify(v));
	return `---\nname: ${quote(skill.name)}\ndescription: ${quote(skill.description)}\n---\n\n${skill.body.trim()}\n`;
}
