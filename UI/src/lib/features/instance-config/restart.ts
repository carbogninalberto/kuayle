import type {
	InstanceConfigVariable,
	InstanceRestartIssue,
	InstanceRestartProposal,
	InstanceRestartRun
} from '$lib/api/instance-config';

// Draft changes to restart-only settings, kept on the page until the sysadmin
// applies them. A string is the proposed value; null returns the variable to
// its .env value. The server validates every value again.

export type Pending = Record<string, string | null>;

/** How often the page asks whether an apply has been decided, in milliseconds. */
export const RESTART_POLL_MS = 2000;

/** The value the input shows: the draft, or what is in effect. */
export function draftValue(variable: InstanceConfigVariable, pending: Pending): string {
	const draft = pending[variable.name];
	if (draft === null) return variable.base_value ?? '';
	return draft ?? variable.value ?? '';
}

/** Records the proposed value, or forgets it when it is the value in effect. */
export function withDraft(pending: Pending, variable: InstanceConfigVariable, raw: string): Pending {
	const next = { ...pending };
	if (raw.trim() === (variable.value ?? '')) delete next[variable.name];
	else next[variable.name] = raw;
	return next;
}

/** Returns the variable to its .env value: a change to apply while an override is in effect, nothing otherwise. */
export function withRevert(pending: Pending, variable: InstanceConfigVariable): Pending {
	const next = { ...pending };
	if (variable.source === 'override') next[variable.name] = null;
	else delete next[variable.name];
	return next;
}

export function withoutDraft(pending: Pending, name: string): Pending {
	const next = { ...pending };
	delete next[name];
	return next;
}

export function isReverting(pending: Pending, name: string): boolean {
	return pending[name] === null;
}

export function pendingNames(pending: Pending): string[] {
	return Object.keys(pending).sort();
}

/** The request body of the dry run and the apply: strings are trimmed. */
export function proposalOf(pending: Pending): InstanceRestartProposal {
	return Object.fromEntries(
		Object.entries(pending).map(([name, value]) => [name, value === null ? null : value.trim()])
	);
}

/** An apply the API has not decided yet: it is restarting or has not reported healthy. */
export function runInProgress(run?: InstanceRestartRun): boolean {
	return run?.status === 'restarting' || run?.status === 'applying';
}

/** Per-variable messages of a dry run or a refused apply; the whole-configuration ones have no name. */
export function issuesByName(issues: InstanceRestartIssue[]): { byName: Record<string, string>; general: string[] } {
	const byName: Record<string, string> = {};
	const general: string[] = [];
	for (const issue of issues) {
		if (issue.name) byName[issue.name] = issue.message;
		else general.push(issue.message);
	}
	return { byName, general };
}

/** The issues of a refused request: the API lists them as error details. */
export function issuesOfError(err: unknown): InstanceRestartIssue[] {
	const details = (err as { error?: { details?: { field?: string; message?: string }[] } } | null)?.error?.details;
	if (!Array.isArray(details)) return [];
	return details
		.filter((detail) => typeof detail?.message === 'string')
		.map((detail) => ({ name: detail.field || undefined, message: detail.message as string }));
}
