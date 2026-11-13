-- KUA-111: tokens created with the previous "Full access" preset must reach
-- the routes that KUA-111 opens to tokens. The preset expands to the scope
-- list below (the UI's RESOURCE_ROWS before this migration); only tokens
-- that hold that complete set are extended. Tokens with any narrower ceiling
-- keep exactly what their creator chose. No wildcard scope is introduced.
--
-- The literal list mirrors UI/src/lib/features/tokens/scope-model.ts before
-- cycle:manage, view:manage, issue:delete, account:write and
-- notifications:write were selectable.
UPDATE personal_access_tokens
SET scopes = scopes || ARRAY(
    SELECT added
    FROM unnest(ARRAY[
        'cycle:manage',
        'view:manage',
        'issue:delete',
        'account:write',
        'notifications:write'
    ]) AS added
    WHERE NOT added = ANY(scopes)
)
WHERE revoked_at IS NULL
  AND scopes @> ARRAY[
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
