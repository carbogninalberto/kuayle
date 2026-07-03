import { updatePreferences } from '$lib/api/preferences';
import { CATEGORY_ORDER, type StatusCategory } from '$lib/types/team-status';

type FontSize = 'small' | 'default' | 'large';
type ThemeMode = 'system' | 'light' | 'dark';
type LightTheme = 'light' | 'rose-light' | 'blue-light';
type DarkTheme = 'dark' | 'dark-gray' | 'amethyst-dark' | 'emerald-dark' | 'cyber-77' | 'blade-49' | 'pipboy';
export type WorkflowSortMode = 'default' | 'active-first' | 'custom';
export type TeamWorkflowSortMode = WorkflowSortMode | 'inherit';

export interface TeamWorkflowSortOverride {
	mode: TeamWorkflowSortMode;
	workflowSortOrder?: StatusCategory[];
}

interface PreferencesData {
	fontSize: FontSize;
	pointerCursors: boolean;
	themeMode: ThemeMode;
	lightTheme: LightTheme;
	darkTheme: DarkTheme;
	workflowSortMode: WorkflowSortMode;
	workflowSortOrder: StatusCategory[];
	teamWorkflowSortOverrides: Record<string, TeamWorkflowSortOverride>;
}

const STORAGE_KEY = 'kuayle-preferences';

// Percentage values applied to <html> font-size so all rem-based
// Tailwind utilities (text-sm, text-xs, etc.) scale proportionally.
const FONT_SIZE_SCALE: Record<FontSize, string> = {
	small: '87.5%',
	default: '100%',
	large: '112.5%',
};

const DEFAULT_WORKFLOW_SORT_ORDER = [...CATEGORY_ORDER];
const ACTIVE_FIRST_WORKFLOW_SORT_ORDER: StatusCategory[] = ['started', 'unstarted', 'backlog', 'completed', 'cancelled'];

class PreferencesState {
	fontSize = $state<FontSize>('default');
	pointerCursors = $state(true);
	themeMode = $state<ThemeMode>('dark');
	lightTheme = $state<LightTheme>('light');
	darkTheme = $state<DarkTheme>('dark');
	workflowSortMode = $state<WorkflowSortMode>('default');
	workflowSortOrder = $state<StatusCategory[]>([...DEFAULT_WORKFLOW_SORT_ORDER]);
	teamWorkflowSortOverrides = $state<Record<string, TeamWorkflowSortOverride>>({});

	private systemPrefersDark = $state(true);
	private initialized = false;
	private remoteLoaded = false;

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

	syncRemote() {
		if (this.remoteLoaded) return;
		this.remoteLoaded = true;
		this.loadRemote();
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
			if (data.workflowSortMode) this.workflowSortMode = data.workflowSortMode;
			if (data.workflowSortOrder) this.workflowSortOrder = normalizeWorkflowSortOrder(data.workflowSortOrder);
			if (data.teamWorkflowSortOverrides) this.teamWorkflowSortOverrides = normalizeTeamOverrides(data.teamWorkflowSortOverrides);
		} catch {
			// ignore corrupt data
		}
	}

	private async loadRemote() {
		try {
			// Use raw fetch to avoid the authenticated client's 401 → /login redirect.
			// On public pages (e.g. /share/:token) there is no session, and the
			// redirect would kick the visitor to the login page before the catch fires.
			const res = await fetch('/api/preferences', {
				credentials: 'include',
				headers: { 'Content-Type': 'application/json' }
			});
			if (!res.ok) return;
			const data = await res.json();
			this.fontSize = data.font_size as FontSize;
			this.pointerCursors = data.pointer_cursors;
			this.themeMode = data.theme_mode as ThemeMode;
			this.lightTheme = data.light_theme as LightTheme;
			this.darkTheme = data.dark_theme as DarkTheme;
			this.workflowSortMode = (data.workflow_sort_mode ?? 'default') as WorkflowSortMode;
			this.workflowSortOrder = normalizeWorkflowSortOrder(data.workflow_sort_order);
			this.teamWorkflowSortOverrides = normalizeTeamOverrides(
				Object.fromEntries(
					Object.entries(data.team_workflow_sort_overrides ?? {}).map(([key, override]: [string, any]) => [
						key,
						{
							mode: override.mode,
							workflowSortOrder: override.workflow_sort_order
						}
					])
				)
			);
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
			workflowSortMode: this.workflowSortMode,
			workflowSortOrder: this.workflowSortOrder,
			teamWorkflowSortOverrides: this.teamWorkflowSortOverrides,
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
			workflow_sort_mode: this.workflowSortMode,
			workflow_sort_order: this.workflowSortOrder,
			team_workflow_sort_overrides: Object.fromEntries(
				Object.entries(this.teamWorkflowSortOverrides).map(([key, override]) => [
					key,
					{
						mode: override.mode,
						workflow_sort_order: override.workflowSortOrder
					}
				])
			),
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

	setWorkflowSortMode(mode: WorkflowSortMode) {
		this.workflowSortMode = mode;
		this.persist();
	}

	setWorkflowSortOrder(order: StatusCategory[]) {
		this.workflowSortOrder = normalizeWorkflowSortOrder(order);
		this.workflowSortMode = 'custom';
		this.persist();
	}

	setTeamWorkflowSortOverride(workspaceSlug: string, teamId: string, override: TeamWorkflowSortOverride) {
		const key = teamWorkflowSortKey(workspaceSlug, teamId);
		this.teamWorkflowSortOverrides = {
			...this.teamWorkflowSortOverrides,
			[key]: {
				mode: override.mode,
				workflowSortOrder: override.workflowSortOrder ? normalizeWorkflowSortOrder(override.workflowSortOrder) : undefined
			}
		};
		this.persist();
	}

	getTeamWorkflowSortOverride(workspaceSlug: string, teamId: string): TeamWorkflowSortOverride {
		return this.teamWorkflowSortOverrides[teamWorkflowSortKey(workspaceSlug, teamId)] ?? { mode: 'inherit' };
	}

	getWorkflowSortMode(workspaceSlug?: string, teamId?: string): WorkflowSortMode {
		if (workspaceSlug && teamId) {
			const override = this.getTeamWorkflowSortOverride(workspaceSlug, teamId);
			if (override.mode !== 'inherit') return override.mode;
		}
		return this.workflowSortMode;
	}

	getWorkflowSortOrder(workspaceSlug?: string, teamId?: string): StatusCategory[] {
		if (workspaceSlug && teamId) {
			const override = this.getTeamWorkflowSortOverride(workspaceSlug, teamId);
			if (override.mode === 'active-first') return [...ACTIVE_FIRST_WORKFLOW_SORT_ORDER];
			if (override.mode === 'custom') return normalizeWorkflowSortOrder(override.workflowSortOrder);
			if (override.mode === 'default') return [...DEFAULT_WORKFLOW_SORT_ORDER];
		}
		if (this.workflowSortMode === 'active-first') return [...ACTIVE_FIRST_WORKFLOW_SORT_ORDER];
		if (this.workflowSortMode === 'custom') return normalizeWorkflowSortOrder(this.workflowSortOrder);
		return [...DEFAULT_WORKFLOW_SORT_ORDER];
	}
}

function teamWorkflowSortKey(workspaceSlug: string, teamId: string) {
	return `${workspaceSlug}/${teamId}`;
}

function normalizeWorkflowSortOrder(order?: string[] | StatusCategory[]): StatusCategory[] {
	if (!order) return [...DEFAULT_WORKFLOW_SORT_ORDER];
	const valid = new Set<StatusCategory>(DEFAULT_WORKFLOW_SORT_ORDER);
	const normalized: StatusCategory[] = [];
	for (const category of order) {
		const normalizedCategory = category as StatusCategory;
		if (valid.has(normalizedCategory) && !normalized.includes(normalizedCategory)) {
			normalized.push(normalizedCategory);
		}
	}
	for (const category of DEFAULT_WORKFLOW_SORT_ORDER) {
		if (!normalized.includes(category)) normalized.push(category);
	}
	return normalized.slice(0, DEFAULT_WORKFLOW_SORT_ORDER.length);
}

function normalizeTeamOverrides(overrides: Record<string, TeamWorkflowSortOverride>) {
	return Object.fromEntries(
		Object.entries(overrides).map(([key, override]) => [
			key,
			{
				mode: override.mode ?? 'inherit',
				workflowSortOrder: override.workflowSortOrder ? normalizeWorkflowSortOrder(override.workflowSortOrder) : undefined
			}
		])
	) as Record<string, TeamWorkflowSortOverride>;
}

export const preferencesState = new PreferencesState();
