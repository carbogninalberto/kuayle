/** True for `/<workspace>/settings` and every page below it. */
export function isWorkspaceSettingsPath(pathname: string): boolean {
	return /^\/[^/]+\/settings(?:\/|$)/.test(pathname);
}
