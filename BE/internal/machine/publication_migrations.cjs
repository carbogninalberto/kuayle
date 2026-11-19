// Runs against the publisher's isolated bare repository. All edits go into
// a temporary Git index. The original task commit and checked-out files stay
// intact until the task next syncs the published branch.
const { execFileSync } = require('node:child_process');
const [repository, base, head, issue] = process.argv.slice(2);
const directory = 'BE/migrations/';

function git(args, input) {
	return execFileSync('git', [`--git-dir=${repository}`, ...args], {
		input, maxBuffer: 32 * 1024 * 1024, stdio: ['pipe', 'pipe', 'inherit']
	});
}

function records(output) {
	return output.toString('utf8').split('\0').filter(Boolean);
}

function tree(revision, path) {
	const args = ['ls-tree', '-rz', revision];
	if (path) args.push('--', path);
	return new Map(records(git(args)).map((entry) => {
		const tab = entry.indexOf('\t');
		const [mode, , sha] = entry.slice(0, tab).split(' ');
		return [entry.slice(tab + 1), { mode, sha }];
	}));
}

function migrations(files) {
	const pairs = new Map();
	for (const path of files.keys()) {
		const match = /^(\d+)_([^/\r\n]+)\.(up|down)\.sql$/.exec(path.slice(directory.length));
		if (!match) continue;
		const [, prefix, slug, direction] = match;
		const stem = `${prefix}_${slug}`;
		if (!pairs.has(stem)) pairs.set(stem, { stem, slug, version: BigInt(prefix), paths: {} });
		pairs.get(stem).paths[direction] = path;
	}
	return pairs;
}

function renumber() {
	const upstream = migrations(tree(base, directory));
	const headMigrations = tree(head, directory);
	const branch = migrations(headMigrations);
	const own = [...branch.values()].filter(({ stem }) => !upstream.has(stem));
	const conflicts = own.flatMap((pair) => [...upstream.values()]
		.filter(({ version, slug }) => version === pair.version && slug !== pair.slug)
		.map((other) => [pair, other]));
	if (!conflicts.length) return;

	const paths = ([first, second]) => [first, second]
		.map((pair) => Object.values(pair.paths).sort().map((path) => JSON.stringify(path)).join(' and '))
		.join(' conflict with ');
	const block = (reason) => {
		process.stdout.write(`blocked\nMigration files ${paths(conflicts[0])} because they share a version. ${reason} Fetch and merge the latest base, renumber the task's migrations and update their references, then run the checks.`);
	};
	let common;
	try {
		common = git(['merge-base', base, head]).toString().trim();
	} catch (error) {
		if (error.status !== 1) throw error;
		return block('The base and task branch have no common ancestor to establish ownership.');
	}
	const original = tree(common, directory);
	const first = conflicts.reduce((oldest, [pair]) => pair.version < oldest ? pair.version : oldest, conflicts[0][0].version);
	// Move the whole branch-owned suffix so the task's migrations still run
	// in their original order after the base migrations.
	const moving = own.filter(({ version }) => version >= first)
		.sort((a, b) => a.version < b.version ? -1 : Number(a.version > b.version));
	let next = [...upstream.values(), ...branch.values()]
		.reduce((highest, { version }) => version > highest ? version : highest, 0n);
	const names = new Map();
	const renames = new Map();
	for (const [index, pair] of moving.entries()) {
		const { up, down } = pair.paths;
		if (!up || !down || [up, down].some((path) => original.has(path) || !/^100(644|755)$/.test(headMigrations.get(path).mode))) {
			return block(`Kuayle could not prove ${directory}${pair.stem} is a new, regular up/down pair owned by the task branch.`);
		}
		if (index > 0 && moving[index - 1].version === pair.version) {
			return block(`The branch has multiple migrations at version ${pair.version}, so their intended order needs review.`);
		}
		if (!/^[A-Za-z0-9_-]+$/.test(pair.slug) || next >= 18446744073709551615n) {
			return block(`Kuayle could not safely assign a new filename to ${directory}${pair.stem}.`);
		}
		const stem = `${String(++next).padStart(6, '0')}_${pair.slug}`;
		names.set(pair.stem, stem);
		renames.set(up, `${directory}${stem}.up.sql`);
		renames.set(down, `${directory}${stem}.down.sql`);
	}

	// Without the slug, the same version can refer to the task migration or
	// the base migration. Leave that choice to the agent when its changes
	// add or edit a version-only reference in a runner, test or document.
	const versions = moving.map(({ version }) => String(version)).join('|');
	const versionReference = `([.](Migrate|Force)[(]|[Mm]igrations?[[:space:]]+|(goto|force)[[:space:]]+)[[:space:]]*0*(${versions})([^[:alnum:]_]|$)`;
	const numberedReferences = records(git([
		'diff', '--no-ext-diff', '--no-textconv', '--name-only', '-z', '-G', versionReference, common, head
	]));
	if (numberedReferences.length) {
		return block(`The change in ${JSON.stringify(numberedReferences[0])} refers to a migration by version alone.`);
	}

	// Search the complete committed tree for literal stems, including docs,
	// tests and scripts. Apply all replacements together so a newly written
	// name cannot be rewritten again. Match whole names within each blob.
	let references;
	try {
		const patterns = [...names.keys()].flatMap((stem) => ['-e', stem]);
		references = records(git(['grep', '-alzF', '--no-textconv', ...patterns, head, '--']))
			.map((entry) => entry.slice(head.length + 1));
	} catch (error) {
		if (error.status !== 1) throw error;
		references = [];
	}
	const files = tree(head);
	const pattern = new RegExp(
		`(?<![A-Za-z0-9_-])(${[...names.keys()].join('|')})(\\.(?:up|down)\\.sql)?(?![A-Za-z0-9_-]|\\.[A-Za-z0-9_.-])`, 'g'
	);
	for (const path of files.keys()) {
		if (!renames.has(path) && path.match(pattern)) return block(`A tracked path also references an old migration name: ${JSON.stringify(path)}.`);
	}
	const edits = new Map();
	for (const path of references) {
		const entry = files.get(path);
		if (!entry || !/^100(644|755)$/.test(entry.mode) || Number(git(['cat-file', '-s', entry.sha])) > 8 * 1024 * 1024) {
			return block(`A reference in ${JSON.stringify(path)} needs to be updated by the task agent.`);
		}
		const blob = git(['cat-file', 'blob', entry.sha]);
		const text = blob.toString('utf8');
		if (text.includes('\0') || !blob.equals(Buffer.from(text))) {
			return block(`A binary reference in ${JSON.stringify(path)} needs to be updated by the task agent.`);
		}
		const updated = text.replace(pattern, (_, stem, direction = '') => names.get(stem) + direction);
		if (updated !== text) edits.set(path, git(['hash-object', '-w', '--stdin'], updated).toString().trim());
	}

	const index = [];
	for (const path of renames.keys()) index.push(`0 ${'0'.repeat(head.length)}\t${path}\0`);
	for (const path of new Set([...renames.keys(), ...edits.keys()])) {
		const { mode, sha } = files.get(path);
		index.push(`${mode} ${edits.get(path) ?? sha}\t${renames.get(path) ?? path}\0`);
	}
	git(['read-tree', head]);
	git(['update-index', '-z', '--index-info'], index.join(''));
	const updated = git(['write-tree']).toString().trim();
	const message = 'chore(migrations): renumber colliding task migrations';
	process.stdout.write(git(['commit-tree', '--no-gpg-sign', updated, '-p', head, '-m', issue ? `${issue}: ${message}` : message]));
}

renumber();
