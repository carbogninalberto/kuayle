// Pure helpers for the SSO sign-in and settings pages. Keep them free of
// Svelte and API imports so tests/unit can load them directly.

export function normalizeWorkspaceSlug(raw: string): string {
	return raw
		.trim()
		.replace(/^\/+|\/+$/g, '')
		.split('/')[0]
		.toLowerCase();
}

export function ssoStartUrl(slug: string, redirectAfter?: string): string {
	const normalized = normalizeWorkspaceSlug(slug);
	const base = `/api/auth/sso/${encodeURIComponent(normalized)}/start`;
	const target = redirectAfter?.trim();
	if (!target || !target.startsWith('/') || target.startsWith('//')) return base;
	return `${base}?redirect_after=${encodeURIComponent(target)}`;
}

export type SSOErrorKey =
	| 'login.sso.error_not_configured'
	| 'login.sso.error_invalid_state'
	| 'login.sso.error_email_unverified'
	| 'login.sso.error_domain_not_allowed'
	| 'login.sso.error_token_invalid'
	| 'login.sso.error_provider'
	| 'login.sso.error_generic';

const ERROR_KEYS: Record<string, SSOErrorKey> = {
	not_configured: 'login.sso.error_not_configured',
	invalid_state: 'login.sso.error_invalid_state',
	email_unverified: 'login.sso.error_email_unverified',
	domain_not_allowed: 'login.sso.error_domain_not_allowed',
	token_invalid: 'login.sso.error_token_invalid',
	provider_error: 'login.sso.error_provider',
	failed: 'login.sso.error_generic'
};

/** The i18n key for a ?sso_error= value, or the generic key for unknown codes. */
export function ssoErrorKey(code: string | null | undefined): SSOErrorKey {
	if (!code) return ERROR_KEYS.failed;
	return ERROR_KEYS[code] ?? ERROR_KEYS.failed;
}

/** The workspace slug named by an API SSO_REQUIRED error, if any. */
export function ssoRequiredSlug(error: unknown): string | null {
	if (!error || typeof error !== 'object') return null;
	const body = error as { status?: number; error?: { code?: string; workspace_slug?: string } };
	if (body.status !== 403 || body.error?.code !== 'SSO_REQUIRED') return null;
	const slug = normalizeWorkspaceSlug(body.error.workspace_slug ?? '');
	return slug || null;
}
