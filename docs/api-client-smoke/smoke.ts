/**
 * Creates an issue and a comment on a running Kuayle instance with a client
 * generated from that instance's OpenAPI document (generated/kuayle.d.ts,
 * written by `npm run generate`). `npm run typecheck` compiles this file
 * against the generated types, so a path, field or status the document does
 * not describe fails before any request is sent.
 *
 * Environment: KUAYLE_URL (default http://localhost:8080), KUAYLE_TOKEN (a
 * personal access token with workspaces:read, teams:read, issue:create,
 * issues:read and comments:read), KUAYLE_WORKSPACE (workspace slug; default:
 * the first workspace the token sees), KUAYLE_TEAM (team key; default: the
 * first team).
 */
import createClient from 'openapi-fetch';
import type { paths } from './generated/kuayle.d.ts';

const baseUrl = process.env.KUAYLE_URL ?? 'http://localhost:8080';
const token = process.env.KUAYLE_TOKEN;
if (!token) {
	console.error('Set KUAYLE_TOKEN to a personal access token (Settings, API Tokens).');
	process.exit(2);
}

const api = createClient<paths>({ baseUrl, headers: { Authorization: `Bearer ${token}` } });

function must<T>(step: string, result: { data?: T; error?: unknown; response: Response }): T {
	if (result.data === undefined) {
		throw new Error(`${step} failed with ${result.response.status}: ${JSON.stringify(result.error)}`);
	}
	return result.data;
}

const workspaces = must('listWorkspaces', await api.GET('/api/workspaces'));
const slug = process.env.KUAYLE_WORKSPACE ?? workspaces[0]?.slug;
if (!slug) throw new Error('The token sees no workspace.');

const teams = must('listTeams', await api.GET('/api/workspaces/{slug}/teams', { params: { path: { slug } } }));
const team = process.env.KUAYLE_TEAM ? teams.find((item) => item.key === process.env.KUAYLE_TEAM) : teams[0];
if (!team) throw new Error(`No team found in workspace ${slug}. Create one first.`);

const issue = must(
	'createIssue',
	await api.POST('/api/workspaces/{slug}/issues', {
		params: { path: { slug } },
		body: { title: 'Created by the generated API client', team_id: team.id, description: 'Smoke test of the OpenAPI document.' }
	})
);

const comment = must(
	'createIssueComment',
	await api.POST('/api/workspaces/{slug}/issues/{identifier}/comments', {
		params: { path: { slug, identifier: issue.identifier } },
		body: { body: 'Comment from the generated API client' }
	})
);

// Read both back through the API to prove they were stored.
const comments = must(
	'listIssueComments',
	await api.GET('/api/workspaces/{slug}/issues/{identifier}/comments', { params: { path: { slug, identifier: issue.identifier } } })
);
if (!comments.some((item) => item.id === comment.id)) throw new Error('The comment was not stored.');
const page = must(
	'listIssues',
	await api.GET('/api/workspaces/{slug}/issues', { params: { path: { slug }, query: { team: team.id, search: issue.identifier, per_page: 5 } } })
);
if (!page.data.some((item) => item.id === issue.id)) throw new Error('The issue is not in the issue list.');

// A request the document forbids is rejected with the documented error shape.
const invalid = await api.POST('/api/workspaces/{slug}/issues', {
	params: { path: { slug } },
	// @ts-expect-error title is required by CreateIssueRequest
	body: { team_id: team.id }
});
if (invalid.response.status !== 400 || invalid.error?.error.code !== 'VALIDATION_ERROR') {
	throw new Error(`Expected a 400 VALIDATION_ERROR, got ${invalid.response.status}`);
}

console.log(`OK: created ${issue.identifier} and comment ${comment.id} in ${slug} on ${baseUrl}`);
