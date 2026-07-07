import type { Project } from '$lib/types/project';
import type { Team } from '$lib/types/team';

const STORAGE_KEY = 'sidebar_collapsed_panel';

const TEAM_COLORS = [
	'#6366f1', // indigo
	'#f43f5e', // rose
	'#10b981', // emerald
	'#f59e0b', // amber
	'#3b82f6', // blue
	'#8b5cf6', // violet
	'#ec4899', // pink
	'#14b8a6', // teal
	'#ef4444', // red
	'#06b6d4', // cyan
];

export function getTeamColor(index: number): string {
	return TEAM_COLORS[index % TEAM_COLORS.length];
}

function hashString(value: string): number {
	let hash = 0;
	for (let i = 0; i < value.length; i++) {
		hash = (hash * 31 + value.charCodeAt(i)) >>> 0;
	}
	return hash;
}

export function getStableTeamColor(team: Pick<Team, 'id' | 'key' | 'color'>): string {
	if (team.color) return team.color;
	return getTeamColor(hashString(team.id || team.key));
}

class SidebarState {
	collapsed = $state(
		typeof localStorage !== 'undefined'
			? localStorage.getItem(STORAGE_KEY) === 'true'
			: false
	);

	projects = $state<Project[]>([]);
	teams = $state<Team[]>([]);

	getTeam(id: string): Team | undefined {
		return this.teams.find(t => t.id === id);
	}

	getTeamColor(id: string): string {
		const team = this.getTeam(id);
		return team ? getStableTeamColor(team) : getTeamColor(hashString(id));
	}

	toggle() {
		this.collapsed = !this.collapsed;
		this.persist();
	}

	expand() {
		this.collapsed = false;
		this.persist();
	}

	collapse() {
		this.collapsed = true;
		this.persist();
	}

	addProject(project: Project) {
		this.projects = [...this.projects, project];
	}

	private persist() {
		if (typeof localStorage !== 'undefined') {
			localStorage.setItem(STORAGE_KEY, String(this.collapsed));
		}
	}
}

export const sidebarState = new SidebarState();
