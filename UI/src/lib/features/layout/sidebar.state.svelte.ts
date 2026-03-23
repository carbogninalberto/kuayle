const STORAGE_KEY = 'sidebar_collapsed_panel';

class SidebarState {
	collapsed = $state(
		typeof localStorage !== 'undefined'
			? localStorage.getItem(STORAGE_KEY) === 'true'
			: false
	);

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

	private persist() {
		if (typeof localStorage !== 'undefined') {
			localStorage.setItem(STORAGE_KEY, String(this.collapsed));
		}
	}
}

export const sidebarState = new SidebarState();
