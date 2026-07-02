import type { Action } from 'svelte/action';

type RevealOptions = {
	/** Delay in ms before the element animates in once visible. */
	delay?: number;
	/** IntersectionObserver threshold. */
	threshold?: number;
};

/**
 * Scroll-reveal action: fades/slides the element in the first time it
 * enters the viewport. Respects prefers-reduced-motion via CSS.
 */
export const reveal: Action<HTMLElement, RevealOptions | undefined> = (node, options) => {
	const { delay = 0, threshold = 0.15 } = options ?? {};

	node.classList.add('reveal-init');
	node.style.setProperty('--reveal-delay', `${delay}ms`);

	const observer = new IntersectionObserver(
		(entries) => {
			for (const entry of entries) {
				if (entry.isIntersecting) {
					node.classList.add('reveal-in');
					observer.disconnect();
				}
			}
		},
		{ threshold }
	);

	observer.observe(node);

	return {
		destroy() {
			observer.disconnect();
		}
	};
};
