/**
 * Locale registry for the language switcher and docs.
 *
 * `full`: maintained by the project and reviewed by native speakers.
 * `community`: machine-assisted translations; may contain awkward wording.
 * `caveats` lists known limitations documented in the README.
 */
export type LocaleSupport = 'full' | 'community';
export type LocaleCaveat = 'plurals' | 'line-breaks' | 'rtl';

export interface LocaleInfo {
	label: string;
	english: string;
	support: LocaleSupport;
	caveats?: LocaleCaveat[];
}

export const LOCALES: Record<string, LocaleInfo> = {
	en: { label: 'English', english: 'English', support: 'full' },
	it: { label: 'Italiano', english: 'Italian', support: 'full' },
	'zh-CN': { label: '简体中文', english: 'Simplified Chinese', support: 'full' },
	'zh-TW': { label: '繁體中文', english: 'Traditional Chinese', support: 'full' },
	es: { label: 'Español', english: 'Spanish', support: 'community' },
	'pt-BR': { label: 'Português (Brasil)', english: 'Portuguese (Brazil)', support: 'community' },
	'pt-PT': { label: 'Português (Portugal)', english: 'Portuguese (Portugal)', support: 'community' },
	de: { label: 'Deutsch', english: 'German', support: 'community' },
	fr: { label: 'Français', english: 'French', support: 'community' },
	ja: { label: '日本語', english: 'Japanese', support: 'community' },
	ko: { label: '한국어', english: 'Korean', support: 'community' },
	nl: { label: 'Nederlands', english: 'Dutch', support: 'community' },
	tr: { label: 'Türkçe', english: 'Turkish', support: 'community' },
	vi: { label: 'Tiếng Việt', english: 'Vietnamese', support: 'community' },
	id: { label: 'Bahasa Indonesia', english: 'Indonesian', support: 'community' },
	ms: { label: 'Bahasa Melayu', english: 'Malay', support: 'community' },
	hi: { label: 'हिन्दी', english: 'Hindi', support: 'community' },
	sv: { label: 'Svenska', english: 'Swedish', support: 'community' },
	da: { label: 'Dansk', english: 'Danish', support: 'community' },
	nb: { label: 'Norsk bokmål', english: 'Norwegian Bokmål', support: 'community' },
	ca: { label: 'Català', english: 'Catalan', support: 'community' },
	ru: { label: 'Русский', english: 'Russian', support: 'community', caveats: ['plurals'] },
	pl: { label: 'Polski', english: 'Polish', support: 'community', caveats: ['plurals'] },
	uk: { label: 'Українська', english: 'Ukrainian', support: 'community', caveats: ['plurals'] },
	cs: { label: 'Čeština', english: 'Czech', support: 'community', caveats: ['plurals'] },
	ro: { label: 'Română', english: 'Romanian', support: 'community', caveats: ['plurals'] },
	hu: { label: 'Magyar', english: 'Hungarian', support: 'community', caveats: ['plurals'] },
	el: { label: 'Ελληνικά', english: 'Greek', support: 'community', caveats: ['plurals'] },
	fi: { label: 'Suomi', english: 'Finnish', support: 'community', caveats: ['plurals'] },
	th: { label: 'ไทย', english: 'Thai', support: 'community', caveats: ['line-breaks'] },
	ar: { label: 'العربية', english: 'Arabic', support: 'community', caveats: ['rtl', 'plurals'] },
	he: { label: 'עברית', english: 'Hebrew', support: 'community', caveats: ['rtl'] },
	fa: { label: 'فارسی', english: 'Persian', support: 'community', caveats: ['rtl'] }
};

/** Native display name for a locale code, falling back to the code itself. */
export function localeLabel(code: string): string {
	return LOCALES[code]?.label ?? code;
}
