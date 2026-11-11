// What this tab currently shows that a notification can be about, e.g. the
// open assistant conversation. An approval asked by a conversation the user is
// looking at needs no sound or system notification (see notification-policy.ts).

export function createOnScreenSubjects() {
	const holders = new Map<string, number>();
	return {
		/** Marks a subject as shown until the returned function is called. */
		show(subject: string): () => void {
			holders.set(subject, (holders.get(subject) ?? 0) + 1);
			let released = false;
			return () => {
				if (released) return;
				released = true;
				const left = (holders.get(subject) ?? 1) - 1;
				if (left > 0) holders.set(subject, left);
				else holders.delete(subject);
			};
		},
		has(subject: string): boolean {
			return holders.has(subject);
		}
	};
}

/** The subjects this tab shows; components register what they display. */
export const onScreenSubjects = createOnScreenSubjects();

/** The subject key of an assistant conversation. */
export function conversationSubject(conversationId: string): string {
	return `conversation:${conversationId}`;
}
