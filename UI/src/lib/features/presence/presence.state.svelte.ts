import type { WorkspaceMember } from '$lib/types/workspace';

const COLORS = [
	'#3b82f6', // blue
	'#22c55e', // green
	'#f97316', // orange
	'#a855f7', // purple
	'#ec4899', // pink
	'#14b8a6', // teal
	'#f59e0b', // amber
	'#ef4444' // red
];

function getColor(userId: string): string {
	let hash = 0;
	for (let i = 0; i < userId.length; i++) {
		hash = (hash * 31 + userId.charCodeAt(i)) | 0;
	}
	return COLORS[Math.abs(hash) % COLORS.length];
}

export interface PresenceUser {
	user_id: string;
	name: string;
	color: string;
	cursor?: { x: number; y: number };
	last_seen: number;
}

class PresenceState {
	viewers = $state<Map<string, PresenceUser>>(new Map());
	issueId = $state<string | null>(null);
	private cleanupInterval: ReturnType<typeof setInterval> | null = null;
	private members: WorkspaceMember[] = [];

	activeViewers = $derived([...this.viewers.values()]);

	join(issueId: string, members?: WorkspaceMember[]) {
		this.issueId = issueId;
		if (members) this.members = members;
		this.viewers = new Map();

		window.dispatchEvent(
			new CustomEvent('ws:send', {
				detail: { type: 'presence.join', payload: { issue_id: issueId } }
			})
		);

		if (!this.cleanupInterval) {
			this.cleanupInterval = setInterval(() => this.removeStale(), 5000);
		}
	}

	leave() {
		if (this.issueId) {
			window.dispatchEvent(
				new CustomEvent('ws:send', {
					detail: { type: 'presence.leave', payload: { issue_id: this.issueId } }
				})
			);
		}
		this.issueId = null;
		this.viewers = new Map();
		if (this.cleanupInterval) {
			clearInterval(this.cleanupInterval);
			this.cleanupInterval = null;
		}
	}

	setMembers(members: WorkspaceMember[]) {
		this.members = members;
		this.resolveNames();
	}

	handleJoin(payload: { issue_id: string; user_id: string }) {
		if (!this.issueId || payload.issue_id !== this.issueId) return;
		const next = new Map(this.viewers);
		next.set(payload.user_id, {
			user_id: payload.user_id,
			name: this.resolveName(payload.user_id),
			color: getColor(payload.user_id),
			last_seen: Date.now()
		});
		this.viewers = next;
	}

	handleLeave(payload: { issue_id: string; user_id: string }) {
		if (!this.issueId || payload.issue_id !== this.issueId) return;
		const next = new Map(this.viewers);
		next.delete(payload.user_id);
		this.viewers = next;
	}

	handleSync(payload: { issue_id: string; users: string[] }) {
		if (!this.issueId || payload.issue_id !== this.issueId) return;
		const next = new Map<string, PresenceUser>();
		for (const userId of payload.users) {
			next.set(userId, {
				user_id: userId,
				name: this.resolveName(userId),
				color: getColor(userId),
				last_seen: Date.now()
			});
		}
		this.viewers = next;
	}

	handleCursorMove(payload: { issue_id: string; user_id: string; x: number; y: number }) {
		if (!this.issueId || payload.issue_id !== this.issueId) return;
		const next = new Map(this.viewers);
		const existing = next.get(payload.user_id);
		if (existing) {
			next.set(payload.user_id, {
				...existing,
				cursor: { x: payload.x, y: payload.y },
				last_seen: Date.now()
			});
		} else {
			// Auto-add viewer from cursor event (they joined before us)
			next.set(payload.user_id, {
				user_id: payload.user_id,
				name: this.resolveName(payload.user_id),
				color: getColor(payload.user_id),
				cursor: { x: payload.x, y: payload.y },
				last_seen: Date.now()
			});
		}
		this.viewers = next;
	}

	private resolveName(userId: string): string {
		const member = this.members.find((m) => m.user_id === userId);
		return member?.name ?? 'Unknown';
	}

	private resolveNames() {
		if (this.viewers.size === 0 || this.members.length === 0) return;
		let changed = false;
		const next = new Map(this.viewers);
		for (const [id, user] of next) {
			if (user.name === 'Unknown') {
				const name = this.resolveName(id);
				if (name !== 'Unknown') {
					next.set(id, { ...user, name });
					changed = true;
				}
			}
		}
		if (changed) this.viewers = next;
	}

	private removeStale() {
		const now = Date.now();
		let changed = false;
		const next = new Map(this.viewers);
		for (const [id, user] of next) {
			if (now - user.last_seen > 15000) {
				next.delete(id);
				changed = true;
			}
		}
		if (changed) {
			this.viewers = next;
		}
	}
}

export const presenceState = new PresenceState();
