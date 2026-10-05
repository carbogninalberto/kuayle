import { getContext, setContext } from 'svelte';
import type { Workspace } from '$lib/types/workspace';

const KEY = Symbol('current-workspace');

class CurrentWorkspace {
	// A role is valid only for the user and workspace that were loaded together.
	membership = $state<{ workspace: Workspace; userId: string } | null>(null);
}

export function setCurrentWorkspace() {
	return setContext(KEY, new CurrentWorkspace());
}

export function getCurrentWorkspace() {
	return getContext<CurrentWorkspace>(KEY);
}
