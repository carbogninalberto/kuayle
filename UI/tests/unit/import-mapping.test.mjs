import assert from 'node:assert/strict';
import test from 'node:test';
import {
    buildImportMapping,
    emptyMappingTargets,
    mappingToTargets
} from '../../src/lib/features/imports/import-mapping.ts';

test('an empty selection becomes an explicit placeholder, not a target', () => {
    const targets = emptyMappingTargets();
    targets.projectTeams['10'] = 'team-a';
    targets.statusTargets['team-a:to do'] = '';
    targets.userTargets['nobody@example.com'] = '';
    targets.priorities['high'] = 2;
    const mapping = buildImportMapping(targets);
    assert.deepEqual(mapping.projects, { '10': { team_id: 'team-a', project_id: null } });
    assert.deepEqual(mapping.statuses, {});
    assert.deepEqual(mapping.users, { 'nobody@example.com': { user_id: null } });
    assert.equal(mapping.priorities['high'], 2);
    assert.equal(mapping.epic_mode, 'project');
    assert.equal(mapping.sprint_mode, 'cycle');
});

test('a status needs a mapped team to be valid', () => {
    const targets = emptyMappingTargets();
    targets.statusTargets['team-a:to do'] = 'status-1';
    assert.deepEqual(buildImportMapping(targets).statuses, {});
    targets.projectTeams['10'] = 'team-a';
    assert.deepEqual(buildImportMapping(targets).statuses, {
        'team-a:to do': { team_id: 'team-a', status_id: 'status-1' }
    });
});

test('statuses are keyed per team and stale teams are dropped', () => {
    const targets = emptyMappingTargets();
    targets.projectTeams['10'] = 'team-a';
    targets.projectTeams['20'] = 'team-b';
    targets.statusTargets['team-a:in progress'] = 'status-a';
    targets.statusTargets['team-b:in progress'] = 'status-b';
    targets.statusTargets['team-gone:done'] = 'status-gone';
    const mapping = buildImportMapping(targets);
    assert.deepEqual(mapping.statuses, {
        'team-a:in progress': { team_id: 'team-a', status_id: 'status-a' },
        'team-b:in progress': { team_id: 'team-b', status_id: 'status-b' }
    });
});

test('a saved mapping round-trips through the form targets with two teams', () => {
    const original = buildImportMapping({
        projectTeams: { p: 'team-a', q: 'team-b' },
        statusTargets: { 'team-a:in progress': 'status-a', 'team-b:in progress': 'status-b' },
        userTargets: { 'jane@example.com': 'user-1' },
        priorities: { high: 2 },
        epicMode: 'parent',
        sprintMode: 'ignore'
    });
    const rebuilt = buildImportMapping(mappingToTargets(original));
    assert.deepEqual(rebuilt, original);
});
