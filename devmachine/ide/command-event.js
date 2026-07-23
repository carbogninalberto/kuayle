const endpoint = process.env.KUAYLE_COLLECTOR_URL;
const command = (process.env.KUAYLE_COMMAND || '')
  .replace(/^\s*\d+\s+/, '')
  .replace(/\b([A-Z0-9_]*(?:KEY|TOKEN|SECRET|PASSWORD)[A-Z0-9_]*)=\S+/gi, '$1=[REDACTED]')
  .replace(/(--?(?:api-?key|token|secret|password))\s+\S+/gi, '$1 [REDACTED]');
if (!endpoint || !command) process.exit(0);

fetch(`${endpoint}/event`, {
  method: 'POST',
  headers: { 'content-type': 'application/json' },
  body: JSON.stringify({
    source: 'shell',
    event_type: 'command.finished',
    payload: { command, exit_code: Number(process.env.KUAYLE_EXIT_CODE || 0) }
  })
}).catch(() => {});
