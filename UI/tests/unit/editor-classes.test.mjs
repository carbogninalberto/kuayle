import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import {
	EDITOR_CLASS_NAMES,
	EDITOR_CLASS_PREFIXES,
	EDITOR_LINK_CLASS,
	filterEditorClasses
} from '../../src/lib/security/editor-classes.ts';
import { Attachment } from '../../src/lib/components/shared/attachment.extension.ts';
import { MentionNode } from '../../src/lib/components/shared/mention/mention.extension.ts';
import { linkIssueKeys } from '../../src/lib/features/wiki/wiki-content.ts';

// The Go sanitizer's tests read the same file, so neither allowlist can change alone.
const contract = JSON.parse(
	readFileSync(new URL('../../../BE/pkg/sanitize/testdata/editor_html_contract.json', import.meta.url), 'utf8')
);

const classAttributes = (html) => [...html.matchAll(/\sclass="([^"]*)"/g)].map((match) => match[1]);

test('the class allowlist is the one the backend sanitizer uses', () => {
	assert.deepEqual([...EDITOR_CLASS_NAMES].sort(), [...contract.allowed_classes].sort());
	assert.deepEqual([...EDITOR_CLASS_PREFIXES], contract.allowed_class_prefixes);
});

test('class values are filtered exactly as the shared contract says', () => {
	assert.ok(contract.class_cases.length > 0);
	for (const { input, output } of contract.class_cases) {
		assert.equal(filterEditorClasses(input), output, JSON.stringify(input));
	}
});

test('stored editor documents keep every class, and injected ones lose theirs', () => {
	for (const { name, html } of contract.documents) {
		for (const value of classAttributes(html)) assert.equal(filterEditorClasses(value), value, name);
	}
	for (const { name, html, want } of contract.stripped) {
		const kept = classAttributes(html).map(filterEditorClasses).filter(Boolean);
		assert.deepEqual(kept, classAttributes(want), name);
	}
});

test('everything the editor and renderers emit passes the filter', () => {
	const emitted = [
		EDITOR_LINK_CLASS,
		Attachment.config.renderHTML.call({}, { HTMLAttributes: { href: '/api/workspaces/acme/assets/1', filename: 'a.pdf', size: 1 } })[1]
			.class,
		MentionNode.config.renderHTML.call({}, { node: { attrs: { kind: 'user', label: 'Ada' } }, HTMLAttributes: {} })[1].class,
		MentionNode.config.renderHTML.call({}, { node: { attrs: { kind: 'issue', label: 'ENG-1' } }, HTMLAttributes: {} })[1].class,
		...classAttributes(linkIssueKeys('Opened PR #12 for ENG-1')),
		'language-typescript'
	];
	assert.equal(emitted.length, 7);
	for (const value of emitted) {
		assert.ok(value, 'a class was expected');
		assert.equal(filterEditorClasses(value), value);
	}
});
