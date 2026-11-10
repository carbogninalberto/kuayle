import { goto } from '$app/navigation';
import { mount, unmount } from 'svelte';
import type { Issue } from '$lib/types/issue';
import type { WorkspaceMember } from '$lib/types/workspace';
import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
import MentionHoverCard from './MentionHoverCard.svelte';
import WikiReferenceBody from '$lib/features/wiki/WikiReferenceBody.svelte';
import { wikiReferences } from '$lib/features/wiki/wiki-reference-store';
import { wikiReferenceHref, type WikiReference } from '$lib/features/wiki/wiki-references';
import { m } from '$lib/features/i18n/messages';

export interface MentionInteractivityOptions {
	slug: string;
	members?: WorkspaceMember[];
	issues?: Issue[];
	onIssueClick?: (identifier: string) => void;
}

type MountedComponent = ReturnType<typeof mount>;

export function mentionInteractivity(node: HTMLElement, options: MentionInteractivityOptions) {
	let currentOptions = options;
	let hoverCard: MountedComponent | null = null;
	let hoverTimer: ReturnType<typeof setTimeout> | null = null;
	const statusIcons = new Map<HTMLElement, MountedComponent>();
	// Wiki page chips: what each one shows, and the reference once resolved.
	const wikiBodies = new Map<HTMLElement, MountedComponent>();
	const wikiResolved = new WeakMap<HTMLElement, WikiReference>();

	// A wiki chip holds only a page ID. It shows a neutral chip until the
	// server says what this reader may see of the page: its title and scope,
	// or nothing, which renders as the inaccessible placeholder.
	function renderWiki(mention: HTMLElement, reference: WikiReference | null) {
		const previous = wikiBodies.get(mention);
		if (previous) void unmount(previous);
		mention.replaceChildren();
		wikiBodies.set(mention, mount(WikiReferenceBody, { target: mention, props: { reference } }));
		mention.classList.toggle('mention-unavailable', !!reference && !reference.accessible);
		if (!reference) {
			mention.removeAttribute('role');
			mention.setAttribute('aria-busy', 'true');
			return;
		}
		mention.removeAttribute('aria-busy');
		if (reference.accessible) {
			mention.setAttribute('role', 'link');
			mention.setAttribute('aria-label', m['wiki.reference.open']({ title: reference.title ?? '' }));
		} else {
			mention.setAttribute('role', 'note');
			mention.setAttribute('aria-label', m['wiki.reference.unavailable_hint']());
		}
	}

	function enhanceWiki(mention: HTMLElement) {
		const id = mention.dataset.id ?? '';
		// A chip that was already resolved keeps its answer while it is checked again.
		const known = wikiResolved.get(mention);
		renderWiki(mention, known?.id === id.toLowerCase() ? known : null);
		void wikiReferences.resolve(currentOptions.slug, id).then((reference) => {
			if (!wikiBodies.has(mention) || mention.dataset.id !== id) return;
			wikiResolved.set(mention, reference);
			observer.disconnect();
			renderWiki(mention, reference);
			observer.observe(node, { childList: true, subtree: true });
		});
	}

	function issueFor(mention: HTMLElement): Issue | undefined {
		const id = mention.dataset.id;
		const identifier = issueIdentifier(mention);
		return currentOptions.issues?.find((issue) => issue.id === id || issue.identifier === identifier);
	}

	function issueIdentifier(mention: HTMLElement): string {
		return mention.dataset.identifier || issueForLabel(mention.dataset.label || '');
	}

	function enhanceMention(mention: HTMLElement) {
		if (mention.dataset.mentionEnhanced === 'true') return;
		mention.dataset.mentionEnhanced = 'true';
		mention.tabIndex = 0;

		if (mention.dataset.kind === 'wiki') {
			enhanceWiki(mention);
		} else if (mention.dataset.kind === 'issue') {
			const issue = issueFor(mention);
			const identifier = issue?.identifier || issueIdentifier(mention);
			mention.setAttribute('role', 'link');
			mention.setAttribute('aria-label', `Open issue ${identifier}`);
			const iconTarget = document.createElement('span');
			iconTarget.className = 'mention-status-icon';
			iconTarget.setAttribute('aria-hidden', 'true');
			mention.prepend(iconTarget);
			statusIcons.set(mention, mount(IssueStatusIcon, {
				target: iconTarget,
				props: {
					status: issue?.status,
					category: issue?.status_info?.category,
					color: issue?.status_info?.color,
					size: 12
				}
			}));
		} else {
			mention.setAttribute('role', 'button');
			mention.setAttribute('aria-label', `View user ${mention.dataset.label || ''}`);
		}
	}

	function enhanceAll() {
		node.querySelectorAll<HTMLElement>('[data-type="mention"]').forEach(enhanceMention);
	}

	function clearEnhancements() {
		for (const [mention, component] of statusIcons) {
			void unmount(component);
			mention.querySelector(':scope > .mention-status-icon')?.remove();
		}
		statusIcons.clear();
		for (const [mention, component] of wikiBodies) {
			void unmount(component);
			mention.replaceChildren();
			mention.classList.remove('mention-unavailable');
			mention.removeAttribute('aria-busy');
		}
		wikiBodies.clear();
		node.querySelectorAll<HTMLElement>('[data-mention-enhanced="true"]').forEach((mention) => {
			delete mention.dataset.mentionEnhanced;
			mention.removeAttribute('tabindex');
			mention.removeAttribute('role');
			mention.removeAttribute('aria-label');
		});
	}

	function mentionFrom(target: EventTarget | null): HTMLElement | null {
		const element = target instanceof Element ? target : null;
		const mention = element?.closest<HTMLElement>('[data-type="mention"]') ?? null;
		return mention && node.contains(mention) ? mention : null;
	}

	function clearHoverTimer() {
		if (!hoverTimer) return;
		clearTimeout(hoverTimer);
		hoverTimer = null;
	}

	function hideHoverCard() {
		clearHoverTimer();
		if (hoverCard) void unmount(hoverCard);
		hoverCard = null;
	}

	function showHoverCard(mention: HTMLElement) {
		hideHoverCard();
		if (mention.dataset.kind === 'wiki') {
			const reference = wikiResolved.get(mention);
			if (!reference) return;
			hoverCard = mount(MentionHoverCard, {
				target: document.body,
				props: { anchor: mention.getBoundingClientRect(), kind: 'wiki', label: '', reference }
			});
			return;
		}
		const isIssue = mention.dataset.kind === 'issue';
		const member = currentOptions.members?.find((candidate) => candidate.user_id === mention.dataset.id);
		hoverCard = mount(MentionHoverCard, {
			target: document.body,
			props: {
				anchor: mention.getBoundingClientRect(),
				kind: isIssue ? 'issue' : 'user',
				label: mention.dataset.label || '',
				member,
				issue: isIssue ? issueFor(mention) : undefined
			}
		});
	}

	function scheduleHoverCard(mention: HTMLElement) {
		clearHoverTimer();
		hoverTimer = setTimeout(() => showHoverCard(mention), 150);
	}

	function openMention(mention: HTMLElement) {
		if (mention.dataset.kind === 'wiki') {
			const reference = wikiResolved.get(mention);
			const href = reference && currentOptions.slug ? wikiReferenceHref(currentOptions.slug, reference) : null;
			if (href) {
				hideHoverCard();
				void goto(href);
			} else showHoverCard(mention);
			return;
		}
		if (mention.dataset.kind !== 'issue') {
			showHoverCard(mention);
			return;
		}
		const identifier = issueFor(mention)?.identifier || issueIdentifier(mention);
		if (!identifier || !currentOptions.slug) return;
		if (currentOptions.onIssueClick) currentOptions.onIssueClick(identifier);
		else void goto(`/${currentOptions.slug}/issue/${identifier}`);
	}

	function handlePointerOver(event: PointerEvent) {
		const mention = mentionFrom(event.target);
		if (!mention || isWithin(mention, event.relatedTarget)) return;
		scheduleHoverCard(mention);
	}

	function handlePointerOut(event: PointerEvent) {
		const mention = mentionFrom(event.target);
		if (!mention || isWithin(mention, event.relatedTarget)) return;
		hideHoverCard();
	}

	function handleClick(event: MouseEvent) {
		const mention = mentionFrom(event.target);
		if (!mention) return;
		event.preventDefault();
		openMention(mention);
	}

	function handleKeyDown(event: KeyboardEvent) {
		if (event.key !== 'Enter' && event.key !== ' ') return;
		const mention = mentionFrom(event.target);
		if (!mention) return;
		event.preventDefault();
		openMention(mention);
	}

	function handleFocusIn(event: FocusEvent) {
		const mention = mentionFrom(event.target);
		if (mention) showHoverCard(mention);
	}

	function handleFocusOut(event: FocusEvent) {
		const mention = mentionFrom(event.target);
		if (mention && !isWithin(mention, event.relatedTarget)) hideHoverCard();
	}

	const observer = new MutationObserver(enhanceAll);
	observer.observe(node, { childList: true, subtree: true });
	enhanceAll();
	node.addEventListener('pointerover', handlePointerOver);
	node.addEventListener('pointerout', handlePointerOut);
	node.addEventListener('click', handleClick);
	node.addEventListener('keydown', handleKeyDown);
	node.addEventListener('focusin', handleFocusIn);
	node.addEventListener('focusout', handleFocusOut);

	return {
		update(newOptions: MentionInteractivityOptions) {
			currentOptions = newOptions;
			observer.disconnect();
			clearEnhancements();
			enhanceAll();
			observer.observe(node, { childList: true, subtree: true });
		},
		destroy() {
			observer.disconnect();
			node.removeEventListener('pointerover', handlePointerOver);
			node.removeEventListener('pointerout', handlePointerOut);
			node.removeEventListener('click', handleClick);
			node.removeEventListener('keydown', handleKeyDown);
			node.removeEventListener('focusin', handleFocusIn);
			node.removeEventListener('focusout', handleFocusOut);
			hideHoverCard();
			clearEnhancements();
		}
	};
}

function issueForLabel(label: string): string {
	const [identifier = ''] = label.trim().split(/\s+/, 1);
	return identifier;
}

function isWithin(element: HTMLElement, target: EventTarget | null): boolean {
	return target instanceof Node && element.contains(target);
}
