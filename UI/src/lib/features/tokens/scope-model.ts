// Scope model for the personal access token create form.
//
// Mirrors the backend public contract (BE internal/domain/personal_access_token.go
// validTokenScopes, PR #55) and the UI interaction model from docs/PAT-SCOPES.md v4 §3.3:
// permissions are presented per resource row with No access / Read / Read & write
// levels, presets batch-set the rows, and what gets submitted is always the
// expanded fine-grained scope list — presets are a pure UI concept.

export type AccessLevel = 'none' | 'read' | 'write';

export interface ResourceRow {
	/** i18n key suffix: settings.tokens.res_{key} */
	key: string;
	readScopes: string[];
	/** Empty for resources without a write permission code (read-only rows). */
	writeScopes: string[];
	/** Rows marked advanced render in the collapsed "Dev machines" section. */
	advanced?: boolean;
	/** i18n key suffix for an inline note shown next to the row. */
	note?: string;
}

export const RESOURCE_ROWS: ResourceRow[] = [
	{ key: 'issues', readScopes: ['issues:read'], writeScopes: ['issue:create', 'issue:update', 'issue:delete_own'] },
	{ key: 'comments', readScopes: ['comments:read'], writeScopes: [], note: 'comments_write_note' },
	{ key: 'projects', readScopes: ['projects:read'], writeScopes: ['project:manage'] },
	{ key: 'cycles', readScopes: ['cycles:read'], writeScopes: [] },
	{ key: 'labels', readScopes: ['labels:read'], writeScopes: ['label:manage'] },
	{ key: 'teams', readScopes: ['teams:read'], writeScopes: ['team:manage'] },
	{ key: 'members', readScopes: ['members:read'], writeScopes: ['member:invite'] },
	{ key: 'workspace', readScopes: ['workspaces:read'], writeScopes: ['workspace:manage'] },
	{ key: 'templates', readScopes: ['templates:read'], writeScopes: [] },
	{ key: 'views', readScopes: ['views:read'], writeScopes: [] },
	{ key: 'analytics', readScopes: ['analytics:read'], writeScopes: [] },
	{ key: 'notifications', readScopes: ['notifications:read'], writeScopes: [] },
	{ key: 'account', readScopes: ['account:read'], writeScopes: [] },
	{ key: 'assets', readScopes: ['assets:read'], writeScopes: [] },
	{
		key: 'dev_machines',
		readScopes: ['dev_machine:read'],
		writeScopes: ['dev_machine:create', 'dev_machine:manage', 'dev_machine:admin'],
		advanced: true
	}
];

export type PresetKey = 'read_only' | 'developer' | 'triage_bot' | 'full_access';

export const PRESET_KEYS: PresetKey[] = ['read_only', 'developer', 'triage_bot', 'full_access'];

const ALL_READ_SCOPES = RESOURCE_ROWS.flatMap((row) => row.readScopes);
const ALL_SCOPES = RESOURCE_ROWS.flatMap((row) => [...row.readScopes, ...row.writeScopes]);

// Preset expansions per docs/PAT-SCOPES.md v4 §3.3. issue:delete_own is
// deliberately not in any non-Full preset — deletion must be opted into
// explicitly by setting the Issues row to Read & write.
export const PRESET_SCOPES: Record<PresetKey, string[]> = {
	read_only: ALL_READ_SCOPES,
	developer: [...ALL_READ_SCOPES, 'issue:create', 'issue:update', 'label:manage'],
	triage_bot: [
		'issues:read',
		'comments:read',
		'labels:read',
		'teams:read',
		'projects:read',
		'cycles:read',
		'issue:create',
		'issue:update'
	],
	full_access: ALL_SCOPES
};

function sortedKey(scopes: Iterable<string>): string {
	return [...scopes].sort().join('\n');
}

/** Derives the displayed access level of a row from a scope set. */
export function levelForScopes(row: ResourceRow, scopes: ReadonlySet<string>): AccessLevel {
	if (row.writeScopes.some((s) => scopes.has(s))) return 'write';
	if (row.readScopes.some((s) => scopes.has(s))) return 'read';
	return 'none';
}

/** Returns a new scope set with the row moved to the given access level. */
export function applyLevel(scopes: ReadonlySet<string>, row: ResourceRow, level: AccessLevel): Set<string> {
	const next = new Set(scopes);
	for (const s of [...row.readScopes, ...row.writeScopes]) next.delete(s);
	if (level === 'read' || level === 'write') {
		for (const s of row.readScopes) next.add(s);
	}
	if (level === 'write') {
		for (const s of row.writeScopes) next.add(s);
	}
	return next;
}

/** Returns the preset whose expansion matches the scope set exactly, or 'custom'. */
export function matchPreset(scopes: ReadonlySet<string>): PresetKey | 'custom' {
	const key = sortedKey(scopes);
	for (const preset of PRESET_KEYS) {
		if (sortedKey(PRESET_SCOPES[preset]) === key) return preset;
	}
	return 'custom';
}
