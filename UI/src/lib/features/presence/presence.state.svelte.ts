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

	activeViewers = $derived([...this.viewers.values()]);

	join(issueId: string) {
		this.issueId = issueId;
		this.viewers = new Map();

		window.dispatchEvent(
			new CustomEvent('ws:send', {
				detail: { type: 'presence.join', payload: { issue_id: issueId } }
			})
		);

		this.cleanupInterval = setInterval(() => this.removeStale(), 5000);
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

	handleJoin(payload: { issue_id: string; user_id: string }, members: WorkspaceMember[]) {
		if (payload.issue_id !== this.issueId) return;
		const member = members.find((m) => m.user_id === payload.user_id);
		const next = new Map(this.viewers);
		next.set(payload.user_id, {
			user_id: payload.user_id,
			name: member?.name ?? 'Unknown',
			color: getColor(payload.user_id),
			last_seen: Date.now()
		});
		this.viewers = next;
	}

	handleLeave(payload: { issue_id: string; user_id: string }) {
		if (payload.issue_id !== this.issueId) return;
		const next = new Map(this.viewers);
		next.delete(payload.user_id);
		this.viewers = next;
	}

	handleSync(payload: { issue_id: string; users: string[] }, members: WorkspaceMember[]) {
		if (payload.issue_id !== this.issueId) return;
		const next = new Map<string, PresenceUser>();
		for (const userId of payload.users) {
			const member = members.find((m) => m.user_id === userId);
			next.set(userId, {
				user_id: userId,
				name: member?.name ?? 'Unknown',
				color: getColor(userId),
				last_seen: Date.now()
			});
		}
		this.viewers = next;
	}

	resolveNames(members: WorkspaceMember[]) {
		if (this.viewers.size === 0) return;
		let changed = false;
		const next = new Map(this.viewers);
		for (const [id, user] of next) {
			if (user.name === 'Unknown') {
				const member = members.find((m) => m.user_id === id);
				if (member) {
					next.set(id, { ...user, name: member.name });
					changed = true;
				}
			}
		}
		if (changed) this.viewers = next;
	}

	handleCursorMove(payload: { issue_id: string; user_id: string; x: number; y: number }) {
		if (payload.issue_id !== this.issueId) return;
		const existing = this.viewers.get(payload.user_id);
		if (existing) {
			const next = new Map(this.viewers);
			next.set(payload.user_id, {
				...existing,
				cursor: { x: payload.x, y: payload.y },
				last_seen: Date.now()
			});
			this.viewers = next;
		}
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
