// A layout choice the person made (a folded sidebar, a closed column), kept
// in this browser. Without storage the layout still works; it is only not
// remembered.
export function readChoice(key: string): string | null {
	try {
		return localStorage.getItem(key);
	} catch {
		return null;
	}
}

export function keepChoice(key: string, value: string) {
	try {
		localStorage.setItem(key, value);
	} catch {
		/* Not remembered; the layout itself is unchanged. */
	}
}
