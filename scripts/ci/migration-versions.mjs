// Compare the PR head with the base fetched at checkout, including commits
// that landed on the base after the PR event.
import { execFileSync } from 'node:child_process';

const [base, head] = process.argv.slice(2);
if (!base || !head) throw new Error('Usage: node scripts/ci/migration-versions.mjs <latest-base> <pr-head>');

function migrations(args) {
  const paths = execFileSync('git', [...args, '--', 'BE/migrations'], { encoding: 'utf8' }).split('\0');
  return paths.flatMap((path) => {
    const match = /^BE\/migrations\/((\d+)_[^/]+)\.(up|down)\.sql$/.exec(path);
    return match ? [{ path, stem: match[1], version: BigInt(match[2]) }] : [];
  });
}

const upstream = new Map();
for (const file of migrations(['ls-tree', '-rz', '--name-only', base])) {
  upstream.set(file.version, file);
}
for (const file of migrations(['diff', '--no-renames', '--name-only', '--diff-filter=A', '-z', `${base}...${head}`])) {
  const other = upstream.get(file.version);
  if (other) {
    console.error(`PR migration ${file.path} reuses version ${file.version} from ${other.path} on ${base}. Renumber the PR migration.`);
    process.exitCode = 1;
  }
}

const seen = new Map();
for (const file of migrations(['ls-tree', '-rz', '--name-only', head])) {
  const other = seen.get(file.version);
  if (other && other.stem !== file.stem) {
    console.error(`Duplicate migration version ${file.version} in ${other.path} and ${file.path}. Renumber one pair.`);
    process.exitCode = 1;
  }
  seen.set(file.version, file);
}
