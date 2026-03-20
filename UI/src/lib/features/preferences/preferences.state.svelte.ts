import { getPreferences, updatePreferences } from '$lib/api/preferences';

type FontSize = 'small' | 'default' | 'large';
type ThemeMode = 'system' | 'light' | 'dark';
type LightTheme = 'light' | 'rose-light' | 'blue-light';
type DarkTheme = 'dark' | 'amethyst-dark' | 'emerald-dark' | 'cyber-77' | 'blade-49' | 'pipboy';

interface PreferencesData {
	fontSize: FontSize;
	pointerCursors: boolean;
	themeMode: ThemeMode;
	lightTheme: LightTheme;
	darkTheme: DarkTheme;
}

const STORAGE_KEY = 'carbon-preferences';

// Percentage values applied to <html> font-size so all rem-based
// Tailwind utilities (text-sm, text-xs, etc.) scale proportionally.
const FONT_SIZE_SCALE: Record<FontSize, string> = {
	small: '87.5%',
	default: '100%',
	large: '112.5%',
};

class PreferencesState {
	fontSize = $state<FontSize>('default');
	pointerCursors = $state(true);
	themeMode = $state<ThemeMode>('dark');
	lightTheme = $state<LightTheme>('light');
	darkTheme = $state<DarkTheme>('dark');

	private systemPrefersDark = $state(true);
	private initialized = false;

	resolvedMode = $derived<'light' | 'dark'>(
		this.themeMode === 'system' ? (this.systemPrefersDark ? 'dark' : 'light') : this.themeMode
	);

	activeTheme = $derived<string>(
		this.resolvedMode === 'dark' ? this.darkTheme : this.lightTheme
	);

	fontSizeScale = $derived(FONT_SIZE_SCALE[this.fontSize]);

	init() {
		if (this.initialized) return;
		this.initialized = true;

		this.loadLocal();
		this.loadRemote();

		const mql = window.matchMedia('(prefers-color-scheme: dark)');
		this.systemPrefersDark = mql.matches;
		mql.addEventListener('change', (e) => {
			this.systemPrefersDark = e.matches;
		});

		$effect(() => {
			const classes: string[] = [this.activeTheme];
			if (this.pointerCursors) {
				classes.push('pointer-cursors');
			}
			document.documentElement.className = classes.join(' ');
			document.documentElement.style.setProperty('--app-font-size', this.fontSizeScale);
		});
	}

	private loadLocal() {
		try {
			const raw = localStorage.getItem(STORAGE_KEY);
			if (!raw) return;
			const data: Partial<PreferencesData> = JSON.parse(raw);
			if (data.fontSize) this.fontSize = data.fontSize;
			if (data.pointerCursors !== undefined) this.pointerCursors = data.pointerCursors;
			if (data.themeMode) this.themeMode = data.themeMode;
			if (data.lightTheme) this.lightTheme = data.lightTheme;
			if (data.darkTheme) this.darkTheme = data.darkTheme;
		} catch {
			// ignore corrupt data
		}
	}

	private async loadRemote() {
		try {
			const data = await getPreferences();
			this.fontSize = data.font_size as FontSize;
			this.pointerCursors = data.pointer_cursors;
			this.themeMode = data.theme_mode as ThemeMode;
			this.lightTheme = data.light_theme as LightTheme;
			this.darkTheme = data.dark_theme as DarkTheme;
			this.persistLocal();
		} catch {
			// API unavailable — local-only is fine
		}
	}

	private persistLocal() {
		const data: PreferencesData = {
			fontSize: this.fontSize,
			pointerCursors: this.pointerCursors,
			themeMode: this.themeMode,
			lightTheme: this.lightTheme,
			darkTheme: this.darkTheme,
		};
		localStorage.setItem(STORAGE_KEY, JSON.stringify(data));
	}

	private persist() {
		this.persistLocal();
		updatePreferences({
			font_size: this.fontSize,
			pointer_cursors: this.pointerCursors,
			theme_mode: this.themeMode,
			light_theme: this.lightTheme,
			dark_theme: this.darkTheme,
		}).catch(() => {
			// fire-and-forget — localStorage is the primary source for instant UX
		});
	}

	setFontSize(size: FontSize) {
		this.fontSize = size;
		this.persist();
	}

	setPointerCursors(enabled: boolean) {
		this.pointerCursors = enabled;
		this.persist();
	}

	setThemeMode(mode: ThemeMode) {
		this.themeMode = mode;
		this.persist();
	}

	setLightTheme(theme: LightTheme) {
		this.lightTheme = theme;
		this.persist();
	}

	setDarkTheme(theme: DarkTheme) {
		this.darkTheme = theme;
		this.persist();
	}
}

export const preferencesState = new PreferencesState();
