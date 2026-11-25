#!/usr/bin/env node
// kuayle-say writes one message for the room of a pipeline task into the
// worktree's .kuayle/outbox/ (a hidden file, then renamed), so an agent never
// hand-writes the JSON. Kuayle reads it as agents' outbox messages; see
// "The task room" in kuayle-agents.md.
'use strict';

const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const { execFileSync } = require('node:child_process');

const KINDS = ['say', 'ask', 'phase', 'verdict', 'request', 'waiver', 'grant', 'fact', 'summary'];
const VERDICTS = ['approve', 'request_changes', 'comment'];
const REQUESTS = ['review', 'merge', 'publish', 'split', 'approve'];
const MAX_RUNES = 4000;
const USAGE = `usage: kuayle-say --to @role|@all [--ask | --kind KIND] [--reply-to N] [--file PATH] [TEXT...]
       kuayle-say --to @role --verdict approve|request_changes|comment [--commit SHA] [--file FINDINGS]
       kuayle-say --to @role --request review|merge|publish|split [TEXT...]
       kuayle-say --to @all --phase PHASE REASON... (the lead moves the task)
       kuayle-say --to @all --request approve --commit SHA
       kuayle-say --to @all --waive CHECK [--standing] REASON
The text is the remaining arguments, or --file, or standard input with "-".`;

function fail(message) {
  process.stderr.write(`kuayle-say: ${message}\n${USAGE}\n`);
  process.exit(2);
}

function parse(argv) {
  const opts = { kind: 'say', text: [] };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    const value = () => {
      if (i + 1 >= argv.length) fail(`${arg} needs a value`);
      return argv[++i];
    };
    switch (arg) {
      case '--to': opts.to = value(); break;
      case '--ask': opts.kind = 'ask'; break;
      case '--kind': opts.kind = value(); break;
      case '--reply-to': opts.replyTo = value(); break;
      case '--file': opts.file = value(); break;
      case '--verdict': opts.kind = 'verdict'; opts.verdict = value(); break;
      case '--commit': opts.commit = value(); break;
      case '--request': opts.kind = 'request'; opts.action = value(); break;
      case '--waive': opts.kind = 'waiver'; opts.check = value(); break;
      case '--standing': opts.standing = true; break;
      case '--phase': opts.kind = 'phase'; opts.phase = value(); break;
      case '-h': case '--help': process.stdout.write(`${USAGE}\n`); process.exit(0); break;
      case '--': opts.text.push(...argv.slice(i + 1)); i = argv.length; break;
      default:
        if (arg.startsWith('--')) fail(`unknown option ${arg}`);
        opts.text.push(arg);
    }
  }
  return opts;
}

function git(args, cwd) {
  return execFileSync('git', args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim();
}

function outboxDir() {
  if (process.env.KUAYLE_OUTBOX_DIR) return process.env.KUAYLE_OUTBOX_DIR;
  let top;
  try { top = git(['rev-parse', '--show-toplevel']); } catch { fail('run it inside your worktree (no git repository here)'); }
  return path.join(top, '.kuayle', 'outbox');
}

function main() {
  const opts = parse(process.argv.slice(2));
  const to = (opts.to || '').trim().toLowerCase();
  if (!/^@[a-z][a-z0-9_-]{0,31}$/.test(to)) fail('--to is "@" and a role of your task, or "@all"');
  if (!KINDS.includes(opts.kind)) fail(`--kind is one of ${KINDS.join(', ')}`);

  let body = opts.text.join(' ');
  if (opts.file !== undefined) {
    if (body) fail('give the text as arguments or --file, not both');
    try { body = opts.file === '-' ? fs.readFileSync(0, 'utf8') : fs.readFileSync(opts.file, 'utf8'); } catch (err) { fail(`cannot read ${opts.file}: ${err.message}`); }
  } else if (body === '-') {
    body = fs.readFileSync(0, 'utf8');
  }
  body = body.trim();
  if ((opts.kind === 'say' || opts.kind === 'ask') && !body) fail('the message is empty');
  if ([...body].length > MAX_RUNES) fail(`the text is at most ${MAX_RUNES} characters; shorten it or point to a file in the repository`);

  const message = { to, kind: opts.kind, body };
  const data = {};
  if (opts.kind === 'verdict') {
    if (!VERDICTS.includes(opts.verdict)) fail(`--verdict is one of ${VERDICTS.join(', ')}`);
    let commit = opts.commit;
    if (!commit) {
      try { commit = git(['rev-parse', 'HEAD']); } catch { fail('--commit is needed outside a git worktree'); }
    }
    if (!/^[0-9a-f]{7,64}$/.test(commit)) fail('--commit is a commit hash');
    Object.assign(data, { verdict: opts.verdict, commit });
  }
  if (opts.kind === 'request') {
    if (!REQUESTS.includes(opts.action)) fail(`--request is one of ${REQUESTS.join(', ')}`);
    data.action = opts.action;
    if (opts.action === 'approve') {
      if (!/^(?:[0-9a-f]{40}|[0-9a-f]{64})$/.test(opts.commit || '')) fail('approval needs --commit with the full room head');
      data.commit = opts.commit;
    }
  }
  if (opts.standing && opts.kind !== 'waiver') fail('--standing applies only to a waiver');
  if (opts.kind === 'waiver') {
    if (!opts.check || !body) fail('a waiver needs --waive CHECK and a reason');
    data.check = opts.check;
    if (opts.standing) data.standing = true;
  }
  if (opts.kind === 'phase') {
    if (!opts.phase || !opts.phase.trim()) fail('--phase names a phase of the workflow');
    if (!body) fail('say why the task moves: kuayle-say --to @all --phase ID "reason"');
    data.phase = opts.phase.trim();
  }
  if (Object.keys(data).length) message.data = data;
  if (opts.replyTo !== undefined) {
    const n = Number(opts.replyTo);
    if (!Number.isInteger(n) || n <= 0) fail('--reply-to is the number (#) of a room message');
    message.reply_to = n;
  }

  const dir = outboxDir();
  fs.mkdirSync(dir, { recursive: true });
  const name = `room-${Date.now()}-${crypto.randomBytes(4).toString('hex')}.json`;
  const hidden = path.join(dir, `.${name}`);
  fs.writeFileSync(hidden, JSON.stringify(message), { mode: 0o644 });
  fs.renameSync(hidden, path.join(dir, name));
  process.stdout.write(`${path.join(dir, name)}\n`);
}

main();
