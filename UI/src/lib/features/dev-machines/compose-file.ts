// Mirrors the backend rule: a repository-relative .yml/.yaml path without dot
// segments. Blank means inherit.
export function validComposePath(value: string): boolean {
	const path = value.trim();
	if (!path) return true;
	return (
		path.length <= 255 &&
		/^[A-Za-z0-9._-]+(\/[A-Za-z0-9._-]+)*\.ya?ml$/.test(path) &&
		!path.split('/').some((segment) => segment === '.' || segment === '..')
	);
}
