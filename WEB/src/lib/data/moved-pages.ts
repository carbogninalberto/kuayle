/**
 * Pages that moved to a new URL. kuayle.com is served as a static build, so each old URL
 * keeps a prerendered stub with rel=canonical and an immediate refresh to the new page.
 * Replace with host-level 301 redirects when the hosting platform supports them.
 */
export const MOVED_COMPARE_PAGES: Record<string, { to: string; label: string }> = {
	'kuayle-vs-linear': { to: '/linear-alternative', label: 'Linear alternative' },
	'kuayle-vs-plane': { to: '/plane-alternative', label: 'Plane.so alternative' }
};
