<script module lang="ts">
	import { BubbleMenuView } from '@tiptap/extension-bubble-menu';
	class SelectionBubbleMenuView extends BubbleMenuView {
		disposed = false;

		override updatePosition() {
			if (this.disposed) return;
			if (this.editor.isDestroyed || !this.getShouldShow()) {
				this.hide();
				return;
			}
			super.updatePosition();
		}

		override show() {
			if (!this.disposed && !this.editor.isDestroyed && this.getShouldShow()) super.show();
		}

		override hide() {
			if (this.disposed) return;
			super.hide();
			this.element.remove();
		}

		override destroy() {
			super.destroy();
			this.disposed = true;
		}
	}
</script>

<script lang="ts">
	import { onMount, type Snippet } from 'svelte';
	import type { BubbleMenuPluginProps } from '@tiptap/extension-bubble-menu';
	import { Plugin, PluginKey } from '@tiptap/pm/state';

	let {
		editor,
		shouldShow,
		children
	}: {
		editor: BubbleMenuPluginProps['editor'];
		shouldShow: NonNullable<BubbleMenuPluginProps['shouldShow']>;
		children: Snippet;
	} = $props();
	let element: HTMLDivElement;

	onMount(() => {
		// Tiptap 3.20.4's positioning callback sets visibility even while its
		// logical menu state is hidden. Keep hidden menus detached, including
		// before their first selection, so a late callback cannot reveal them.
		element.remove();

		const pluginKey = new PluginKey('editorBubbleMenu');
		let menu: SelectionBubbleMenuView;
		editor.registerPlugin(
			new Plugin({
				key: pluginKey,
				view: (view) => {
					menu = new SelectionBubbleMenuView({ editor, view, element, pluginKey, shouldShow });
					return menu;
				}
			})
		);

		// Scroll does not bubble from the issue page's nested containers.
		const reposition = () => menu.updatePosition();
		const dismissIneligible = () => {
			if (editor.isDestroyed) return;
			if (!menu.getShouldShow()) menu.hide();
			else menu.update(editor.view);
		};
		document.addEventListener('scroll', reposition, true);
		document.addEventListener('selectionchange', dismissIneligible);
		document.addEventListener('focusin', dismissIneligible);
		return () => {
			document.removeEventListener('scroll', reposition, true);
			document.removeEventListener('selectionchange', dismissIneligible);
			document.removeEventListener('focusin', dismissIneligible);
			editor.unregisterPlugin(pluginKey);
			element.remove();
		};
	});
</script>

<div bind:this={element} style="visibility: hidden; position: absolute;">
	{@render children()}
</div>
