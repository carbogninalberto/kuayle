export type PasswordIssue = 'too_short' | 'weak' | 'mismatch';

/** Mirrors the backend rules: 12+ characters with an uppercase letter, a lowercase letter and a digit. */
export function validateNewPassword(password: string, confirm: string): PasswordIssue | null {
	if (password.length < 12) return 'too_short';
	if (!/\p{Lu}/u.test(password) || !/\p{Ll}/u.test(password) || !/\p{Nd}/u.test(password)) return 'weak';
	if (password !== confirm) return 'mismatch';
	return null;
}
