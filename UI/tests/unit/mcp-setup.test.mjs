import assert from 'node:assert/strict';
import test from 'node:test';
import { TOKEN_PLACEHOLDER, mcpClientSetups, mcpServerUrl } from '../../src/lib/features/mcp/setup.ts';

const url = mcpServerUrl('https://kuayle.example.com/', 'acme team');
const byId = Object.fromEntries(mcpClientSetups(url).map((setup) => [setup.id, setup]));

test('the server URL is the instance /mcp endpoint pinned to the workspace', () => {
	assert.equal(url, 'https://kuayle.example.com/mcp?workspace=acme%20team');
});

test('every supported client has a snippet with the URL and the token placeholder', () => {
	assert.deepEqual(Object.keys(byId), ['claude-code', 'claude-desktop', 'cursor', 'codex']);
	for (const setup of Object.values(byId)) assert.ok(setup.code.includes(url), setup.id);
	for (const id of ['claude-code', 'claude-desktop', 'cursor']) assert.ok(byId[id].code.includes(TOKEN_PLACEHOLDER), id);
});

test('Claude Code gets a single shell command with a quoted URL and bearer header', () => {
	assert.equal(byId['claude-code'].file, undefined);
	assert.equal(
		byId['claude-code'].code,
		`claude mcp add --transport http kuayle "${url}" --header "Authorization: Bearer ${TOKEN_PLACEHOLDER}"`
	);
});

test('file snippets are valid configuration for their client', () => {
	const cursor = JSON.parse(byId.cursor.code).mcpServers.kuayle;
	assert.deepEqual(cursor, { url, headers: { Authorization: `Bearer ${TOKEN_PLACEHOLDER}` } });

	const desktop = JSON.parse(byId['claude-desktop'].code).mcpServers.kuayle;
	assert.equal(desktop.command, 'npx');
	assert.deepEqual(desktop.args, ['-y', 'mcp-remote', url, '--header', 'Authorization:${AUTH_HEADER}']);
	assert.deepEqual(desktop.env, { AUTH_HEADER: `Bearer ${TOKEN_PLACEHOLDER}` });

	// Codex reads the token from the environment, so the file holds no secret.
	assert.equal(
		byId.codex.code,
		`[mcp_servers.kuayle]\nurl = "${url}"\nbearer_token_env_var = "KUAYLE_TOKEN"`
	);
});

test('a plain-http instance opts the Claude Desktop bridge into http', () => {
	const local = mcpClientSetups(mcpServerUrl('http://localhost:5173', 'acme'));
	const args = JSON.parse(local.find((setup) => setup.id === 'claude-desktop').code).mcpServers.kuayle.args;
	assert.equal(args.at(-1), '--allow-http');
});
