import assert from 'node:assert/strict';
import test from 'node:test';
import {
    buildImportMapping,
    emptyMappingTargets,
    mappingToTargets,
    reportItemLabel,
    sourceUserKey,
    unmappedList
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

test('users without an email are keyed by their source id, never by an empty key', () => {
    assert.equal(sourceUserKey({ external_id: 'acc-1', email: ' Jane@Example.com ' }), 'jane@example.com');
    assert.equal(sourceUserKey({ external_id: 'acc-2', email: '' }), 'acc-2');
    const targets = emptyMappingTargets();
    targets.userTargets[sourceUserKey({ external_id: 'acc-2', email: '' })] = 'user-2';
    targets.userTargets[sourceUserKey({ external_id: 'acc-3', email: '' })] = '';
    assert.deepEqual(buildImportMapping(targets).users, {
        'acc-2': { user_id: 'user-2' },
        'acc-3': { user_id: null }
    });
});

test('the unmapped list joins source notes and the current mapping gaps once each', () => {
    assert.deepEqual(
        unmappedList({ unmapped_fields: ['custom field: customfield_1', 'status: Done'], mapping_gaps: ['status: Done', 'project: Ops (OPS)'] }),
        ['custom field: customfield_1', 'status: Done', 'project: Ops (OPS)']
    );
    assert.deepEqual(unmappedList({ unmapped_fields: null, mapping_gaps: undefined }), []);
});

test('report rows name the source key so the item can be found in the tracker', () => {
    assert.equal(reportItemLabel({ external_type: 'issue', external_id: '10001', external_key: 'PROJ-1' }), 'PROJ-1');
    assert.equal(reportItemLabel({ external_type: 'comment', external_id: 'c9', external_key: 'PROJ-1' }), 'PROJ-1 · comment c9');
    assert.equal(reportItemLabel({ external_type: 'parent', external_id: '10002', external_key: 'PROJ-2' }), 'PROJ-2 · parent');
    assert.equal(reportItemLabel({ external_type: 'issue', external_id: '10003', external_key: '' }), '10003');
});
