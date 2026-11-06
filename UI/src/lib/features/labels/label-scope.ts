import type { Label } from '../../types/label';

type Scoped = Pick<Label, 'project_id'>;

/** Whether a label can be used on an issue of `projectId` (null or undefined: no project). */
export function labelAppliesTo(label: Scoped, projectId: string | null | undefined): boolean {
	return !label.project_id || label.project_id === projectId;
}

/** The labels usable on every one of `issues`, e.g. for a bulk or menu action. */
export function labelsForIssues<T extends Scoped>(labels: T[], issues: { project_id?: string | null }[]): T[] {
	return labels.filter((label) => issues.every((issue) => labelAppliesTo(label, issue.project_id)));
}
