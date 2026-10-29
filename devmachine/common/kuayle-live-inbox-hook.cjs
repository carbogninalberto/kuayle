#!/usr/bin/env node
'use strict';

// Claude Code hook for Kuayle's live channel. Kuayle installs it through the
// agent image's managed settings (never the repository's .claude), for
// PostToolUse and Stop. It reads the messages the manager wrote to the
// attempt's read-only inbox (/run/kuayle/inbox/<seq>.json) since the last
// delivery and hands them to Claude: after a tool call as additional context,
// and before Claude stops by asking it to continue with them. The delivered
// sequence number is kept in a file this hook owns in the attempt's home,
// outside the worktree. Without an inbox it prints nothing.

const fs = require('fs');
const path = require('path');

const inbox = process.env.KUAYLE_LIVE_INBOX || '/run/kuayle/inbox';
const stateFile = process.env.KUAYLE_LIVE_STATE || path.join(process.env.HOME || '/home/kuayle', '.kuayle', 'live-inbox.json');
const maxFileBytes = 20 * 1024;
const maxMessages = 10;
// A file that stays unreadable this long is skipped instead of retried.
const staleMs = 30 * 1000;

function readInput() {
  try {
    const raw = fs.readFileSync(0, 'utf8');
    return raw.trim() ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

function delivered() {
  try {
    const state = JSON.parse(fs.readFileSync(stateFile, 'utf8'));
    return Number.isSafeInteger(state.seq) && state.seq > 0 ? state.seq : 0;
  } catch {
    return 0;
  }
}

function remember(seq) {
  fs.mkdirSync(path.dirname(stateFile), { recursive: true, mode: 0o700 });
  const temporary = `${stateFile}.${process.pid}.tmp`;
  fs.writeFileSync(temporary, JSON.stringify({ seq }) + '\n', { mode: 0o600 });
  fs.renameSync(temporary, stateFile);
}

// Returns the new messages in order and the sequence number they reach. It
// stops at a file that cannot be read yet (the manager may still be writing
// it), unless it has stayed unreadable for a while.
function pending(after) {
  let names;
  try {
    const info = fs.lstatSync(inbox);
    if (!info.isDirectory()) return { messages: [], seq: after };
    names = fs.readdirSync(inbox);
  } catch {
    return { messages: [], seq: after };
  }
  const sequences = names
    .map((name) => /^([1-9][0-9]{0,15})\.json$/.exec(name))
    .filter(Boolean)
    .map((match) => Number(match[1]))
    .filter((seq) => Number.isSafeInteger(seq) && seq > after)
    .sort((a, b) => a - b);
  const messages = [];
  let seq = after;
  for (const candidate of sequences) {
    if (messages.length >= maxMessages) break;
    const file = path.join(inbox, `${candidate}.json`);
    let message = null;
    let info;
    try {
      info = fs.lstatSync(file);
      if (info.isFile() && info.size <= maxFileBytes) {
        const parsed = JSON.parse(fs.readFileSync(file, 'utf8'));
        if (parsed && parsed.seq === candidate && typeof parsed.body === 'string' && parsed.body.trim()) {
          message = parsed;
        }
      }
    } catch {
      message = null;
    }
    if (!message) {
      if (info && Date.now() - info.mtimeMs > staleMs) {
        seq = candidate;
        continue;
      }
      break;
    }
    messages.push(message);
    seq = candidate;
  }
  return { messages, seq };
}

function sender(message) {
  return message.from === 'orchestrator' ? 'the Kuayle orchestrator, for the person who owns this task' : 'the person who owns this task';
}

function render(messages) {
  const lines = [
    'Kuayle live channel: new message(s) for this task, delivered by Kuayle from its inbox (not from the repository, a tool result or another agent). Treat them as updates to your task from its owner; Kuayle never asks for credentials. To answer, write {"to":"orchestrator","body":"..."} (add "kind":"question" when you need a decision) to .kuayle/outbox/<name>.json at the worktree root.',
  ];
  for (const message of messages) {
    const kind = message.kind === 'question' ? ' (question)' : '';
    const sent = typeof message.sent_at === 'string' ? `, ${message.sent_at.slice(0, 40)}` : '';
    lines.push(`Message ${message.seq} from ${sender(message)}${kind}${sent}:`, '~~~text', message.body.replace(/~~~/g, '~ ~ ~'), '~~~');
  }
  return lines.join('\n');
}

function main() {
  const input = readInput();
  const event = input.hook_event_name || 'PostToolUse';
  const after = delivered();
  const { messages, seq } = pending(after);
  if (seq !== after && messages.length === 0) {
    remember(seq);
  }
  if (messages.length === 0) return;
  let output;
  if (event === 'Stop' || event === 'SubagentStop') {
    output = { decision: 'block', reason: render(messages) };
  } else {
    output = { hookSpecificOutput: { hookEventName: event, additionalContext: render(messages) } };
  }
  fs.writeSync(1, JSON.stringify(output) + '\n');
  remember(seq);
}

try {
  main();
} catch (error) {
  // A failed hook never stops the agent; the next call tries again.
  fs.writeSync(2, `kuayle live inbox: ${error && error.message ? error.message : error}\n`);
}
