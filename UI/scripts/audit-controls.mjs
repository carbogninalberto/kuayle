import { readdir, readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { relative, join } from 'node:path';
import { parse } from 'svelte/compiler';

const root = fileURLToPath(new URL('../../', import.meta.url));
const controls = new Set(['button', 'input', 'textarea', 'select', 'option']);
const violations = [];
let files = 0;
let primitiveControls = 0;

async function scan(directory) {
	for (const entry of await readdir(directory, { withFileTypes: true })) {
		const path = join(directory, entry.name);
		if (entry.isDirectory()) await scan(path);
		else if (entry.name.endsWith('.svelte')) {
			files++;
			const source = await readFile(path, 'utf8');
			const ast = parse(source);
			const isPrimitive = path.includes('/components/ui/');
			visit(ast.html, (node) => {
				if (node.type !== 'Element' || !controls.has(node.name)) return;
				if (isPrimitive) primitiveControls++;
				else violations.push(`${relative(root, path)}:${source.slice(0, node.start).split('\n').length}: use a shadcn-svelte composition for <${node.name}>`);
			});
		}
	}
}

function visit(node, callback) {
	if (!node || typeof node !== 'object') return;
	if (Array.isArray(node)) { node.forEach((child) => visit(child, callback)); return; }
	callback(node);
	Object.values(node).forEach((child) => visit(child, callback));
}

await scan(join(root, 'UI/src'));
await scan(join(root, 'WEB/src'));
if (violations.length) {
	console.error(violations.join('\n'));
	process.exitCode = 1;
} else {
	console.log(`Audited ${files} Svelte files: no direct application controls; ${primitiveControls} native elements inside UI primitives.`);
	console.log('Review imperative DOM and rich-text content separately; see docs/kua-62-controls.md for the explicit exceptions.');
}
