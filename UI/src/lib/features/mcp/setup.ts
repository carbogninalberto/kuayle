/** Stands in for the personal access token in every snippet; it is never filled in by the app. */
export const TOKEN_PLACEHOLDER = 'kuayle_pat_YOUR_TOKEN';

/** The environment variable Codex reads the token from. */
export const CODEX_TOKEN_VARIABLE = 'KUAYLE_TOKEN';

export interface McpClientSetup {
	id: 'claude-code' | 'claude-desktop' | 'cursor' | 'codex';
	/** Product name; not translated. */
	name: string;
	/** The file the snippet goes into; a snippet without one is a terminal command. */
	file?: string;
	code: string;
}

/** The MCP endpoint of an instance, pinned to one workspace. */
export function mcpServerUrl(origin: string, workspaceSlug: string): string {
	return `${origin.replace(/\/+$/, '')}/mcp?workspace=${encodeURIComponent(workspaceSlug)}`;
}

const json = (value: unknown) => JSON.stringify(value, null, 2);

/** Copy-paste setup for each supported client, for the given endpoint. */
export function mcpClientSetups(url: string): McpClientSetup[] {
	const authorization = `Bearer ${TOKEN_PLACEHOLDER}`;
	// Claude Desktop starts local servers only, so it reaches the endpoint
	// through mcp-remote. The header value sits in env because Claude Desktop
	// does not quote spaces in args; plain http needs an explicit opt-in.
	const bridgeArgs = ['-y', 'mcp-remote', url, '--header', 'Authorization:${AUTH_HEADER}'];
	if (url.startsWith('http://')) bridgeArgs.push('--allow-http');
	return [
		{
			id: 'claude-code',
			name: 'Claude Code',
			code: `claude mcp add --transport http kuayle "${url}" --header "Authorization: ${authorization}"`
		},
		{
			id: 'claude-desktop',
			name: 'Claude Desktop',
			file: 'claude_desktop_config.json',
			code: json({
				mcpServers: { kuayle: { command: 'npx', args: bridgeArgs, env: { AUTH_HEADER: authorization } } }
			})
		},
		{
			id: 'cursor',
			name: 'Cursor',
			file: '~/.cursor/mcp.json',
			code: json({ mcpServers: { kuayle: { url, headers: { Authorization: authorization } } } })
		},
		{
			id: 'codex',
			name: 'Codex',
			file: '~/.codex/config.toml',
			code: `[mcp_servers.kuayle]\nurl = ${JSON.stringify(url)}\nbearer_token_env_var = "${CODEX_TOKEN_VARIABLE}"`
		}
	];
}
