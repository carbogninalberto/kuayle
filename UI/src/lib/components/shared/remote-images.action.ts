import { mount, unmount } from 'svelte';
import {
	REMOTE_IMAGE_ATTRIBUTE,
	allowRemoteImage,
	isRemoteImageAllowed,
	onRemoteImageChoice,
	remoteImageHost,
	trustRemoteImageHost
} from '$lib/security/remote-images';
import RemoteImagePlaceholder from './RemoteImagePlaceholder.svelte';

/** Marks the element holding a placeholder; the editor leaves its events alone. */
export const REMOTE_IMAGE_PLACEHOLDER_ATTRIBUTE = 'data-remote-image-placeholder';

type Placeholder = { src: string; holder: HTMLElement; component: ReturnType<typeof mount> };

const SELECTOR = `img[${REMOTE_IMAGE_ATTRIBUTE}]`;

/**
 * Gives every image waiting under `root` (see `$lib/security/remote-images`) a
 * placeholder naming its host, with the controls that load it. Used once, on
 * the document body, so each surface that renders sanitized content or the
 * editor is covered without code of its own; content rendered where this does
 * not run simply shows no remote image.
 */
export function remoteImages(root: HTMLElement) {
	const origin = window.location.origin;
	const placeholders = new Map<HTMLImageElement, Placeholder>();

	function release(image: HTMLImageElement) {
		const placeholder = placeholders.get(image);
		if (!placeholder) return;
		placeholders.delete(image);
		void unmount(placeholder.component);
		placeholder.holder.remove();
	}

	function load(image: HTMLImageElement, src: string) {
		release(image);
		image.removeAttribute(REMOTE_IMAGE_ATTRIBUTE);
		image.setAttribute('src', src);
	}

	function refresh(image: HTMLImageElement) {
		const src = image.getAttribute(REMOTE_IMAGE_ATTRIBUTE);
		if (!src || !image.isConnected) return release(image);
		const host = remoteImageHost(src, origin);
		// Never the case for what the sanitizer wrote; leave such an image unloaded.
		if (!host) return release(image);
		if (isRemoteImageAllowed(src, origin)) return load(image, src);
		if (placeholders.get(image)?.src === src) return;
		release(image);

		const holder = document.createElement('span');
		holder.setAttribute(REMOTE_IMAGE_PLACEHOLDER_ATTRIBUTE, '');
		holder.contentEditable = 'false';
		image.after(holder);
		const component = mount(RemoteImagePlaceholder, {
			target: holder,
			props: {
				host,
				onload: () => allowRemoteImage(src),
				onalways: () => trustRemoteImageHost(host)
			}
		});
		placeholders.set(image, { src, holder, component });
	}

	function refreshWithin(node: Node) {
		if (!(node instanceof Element)) return;
		if (node instanceof HTMLImageElement) return refresh(node);
		for (const image of node.querySelectorAll<HTMLImageElement>(SELECTOR)) refresh(image);
	}

	const observer = new MutationObserver((records) => {
		for (const record of records) {
			if (record.type === 'attributes') refreshWithin(record.target);
			else for (const node of record.addedNodes) refreshWithin(node);
		}
		// Content that was replaced takes its placeholders with it.
		for (const image of [...placeholders.keys()]) if (!image.isConnected) release(image);
	});
	observer.observe(root, {
		subtree: true,
		childList: true,
		attributes: true,
		attributeFilter: [REMOTE_IMAGE_ATTRIBUTE]
	});
	// A choice applies to every waiting copy of that image, or image of that host.
	const stopListening = onRemoteImageChoice(() => {
		for (const image of [...placeholders.keys()]) refresh(image);
	});
	refreshWithin(root);

	return {
		destroy() {
			observer.disconnect();
			stopListening();
			for (const image of [...placeholders.keys()]) release(image);
		}
	};
}
