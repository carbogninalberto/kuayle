// Client-side mirror of the API's GitHub bot rules, so the form explains a
// problem before saving; the API remains the authority.

export const BOT_NAME_MAX = 64;

// Characters the API refuses in a bot name: they could break out of the plain
// text Kuayle renders on GitHub or mention someone.
const FORBIDDEN_NAME = /[\p{Cc}<>@`[\]\\*_~|#]/u;

export type BotNameIssue = 'length' | 'characters';

export function validateBotName(raw: string): BotNameIssue | null {
	const name = raw.trim();
	if ([...name].length > BOT_NAME_MAX) return 'length';
	return FORBIDDEN_NAME.test(name) ? 'characters' : null;
}

export function validateBotAvatarUrl(raw: string): 'url' | null {
	const value = raw.trim();
	if (value.length > 2048 || /["'<>\\`\s]/.test(value)) return 'url';
	try {
		const url = new URL(value);
		return url.protocol === 'https:' && url.host && !url.username && !url.password ? null : 'url';
	} catch {
		return 'url';
	}
}

// SelectField treats an empty value as its placeholder, so the default
// harness uses a sentinel in the form and an empty value in the API.
export const DEFAULT_MENTION_HARNESS = 'default';

export function mentionHarnessChoice(harness: string): string {
	return harness || DEFAULT_MENTION_HARNESS;
}

export function mentionHarnessFromChoice(choice: string): string {
	return choice === DEFAULT_MENTION_HARNESS ? '' : choice;
}
