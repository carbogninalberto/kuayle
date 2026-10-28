import { conversations, type ConversationAttachment, type UploadError } from '$lib/api/conversations';
import { m } from '$lib/paraglide/messages.js';
import { acceptImages, type RejectedFile } from './attachment-rules';

export interface DraftAttachment {
	key: string;
	file: File;
	preview: string;
	progress: number;
	status: 'uploading' | 'ready' | 'failed';
	error?: string;
	attachment?: ConversationAttachment;
}

function rejectionText(rejected: RejectedFile[]) {
	const [first] = rejected;
	if (!first) return '';
	if (first.reason === 'count') return m['assistant.attachment_count']();
	if (first.reason === 'size') return m['assistant.attachment_size']({ name: first.name });
	return m['assistant.attachment_type']({ name: first.name });
}

function uploadErrorText(error: UploadError, name: string) {
	if (error?.status === 413) return m['assistant.attachment_size']({ name });
	if (error?.status === 415) return m['assistant.attachment_type']({ name });
	return m['assistant.attachment_failed']({ name });
}

// Composer images: each is uploaded through the authorized conversation
// endpoint as soon as it is attached, and sent by ID with the message.
export class ComposerAttachments {
	items = $state<DraftAttachment[]>([]);
	notice = $state('');
	readonly uploading = $derived(this.items.some((item) => item.status === 'uploading'));
	readonly failed = $derived(this.items.some((item) => item.status === 'failed'));
	readonly ids = $derived(
		this.items.flatMap((item) => (item.status === 'ready' && item.attachment ? [item.attachment.id] : []))
	);
	private conversation: string | null = null;

	constructor(
		private slug: string,
		private ensureConversation: () => Promise<string>
	) {}

	add(files: Iterable<File>) {
		const { accepted, rejected } = acceptImages(files, this.items.length);
		this.notice = rejectionText(rejected);
		for (const file of accepted) {
			this.items.push({
				key: crypto.randomUUID(),
				file,
				preview: URL.createObjectURL(file),
				progress: 0,
				status: 'uploading'
			});
			void this.upload(this.items[this.items.length - 1]);
		}
	}

	async upload(item: DraftAttachment) {
		item.status = 'uploading';
		item.error = undefined;
		item.progress = 0;
		try {
			this.conversation = await this.ensureConversation();
			const attachment = await conversations.uploadAttachment(this.slug, this.conversation, item.file, (fraction) => {
				item.progress = fraction;
			});
			if (!this.items.includes(item)) {
				// Removed while uploading: discard it on the server too.
				void conversations.removeAttachment(this.slug, this.conversation, attachment.id).catch(() => {});
				return;
			}
			item.attachment = attachment;
			item.progress = 1;
			item.status = 'ready';
		} catch (error) {
			item.status = 'failed';
			item.error = uploadErrorText(error as UploadError, item.file.name);
		}
	}

	retry(key: string) {
		const item = this.items.find((candidate) => candidate.key === key);
		if (item?.status === 'failed') void this.upload(item);
	}

	remove(key: string) {
		const item = this.items.find((candidate) => candidate.key === key);
		if (!item) return;
		this.items = this.items.filter((candidate) => candidate !== item);
		URL.revokeObjectURL(item.preview);
		this.notice = '';
		if (item.attachment && this.conversation)
			void conversations.removeAttachment(this.slug, this.conversation, item.attachment.id).catch(() => {});
	}

	// After a successful send the images belong to the message.
	clear() {
		for (const item of this.items) URL.revokeObjectURL(item.preview);
		this.items = [];
		this.notice = '';
	}
}
