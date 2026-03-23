import type { Project } from '$lib/types/project';

const STORAGE_KEY = 'sidebar_collapsed_panel';

class SidebarState {
	collapsed = $state(
		typeof localStorage !== 'undefined'
			? localStorage.getItem(STORAGE_KEY) === 'true'
			: false
	);

	projects = $state<Project[]>([]);

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
