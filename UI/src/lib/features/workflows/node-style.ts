import type { Component } from 'svelte';
import {
	Bot,
	CircleCheck,
	CircleX,
	GitMerge,
	GitPullRequestArrow,
	ListChecks,
	Play,
	ScanSearch,
	UserCheck
} from '@lucide/svelte';
import type { WorkflowNode, WorkflowNodeType } from '$lib/api/workflows';
import type { Block } from './workflow-graph';
import { nodeTypeLabel } from './labels';
import { m } from '$lib/features/i18n/messages';

// How each step type looks on the canvas and in the editor's lists: one icon
// and one accent per type, so a workflow reads at a glance.

const icons: Record<WorkflowNodeType, Component> = {
	start: Play,
	agent: Bot,
	check: ListChecks,
	approval: UserCheck,
	publish: GitPullRequestArrow,
	merge: GitMerge,
	end: CircleCheck
};

// Tinted tile behind the icon; readable in both themes.
const tones: Record<WorkflowNodeType, string> = {
	start: 'bg-muted text-muted-foreground',
	agent: 'bg-violet-500/12 text-violet-600 dark:text-violet-400',
	check: 'bg-sky-500/12 text-sky-600 dark:text-sky-400',
	approval: 'bg-amber-500/14 text-amber-700 dark:text-amber-400',
	publish: 'bg-emerald-500/12 text-emerald-600 dark:text-emerald-400',
	merge: 'bg-teal-500/12 text-teal-600 dark:text-teal-400',
	end: 'bg-emerald-500/12 text-emerald-600 dark:text-emerald-400'
};

const failedTone = 'bg-destructive/10 text-destructive';

export function nodeIcon(node: Pick<WorkflowNode, 'type' | 'outcome'>): Component {
	return node.type === 'end' && node.outcome === 'failed' ? CircleX : icons[node.type];
}

export function nodeTone(node: Pick<WorkflowNode, 'type' | 'outcome'>): string {
	return node.type === 'end' && node.outcome === 'failed' ? failedTone : tones[node.type];
}

const hints: Partial<Record<WorkflowNodeType, () => string>> = {
	agent: m['workflows.editor.hint.agent'],
	check: m['workflows.editor.hint.check'],
	approval: m['workflows.editor.hint.approval'],
	publish: m['workflows.editor.hint.publish'],
	merge: m['workflows.editor.hint.merge'],
	end: m['workflows.editor.hint.end']
};

/** One line on what a step of this type does, for the add-step menu. */
export const nodeHint = (type: WorkflowNodeType) => hints[type]?.() ?? '';

// The add-step menus' entries: node types, plus the automated review block.
export const blockIcon = (block: Block): Component => (block === 'review' ? ScanSearch : nodeIcon({ type: block }));
export const blockTone = (block: Block): string => (block === 'review' ? tones.agent : nodeTone({ type: block }));
export const blockLabel = (block: Block): string =>
	block === 'review' ? m['workflows.editor.block_review']() : nodeTypeLabel(block);
export const blockHint = (block: Block): string =>
	block === 'review' ? m['workflows.editor.hint.review']() : nodeHint(block);
