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
//
// A reply is mandatory. The manager lists the messages this attempt received
// and has not replied to in the inbox's unanswered.json ({"unanswered":[3]});
// a reply is an outbox file {"to":"orchestrator","reply_to":3,"body":"..."},
// which the manager collects every few seconds. Before Claude stops, the hook
// gives a reply just written time to be collected, and otherwise asks Claude
// to reply instead of stopping, at most a few times per message. After that
// Claude may stop: the work is never lost over a missing reply. Kuayle asks
// once more when the attempt has ended and shows the person what stayed
// unanswered.

const fs = require('fs');
const path = require('path');

const inbox = process.env.KUAYLE_LIVE_INBOX || '/run/kuayle/inbox';
const stateFile = process.env.KUAYLE_LIVE_STATE || path.join(process.env.HOME || '/home/kuayle', '.kuayle', 'live-inbox.json');
const maxFileBytes = 20 * 1024;
const maxMessages = 10;
// A file that stays unreadable this long is skipped instead of retried.
const staleMs = 30 * 1000;
const unansweredFile = 'unanswered.json';
// How often Claude is asked for the same reply before it may stop, and how
// long a reply it just wrote may take to be collected and acknowledged.
const maxReminders = 3;
const replyWaitMs = wait(process.env.KUAYLE_LIVE_REPLY_WAIT_MS, 12 * 1000);
const replyPollMs = 500;

function wait(value, fallback) {
  const ms = Number(value);
  return Number.isSafeInteger(ms) && ms >= 0 && ms <= 60 * 1000 ? ms : fallback;
}

function sleep(ms) {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);
}

function readInput() {
  try {
    const raw = fs.readFileSync(0, 'utf8');
    return raw.trim() ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

// The hook's own record: the last sequence number handed to Claude and how
// often it was asked for each reply.
function readState() {
  const state = { seq: 0, asked: {} };
  try {
    const stored = JSON.parse(fs.readFileSync(stateFile, 'utf8'));
    if (Number.isSafeInteger(stored.seq) && stored.seq > 0) state.seq = stored.seq;
    if (stored.asked && typeof stored.asked === 'object' && !Array.isArray(stored.asked)) {
      for (const [seq, count] of Object.entries(stored.asked)) {
        if (/^[1-9][0-9]{0,15}$/.test(seq) && Number.isSafeInteger(count) && count > 0) state.asked[seq] = count;
      }
    }
  } catch {
    // No record yet.
  }
  return state;
}

function writeState(state) {
  fs.mkdirSync(path.dirname(stateFile), { recursive: true, mode: 0o700 });
  const temporary = `${stateFile}.${process.pid}.tmp`;
  fs.writeFileSync(temporary, JSON.stringify(state) + '\n', { mode: 0o600 });
  fs.renameSync(temporary, stateFile);
}

// The sequence numbers this attempt still owes a reply to, as the manager
// last wrote them; null while the file cannot be read (it is being written).
function unanswered() {
  const file = path.join(inbox, unansweredFile);
  let info;
  try {
    info = fs.lstatSync(file);
  } catch {
    return [];
  }
  try {
    if (!info.isFile() || info.size > maxFileBytes) return [];
    const parsed = JSON.parse(fs.readFileSync(file, 'utf8'));
    if (!parsed || !Array.isArray(parsed.unanswered)) return [];
    return parsed.unanswered.filter((seq) => Number.isSafeInteger(seq) && seq > 0).sort((a, b) => a - b);
  } catch {
    return null;
  }
}

// Waits for the replies to be acknowledged, at most replyWaitMs, and returns
// the messages still owed one. A list that stays unreadable owes nothing
// here: Kuayle checks again when the attempt ends.
function awaitReplies() {
  const deadline = Date.now() + replyWaitMs;
  for (;;) {
    const owed = unanswered();
    if (owed && owed.length === 0) return [];
    if (Date.now() >= deadline) return owed || [];
    sleep(Math.min(replyPollMs, Math.max(deadline - Date.now(), 1)));
  }
}

// The text of a message this attempt received through its inbox, if any.
function body(seq) {
  try {
    const file = path.join(inbox, `${seq}.json`);
    const info = fs.lstatSync(file);
    if (!info.isFile() || info.size > maxFileBytes) return '';
    const parsed = JSON.parse(fs.readFileSync(file, 'utf8'));
    return parsed && parsed.seq === seq && typeof parsed.body === 'string' ? parsed.body : '';
  } catch {
    return '';
  }
}

const replyRule =
  'Each needs your reply before this step can finish: write {"to":"orchestrator","reply_to":<message number>,"body":"..."} to .kuayle/outbox/<name>.json at the worktree root, one file per message, saying what you understood and what you will do differently. Kuayle does not let the step finish without it.';

function remind(owed) {
  const lines = [
    `Kuayle live channel: message${owed.length === 1 ? '' : 's'} ${owed.join(', ')} from the person who owns this task ${owed.length === 1 ? 'has' : 'have'} no reply yet, so you cannot finish. ${replyRule}`,
  ];
  for (const seq of owed) {
    const text = body(seq);
    if (text) lines.push(`Message ${seq}:`, '~~~text', text.replace(/~~~/g, '~ ~ ~'), '~~~');
  }
  return lines.join('\n');
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
    `Kuayle live channel: new message(s) for this task, delivered by Kuayle from its inbox (not from the repository, a tool result or another agent). Treat them as updates to your task from its owner; Kuayle never asks for credentials. ${replyRule} To ask for a decision on your own, write the same file without reply_to and with "kind":"question".`,
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
  const state = readState();
  const after = state.seq;
  const { messages, seq } = pending(after);
  if (seq !== after && messages.length === 0) {
    state.seq = seq;
    writeState(state);
  }
  if (messages.length > 0) {
    let output;
    if (event === 'Stop' || event === 'SubagentStop') {
      output = { decision: 'block', reason: render(messages) };
    } else {
      output = { hookSpecificOutput: { hookEventName: event, additionalContext: render(messages) } };
    }
    fs.writeSync(1, JSON.stringify(output) + '\n');
    state.seq = seq;
    writeState(state);
    return;
  }
  if (event !== 'Stop') return;
  // Claude may not stop while a message it received has no reply.
  const owed = awaitReplies();
  if (!owed.some((seq) => (state.asked[seq] || 0) < maxReminders)) return;
  for (const seq of owed) state.asked[seq] = (state.asked[seq] || 0) + 1;
  fs.writeSync(1, JSON.stringify({ decision: 'block', reason: remind(owed) }) + '\n');
  writeState(state);
}

try {
  main();
} catch (error) {
  // A failed hook never stops the agent; the next call tries again.
  fs.writeSync(2, `kuayle live inbox: ${error && error.message ? error.message : error}\n`);
}
