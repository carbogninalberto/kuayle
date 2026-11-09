// Autosave for a wiki page, with the issue description's timing: edits are
// sent two seconds after the last change, and leaving flushes immediately.
// Saves are serialized and carry the page version, so a page changed by
// someone else is never overwritten: the save stops at a conflict and keeps
// the local text. Free of Svelte and $lib imports for the Node unit tests.
import { normalizeWikiContent } from './wiki-draft.ts';

export type SaveState = 'idle' | 'pending' | 'saving' | 'saved' | 'error' | 'conflict';

export interface WikiFields {
	title: string;
	content: string;
}

export interface SavedPage extends WikiFields {
	version: number;
}

export interface WikiAutosaveOptions<P extends SavedPage> {
	save: (changes: Partial<WikiFields>, version: number) => Promise<P>;
	onstate: (state: SaveState) => void;
	onsaved: (page: P) => void;
	/** Errors other than a version conflict. */
	onerror?: (error: unknown) => void;
	delay?: number;
}

export const AUTOSAVE_DELAY_MS = 2000;

// A page without content (a new one, or a partial response) counts as empty.
function snapshot(page: SavedPage): SavedPage {
	return { title: page.title ?? '', content: page.content ?? '', version: page.version };
}

function isConflict(error: unknown): boolean {
	return (error as { status?: number } | null)?.status === 409;
}

export class WikiAutosave<P extends SavedPage> {
	private saved: SavedPage;
	private pending: Partial<WikiFields> = {};
	private timer: ReturnType<typeof setTimeout> | null = null;
	private running: Promise<void> | null = null;
	private conflicted = false;
	private readonly delay: number;
	private readonly options: WikiAutosaveOptions<P>;

	constructor(page: SavedPage, options: WikiAutosaveOptions<P>) {
		this.options = options;
		this.saved = snapshot(page);
		this.delay = options.delay ?? AUTOSAVE_DELAY_MS;
	}

	/** Whether there are edits the server does not have yet. */
	get dirty(): boolean {
		return Object.keys(this.changes()).length > 0;
	}

	get conflict(): boolean {
		return this.conflicted;
	}

	/** Records an edit and saves it after the delay. */
	edit(changes: Partial<WikiFields>) {
		this.pending = { ...this.pending, ...changes };
		if (this.conflicted) return;
		if (!this.dirty) {
			this.clearTimer();
			return;
		}
		this.options.onstate('pending');
		this.clearTimer();
		this.timer = setTimeout(() => {
			this.timer = null;
			void this.flush();
		}, this.delay);
	}

	/** Saves pending edits now, after any save already running. */
	async flush(): Promise<void> {
		this.clearTimer();
		while (this.running) await this.running;
		if (this.conflicted || !this.dirty) return;
		this.running = this.send().finally(() => (this.running = null));
		await this.running;
	}

	/** Adopts a page loaded from the server, dropping any local edits. */
	reset(page: SavedPage) {
		this.clearTimer();
		this.saved = snapshot(page);
		this.pending = {};
		this.conflicted = false;
		this.options.onstate('idle');
	}

	/** The server's copy changed without local edits pending: track it. */
	follow(page: SavedPage) {
		this.saved = snapshot(page);
	}

	private changes(): Partial<WikiFields> {
		const changes: Partial<WikiFields> = {};
		const { title, content } = this.pending;
		if (title !== undefined && title.trim() !== this.saved.title) changes.title = title.trim();
		if (content !== undefined && normalizeWikiContent(content) !== normalizeWikiContent(this.saved.content))
			changes.content = content;
		return changes;
	}

	private async send() {
		const changes = this.changes();
		this.options.onstate('saving');
		try {
			const page = await this.options.save(changes, this.saved.version);
			this.saved = snapshot({ ...page, content: changes.content ?? page.content });
			this.options.onsaved(page);
			this.options.onstate(this.dirty ? 'pending' : 'saved');
			if (this.dirty) await this.send();
		} catch (error) {
			if (isConflict(error)) {
				this.conflicted = true;
				this.options.onstate('conflict');
			} else {
				this.options.onstate('error');
				this.options.onerror?.(error);
			}
		}
	}

	private clearTimer() {
		if (this.timer) clearTimeout(this.timer);
		this.timer = null;
	}
}
