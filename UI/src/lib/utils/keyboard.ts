type KeyHandler = (e: KeyboardEvent) => void;

interface Shortcut {
	key: string;
	ctrl?: boolean;
	meta?: boolean;
	shift?: boolean;
	handler: KeyHandler;
}

export function createKeyboardHandler(shortcuts: Shortcut[]): KeyHandler {
	return (e: KeyboardEvent) => {
		const target = e.target as HTMLElement;
		if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable) {
			return;
		}

		for (const shortcut of shortcuts) {
			const metaOrCtrl = shortcut.meta || shortcut.ctrl;
			if (
				e.key.toLowerCase() === shortcut.key.toLowerCase() &&
				(!metaOrCtrl || e.metaKey || e.ctrlKey) &&
				(!shortcut.shift || e.shiftKey)
			) {
				e.preventDefault();
				shortcut.handler(e);
				return;
			}
		}
	};
}
