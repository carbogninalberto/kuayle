type FontSize = 'small' | 'default' | 'large';
type ThemeMode = 'system' | 'light' | 'dark';
type LightTheme = 'light' | 'rose-light' | 'blue-light';
type DarkTheme = 'dark' | 'amethyst-dark' | 'emerald-dark';

interface PreferencesData {
	fontSize: FontSize;
	pointerCursors: boolean;
	themeMode: ThemeMode;
	lightTheme: LightTheme;
	darkTheme: DarkTheme;
}

const STORAGE_KEY = 'carbon-preferences';

const FONT_SIZE_MAP: Record<FontSize, number> = {
	small: 13,
	default: 14,
	large: 16,
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

	fontSizePx = $derived(FONT_SIZE_MAP[this.fontSize]);

	init() {
		if (this.initialized) return;
		this.initialized = true;

		this.load();

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
			document.documentElement.style.setProperty('--app-font-size', `${this.fontSizePx}px`);
		});
	}

	private load() {
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

	private persist() {
		const data: PreferencesData = {
			fontSize: this.fontSize,
			pointerCursors: this.pointerCursors,
			themeMode: this.themeMode,
			lightTheme: this.lightTheme,
			darkTheme: this.darkTheme,
		};
		localStorage.setItem(STORAGE_KEY, JSON.stringify(data));
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
