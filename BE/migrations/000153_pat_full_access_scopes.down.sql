-- Roll back KUA-111's extension of full-access tokens. Tokens that hold the
-- complete pre-KUA-111 full-access set lose the five scopes the migration
-- added; a token's other scopes are untouched.
UPDATE personal_access_tokens
SET scopes = ARRAY(
    SELECT kept
    FROM unnest(scopes) AS kept
    WHERE kept NOT IN (
        'cycle:manage',
        'view:manage',
        'issue:delete',
        'account:write',
        'notifications:write'
    )
)
WHERE scopes @> ARRAY[
    'issues:read',
    'comments:read',
    'projects:read',
    'cycles:read',
    'labels:read',
    'teams:read',
    'members:read',
    'workspaces:read',
    'templates:read',
    'views:read',
    'wiki:read',
    'analytics:read',
    'notifications:read',
    'account:read',
    'assets:read',
    'agent_tasks:read',
    'workflows:read',
    'dev_machine:read',
    'issue:create',
    'issue:update',
    'issue:delete_own',
    'project:manage',
    'label:manage',
    'team:manage',
    'member:invite',
    'workspace:manage',
    'workspace:transfer',
    'wiki:write',
    'dev_machine:create',
    'dev_machine:manage',
    'dev_machine:admin'
]::text[];
