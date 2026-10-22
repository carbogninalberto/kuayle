import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, isAbsolute, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

// Deliberately supports the harness's inline Markdown links and plain scalar
// frontmatter. This is a local integrity check, not a Markdown/YAML parser.
export function checkHarness(root) {
	root = resolve(root);
	const errors = [];
	const links = new Map();
	const entrypoints = ['AGENTS.md', 'AGENTS/TOOLS.md', 'AGENTS/SKILLS.md'];
	const skillsDir = resolve(root, 'AGENTS/SKILLS');
	const skills = existsSync(skillsDir)
		? readdirSync(skillsDir, { withFileTypes: true }).filter((entry) => entry.isDirectory()).map((entry) => entry.name)
		: [];
	if (!skills.length) errors.push('AGENTS/SKILLS: no skill directories');
	if (!existsSync(resolve(root, 'AGENTS/TOOLS'))) errors.push('AGENTS/TOOLS: missing');

	function markdownFiles(directory) {
		if (!existsSync(directory)) return [];
		return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
			const path = resolve(directory, entry.name);
			return entry.isDirectory() ? markdownFiles(path) : entry.name.endsWith('.md') ? [relative(root, path)] : [];
		});
	}
	const files = new Set([...entrypoints, ...markdownFiles(resolve(root, 'AGENTS')),
		...skills.map((name) => `AGENTS/SKILLS/${name}/SKILL.md`)]);
	for (const file of files) {
		const path = resolve(root, file);
		if (!existsSync(path)) {
			errors.push(`${file}: missing`);
			continue;
		}
		const source = readFileSync(path, 'utf8');
		const body = source.replace(/^```[^\n]*\n[\s\S]*?^```\s*$/gm, '');
		const targets = new Set();
		links.set(file, targets);
		for (const match of body.matchAll(/\[[^\]\n]*\]\(([^)\n]+)\)/g)) {
			const href = match[1];
			if (/^(?:https?:|mailto:|#)/.test(href)) continue;
			const destination = href.split('#')[0];
			const target = resolve(dirname(path), destination);
			const local = relative(root, target);
			if (isAbsolute(destination) || local === '..' || local.startsWith(`..${sep}`)) {
				errors.push(`${file}: link outside repository: ${href}`);
			} else if (!existsSync(target)) {
				errors.push(`${file}: broken link: ${href}`);
			}
			targets.add(local.split(sep).join('/'));
		}
		if (/^AGENTS\/SKILLS\/[^/]+\/SKILL\.md$/.test(file)) {
			const name = file.split('/')[2];
			const metadata = source.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/)?.[1] ?? '';
			if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(name) || name.length > 64 ||
				metadata.match(/^name: *([^\r\n]+)$/m)?.[1].trim() !== name) {
				errors.push(`${file}: name must match the lowercase hyphenated directory name`);
			}
			if (!metadata.match(/^description: *([^\r\n]+)$/m)?.[1].trim()) {
				errors.push(`${file}: description must be a nonempty single-line scalar`);
			}
		}
	}
	for (const file of entrypoints.slice(1)) {
		if (!links.get('AGENTS.md')?.has(file)) errors.push(`AGENTS.md: must link ${file}`);
	}
	for (const name of skills) {
		const file = `AGENTS/SKILLS/${name}/SKILL.md`;
		if (!links.get('AGENTS/SKILLS.md')?.has(file)) errors.push(`${file}: missing from skill index`);
	}
	return errors;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
	const errors = checkHarness(fileURLToPath(new URL('../../', import.meta.url)));
	if (errors.length) {
		console.error(errors.join('\n'));
		process.exitCode = 1;
	} else {
		console.log('Agent harness: local links, skill metadata and discovery passed.');
	}
}
