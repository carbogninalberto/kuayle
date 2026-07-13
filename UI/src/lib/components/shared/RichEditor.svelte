<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { Editor } from 'svelte-tiptap';
	import { EditorContent, BubbleMenu } from 'svelte-tiptap';
	import StarterKit from '@tiptap/starter-kit';
	import Placeholder from '@tiptap/extension-placeholder';
	import TaskList from '@tiptap/extension-task-list';
	import TaskItem from '@tiptap/extension-task-item';
	import Link from '@tiptap/extension-link';
	import CodeBlockLowlight from '@tiptap/extension-code-block-lowlight';
	import Image from '@tiptap/extension-image';
	import Underline from '@tiptap/extension-underline';
	import { Extension, InputRule } from '@tiptap/core';
	import { Plugin, PluginKey } from 'prosemirror-state';
	import { Decoration, DecorationSet } from 'prosemirror-view';
	import { common, createLowlight } from 'lowlight';
	import { Button } from '$lib/components/ui/button';
	import { Separator } from '$lib/components/ui/separator';
	import {
		Bold,
		Italic,
		Strikethrough,
		Underline as UnderlineIcon,
		Code,
		Heading1,
		Heading2,
		List,
		ListOrdered,
		ListChecks,
		Link as LinkIcon,
		Quote,
		Code2,
		Undo2,
		Redo2,
		SquareArrowOutUpRight,
		Sparkles,
		ImagePlus,
		Paperclip
	} from 'lucide-svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { sanitizeEditorOutput } from '$lib/security/sanitize';
	import { createSlashCommandExtension } from './slash-command/slash-command.extension';
	import { filterSlashItems, flatFilteredItems, type SlashMenuItem } from './slash-command/slash-items';
	import SlashCommandMenu from './slash-command/SlashCommandMenu.svelte';
	import { MentionNode, createMentionPlugin, type MentionItem } from './mention/mention.extension';
	import MentionList from './mention/MentionList.svelte';
	import { mentionInteractivity } from './mention/mention-interactivity.action';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Issue } from '$lib/types/issue';
	import { Attachment } from './attachment.extension';

	let {
		content = '',
		placeholder = 'Write something...',
		editable = true,
		minimal = false,
		compact = false,
		bubbleMenu = false,
		borderless = false,
		minHeight,
		onupdate,
		onsubmit,
		uploadUrl,
		workspaceSlug = '',
		members = [],
		issues = [],
		remoteCursors,
		onfocus: onFocusProp,
		onblur: onBlurProp,
		oncursorchange,
		oncreateissue,
		onreworkselection
	}: {
		content?: string;
		placeholder?: string;
		editable?: boolean;
		minimal?: boolean;
		compact?: boolean;
		bubbleMenu?: boolean;
		borderless?: boolean;
		minHeight?: string;
		onupdate?: (html: string) => void;
		onsubmit?: () => void;
		uploadUrl?: string;
		workspaceSlug?: string;
		members?: WorkspaceMember[];
		issues?: Issue[];
		remoteCursors?: Array<{ name: string; color: string; position: number; anchor?: number }>;
		onfocus?: () => void;
		onblur?: () => void;
		oncursorchange?: (position: number, anchor: number) => void;
		oncreateissue?: (selectedText: string) => void;
		onreworkselection?: (selectedText: string) => Promise<string>;
	} = $props();

	let editor = $state<Editor | null>(null);
	let isFocused = $state(false);
	let linkInputVisible = $state(false);
	let linkUrl = $state('');
	let cursorElements: HTMLElement[] = [];

	// Slash command state
	let slashActive = $state(false);
	let slashQuery = $state('');
	let slashPosition = $state({ x: 0, y: 0 });
	let slashSelectedIndex = $state(0);
	let slashRange = $state<{ from: number; to: number } | null>(null);

	const slashFilteredGroups = $derived(filterSlashItems(slashQuery));
	const slashFlatItems = $derived(flatFilteredItems(slashQuery));

	// Mention state
	let mentionActive = $state(false);
	let mentionQuery = $state('');
	let mentionPosition = $state({ x: 0, y: 0 });
	let mentionSelectedIndex = $state(0);
	let mentionRange = $state<{ from: number; to: number } | null>(null);
	let reworkingSelection = $state(false);
	let rewriteJustApplied = $state(false);
	let lastSelection = $state<{ from: number; to: number; text: string } | null>(null);
	let rewriteAnimationTimer: ReturnType<typeof setTimeout> | null = null;

	const mentionFilteredItems: MentionItem[] = $derived.by(() => {
		const q = mentionQuery.toLowerCase();
		const userItems = members.map((m) => ({ kind: 'user' as const, id: m.user_id, name: m.name, email: m.email }));
		const issueItems = issues.map((i) => ({
			kind: 'issue' as const,
			id: i.id,
			identifier: i.identifier,
			title: i.title,
			status: i.status,
			status_category: i.status_info?.category,
			status_color: i.status_info?.color ?? null
		}));

		if (!q) return [...userItems.slice(0, 6), ...issueItems.slice(0, 4)];

		const filteredUsers = userItems.filter(
			(u) => u.name.toLowerCase().includes(q) || u.email.toLowerCase().includes(q)
		).slice(0, 6);
		const filteredIssues = issueItems.filter(
			(i) => i.identifier.toLowerCase().includes(q) || i.title.toLowerCase().includes(q)
		).slice(0, 4);

		return [...filteredUsers, ...filteredIssues];
	});

	const lowlight = createLowlight(common);

	const TaskListShortcut = Extension.create({
		name: 'taskListShortcut',
		addInputRules() {
			return [
				new InputRule({
					find: /^\s*\[([ xX])\]\s$/,
					handler: ({ state, range, chain }) => {
						chain().deleteRange(range).toggleTaskList().run();
					},
				}),
			];
		},
	});

	const editorClass = $derived(compact
		? 'prose prose-invert prose-sm max-w-none outline-none text-[var(--color-text-primary)] compact-editor'
		: borderless
			? 'prose prose-invert prose-sm max-w-none outline-none text-[var(--color-text-primary)] borderless-editor'
			: 'prose prose-invert prose-sm max-w-none outline-none min-h-[80px] px-3 py-2 text-[var(--color-text-primary)]');

	type UploadedFile = { url: string; filename: string; size: number; contentType: string };
	type UploadPlaceholder = { id: string; pos: number; label: string; side: number };
	type UploadPlaceholderMeta = { add?: UploadPlaceholder[]; remove?: string[] };

	const uploadPlaceholderKey = new PluginKey<DecorationSet>('uploadPlaceholder');
	let uploadPlaceholderSequence = 0;

	const ResizableImage = Image.extend({
		addAttributes() {
			return {
				...this.parent?.(),
				width: {
					default: null,
					parseHTML: (element) => element.getAttribute('width'),
					renderHTML: (attributes) => attributes.width ? { width: attributes.width } : {}
				}
			};
		},
		addNodeView() {
			return ({ node: initialNode, editor: nodeEditor, getPos }) => {
				let currentNode = initialNode;
				let pendingWidth: string | null = null;
				let resizing = false;
				let previousBodyCursor = '';
				let previousBodyUserSelect = '';

				const dom = document.createElement('span');
				dom.className = 'resizable-image-wrapper';
				dom.setAttribute('data-drag-handle', '');

				const image = document.createElement('img');
				image.draggable = false;

				const handle = document.createElement('button');
				handle.type = 'button';
				handle.className = 'image-resize-handle';
				handle.contentEditable = 'false';
				handle.setAttribute('aria-label', 'Resize image');
				handle.title = 'Drag to resize. Use arrow keys for 5% steps.';

				function currentPercent(): number {
					const stored = Number.parseFloat(String(currentNode.attrs.width ?? ''));
					if (Number.isFinite(stored)) return stored;
					const containerWidth = dom.parentElement?.getBoundingClientRect().width ?? 0;
					if (!containerWidth) return 100;
					return Math.round((dom.getBoundingClientRect().width / containerWidth) * 100);
				}

				function syncDOM() {
					for (const attribute of ['src', 'alt', 'title']) {
						const value = currentNode.attrs[attribute];
						if (value) image.setAttribute(attribute, String(value));
						else image.removeAttribute(attribute);
					}
					const width = currentNode.attrs.width ? String(currentNode.attrs.width) : null;
					if (width) {
						dom.style.width = width;
						image.setAttribute('width', width);
						image.style.width = '100%';
					} else {
						dom.style.removeProperty('width');
						image.removeAttribute('width');
						image.style.width = 'auto';
					}
					handle.setAttribute('aria-valuenow', String(Math.round(currentPercent())));
				}

				function commitWidth(width: string | null) {
					const position = getPos();
					if (typeof position !== 'number') return;
					nodeEditor.view.dispatch(
						nodeEditor.view.state.tr.setNodeMarkup(position, undefined, {
							...currentNode.attrs,
							width
						})
					);
				}

				function finishResize(commit: boolean) {
					if (!resizing) return;
					resizing = false;
					window.removeEventListener('pointermove', handlePointerMove);
					window.removeEventListener('pointerup', handlePointerUp);
					window.removeEventListener('pointercancel', handlePointerCancel);
					document.body.style.cursor = previousBodyCursor;
					document.body.style.userSelect = previousBodyUserSelect;
					dom.classList.remove('is-resizing');
					if (commit && pendingWidth) commitWidth(pendingWidth);
					else syncDOM();
					pendingWidth = null;
				}

				let dragStartX = 0;
				let dragStartWidth = 0;
				let dragContainerWidth = 0;

				function handlePointerMove(event: PointerEvent) {
					if (!resizing || !dragContainerWidth) return;
					const widthInPixels = Math.max(
						dragContainerWidth * 0.1,
						Math.min(dragContainerWidth, dragStartWidth + event.clientX - dragStartX)
					);
					const percent = Math.max(10, Math.min(100, Math.round((widthInPixels / dragContainerWidth) * 100)));
					pendingWidth = `${percent}%`;
					dom.style.width = pendingWidth;
					image.style.width = '100%';
					handle.setAttribute('aria-valuenow', String(percent));
				}

				function handlePointerUp() {
					finishResize(true);
				}

				function handlePointerCancel() {
					finishResize(false);
				}

				handle.addEventListener('pointerdown', (event) => {
					if (event.button !== 0 || !nodeEditor.isEditable) return;
					event.preventDefault();
					event.stopPropagation();
					dragContainerWidth = dom.parentElement?.getBoundingClientRect().width ?? 0;
					dragStartWidth = dom.getBoundingClientRect().width;
					if (!dragContainerWidth || !dragStartWidth) return;
					dragStartX = event.clientX;
					pendingWidth = null;
					resizing = true;
					previousBodyCursor = document.body.style.cursor;
					previousBodyUserSelect = document.body.style.userSelect;
					document.body.style.cursor = 'nwse-resize';
					document.body.style.userSelect = 'none';
					dom.classList.add('is-resizing');
					window.addEventListener('pointermove', handlePointerMove);
					window.addEventListener('pointerup', handlePointerUp);
					window.addEventListener('pointercancel', handlePointerCancel);
				});

				handle.addEventListener('keydown', (event) => {
					if (!nodeEditor.isEditable) return;
					let percent = currentPercent();
					const step = event.shiftKey ? 10 : 5;
					if (event.key === 'ArrowLeft' || event.key === 'ArrowDown') percent -= step;
					else if (event.key === 'ArrowRight' || event.key === 'ArrowUp') percent += step;
					else if (event.key === 'Home') percent = 10;
					else if (event.key === 'End') percent = 100;
					else return;
					event.preventDefault();
					event.stopPropagation();
					commitWidth(`${Math.max(10, Math.min(100, Math.round(percent)))}%`);
				});

				dom.append(image, handle);
				syncDOM();

				return {
					dom,
					update(updatedNode) {
						if (updatedNode.type !== currentNode.type) return false;
						currentNode = updatedNode;
						if (!resizing) syncDOM();
						return true;
					},
					selectNode() {
						dom.classList.add('ProseMirror-selectednode');
					},
					deselectNode() {
						dom.classList.remove('ProseMirror-selectednode');
					},
					stopEvent(event) {
						return event.target === handle;
					},
					ignoreMutation: () => true,
					destroy() {
						finishResize(false);
					}
				};
			};
		}
	});

	const UploadPlaceholderExtension = Extension.create({
		name: 'uploadPlaceholder',
		addProseMirrorPlugins() {
			return [
				new Plugin<DecorationSet>({
					key: uploadPlaceholderKey,
					state: {
						init: () => DecorationSet.empty,
						apply(transaction, decorations) {
							let next = decorations.map(transaction.mapping, transaction.doc);
							const meta = transaction.getMeta(uploadPlaceholderKey) as UploadPlaceholderMeta | undefined;
							if (meta?.add?.length) {
								const widgets = meta.add.map((placeholder) => {
									const element = document.createElement('span');
									element.className = 'editor-upload-placeholder';
									element.textContent = `Uploading ${placeholder.label}...`;
									return Decoration.widget(placeholder.pos, element, {
										id: placeholder.id,
										side: placeholder.side
									});
								});
								next = next.add(transaction.doc, widgets);
							}
							if (meta?.remove?.length) {
								const ids = new Set(meta.remove);
								next = next.remove(next.find(undefined, undefined, (spec) => ids.has(spec.id)));
							}
							return next;
						}
					},
					props: {
						decorations(state) {
							return uploadPlaceholderKey.getState(state);
						}
					}
				})
			];
		}
	});

	async function uploadFile(file: File): Promise<UploadedFile | null> {
		if (!uploadUrl) return null;
		const form = new FormData();
		form.append('file', file);
		try {
			const res = await fetch(uploadUrl, { method: 'POST', body: form, credentials: 'include' });
			if (!res.ok) {
				const data = await res.json().catch(() => null);
				appToast.error(data?.error?.message ?? data?.message ?? `Failed to upload ${file.name}`);
				return null;
			}
			const data = await res.json();
			const result = data.data ?? data;
			if (!result.url) return null;
			return {
				url: result.url,
				filename: result.filename ?? file.name,
				size: file.size,
				contentType: result.content_type ?? file.type
			};
		} catch {
			appToast.error(`Failed to upload ${file.name}`);
			return null;
		}
	}

	function reserveUploadPlaceholders(files: File[], position?: number): UploadPlaceholder[] {
		if (!editor || editor.isDestroyed) return [];
		const docSize = editor.state.doc.content.size;
		const pos = Math.max(0, Math.min(position ?? editor.state.selection.from, docSize));
		const placeholders = files.map((file, index) => ({
			id: `upload-${Date.now()}-${uploadPlaceholderSequence++}`,
			pos,
			label: file.name,
			side: index + 1
		}));
		editor.view.dispatch(editor.state.tr.setMeta(uploadPlaceholderKey, { add: placeholders }));
		return placeholders;
	}

	function removeUploadPlaceholder(id: string) {
		if (!editor || editor.isDestroyed) return;
		editor.view.dispatch(editor.state.tr.setMeta(uploadPlaceholderKey, { remove: [id] }));
	}

	async function uploadAndInsert(file: File, placeholder: UploadPlaceholder) {
		const uploaded = await uploadFile(file);
		if (!uploaded || !editor || editor.isDestroyed) {
			removeUploadPlaceholder(placeholder.id);
			return;
		}
		const decoration = uploadPlaceholderKey
			.getState(editor.state)
			?.find(undefined, undefined, (spec) => spec.id === placeholder.id)[0];
		if (!decoration) return;
		const content = uploaded.contentType.startsWith('image/')
			? { type: 'image', attrs: { src: uploaded.url, alt: uploaded.filename } }
			: {
				type: 'attachment',
				attrs: {
					href: `${uploaded.url}?download=1`,
					filename: uploaded.filename,
					size: uploaded.size
				}
			};
		editor.commands.insertContentAt(decoration.from, content, { updateSelection: false });
		removeUploadPlaceholder(placeholder.id);
	}

	async function uploadFiles(files: File[], position?: number) {
		const placeholders = reserveUploadPlaceholders(files, position);
		for (const [index, file] of files.entries()) {
			const placeholder = placeholders[index];
			if (placeholder) await uploadAndInsert(file, placeholder);
		}
	}

	function chooseFiles(imagesOnly = false) {
		if (!uploadUrl) return;
		const input = document.createElement('input');
		input.type = 'file';
		input.multiple = !imagesOnly;
		if (imagesOnly) input.accept = 'image/*';
		input.onchange = () => {
			void uploadFiles(Array.from(input.files ?? []));
		};
		input.click();
	}

	function createImagePasteHandler() {
		if (!uploadUrl) return null;
		return Extension.create({
			name: 'imagePaste',
			addProseMirrorPlugins() {
				return [
					new Plugin({
						props: {
							handlePaste(_view: any, event: ClipboardEvent) {
								const items = event.clipboardData?.items;
								if (!items) return false;
								for (const item of items) {
									if (item.kind === 'file') {
										event.preventDefault();
										const file = item.getAsFile();
										if (file) void uploadFiles([file]);
										return true;
									}
								}
								return false;
							},
							handleDrop(view: any, event: DragEvent) {
								const files = event.dataTransfer?.files;
								if (!files || files.length === 0) return false;
								event.preventDefault();
								const position = view.posAtCoords({ left: event.clientX, top: event.clientY })?.pos;
								void uploadFiles(Array.from(files), position);
								return true;
							}
						}
					})
				];
			}
		});
	}

	onMount(() => {
		const SubmitShortcut = onsubmit ? Extension.create({
			name: 'submitShortcut',
			addKeyboardShortcuts() {
				return {
					'Mod-Enter': () => {
						onsubmit?.();
						return true;
					}
				};
			}
		}) : null;

		const imagePasteExt = createImagePasteHandler();

		const mentionPlugin = createMentionPlugin({
			onStateChange(state) {
				mentionActive = state.active;
				mentionQuery = state.query;
				mentionPosition = { x: state.x, y: state.y };
				mentionRange = state.range;
				if (state.active) mentionSelectedIndex = 0;
			},
			onNavigate(direction) {
				const total = mentionFilteredItems.length;
				if (total === 0) return;
				if (direction === 'down') {
					mentionSelectedIndex = (mentionSelectedIndex + 1) % total;
				} else {
					mentionSelectedIndex = (mentionSelectedIndex - 1 + total) % total;
				}
			},
			onSelect() {
				handleMentionSelect(mentionFilteredItems[mentionSelectedIndex]);
			}
		});

		const slashCommandExt = createSlashCommandExtension({
			onStateChange(state) {
				slashActive = state.active;
				slashQuery = state.query;
				slashPosition = { x: state.x, y: state.y };
				slashRange = state.range;
				if (state.active) slashSelectedIndex = 0;
			},
			onNavigate(direction) {
				const total = slashFlatItems.length;
				if (total === 0) return;
				if (direction === 'down') {
					slashSelectedIndex = (slashSelectedIndex + 1) % total;
				} else {
					slashSelectedIndex = (slashSelectedIndex - 1 + total) % total;
				}
			},
			onSelect() {
				handleSlashSelect(slashFlatItems[slashSelectedIndex]);
			}
		});

		const extensions = [
			StarterKit.configure({
				codeBlock: false,
				link: false,
				underline: false,
			}),
			Placeholder.configure({ placeholder }),
			TaskList,
			TaskItem.configure({ nested: true }),
			Link.configure({
				openOnClick: false,
				HTMLAttributes: { class: 'text-[var(--app-accent-light)] underline' }
			}),
			CodeBlockLowlight.configure({ lowlight }),
			ResizableImage.configure({ inline: true, allowBase64: false }),
			Attachment,
			UploadPlaceholderExtension,
			Underline,
			MentionNode,
			TaskListShortcut,
			slashCommandExt,
			Extension.create({
				name: 'mentionPlugin',
				addProseMirrorPlugins() {
					return [mentionPlugin];
				}
			}),
			...(SubmitShortcut ? [SubmitShortcut] : []),
			...(imagePasteExt ? [imagePasteExt] : []),
		];

		editor = new Editor({
			extensions,
			content,
			editable,
			onUpdate: ({ editor: e }) => {
				onupdate?.(sanitizeEditorOutput(e.getHTML()));
			},
			onSelectionUpdate: ({ editor: e }) => {
				const { from, to, head, anchor } = e.state.selection;
				const selectedText = e.state.doc.textBetween(from, to, ' ').trim();
				lastSelection = selectedText ? { from, to, text: selectedText } : null;
				oncursorchange?.(head, anchor);
			},
			onFocus: () => { isFocused = true; onFocusProp?.(); },
			onBlur: () => { isFocused = false; onBlurProp?.(); },
			editorProps: {
				attributes: {
					class: editorClass
				}
			}
		});

	});

	// Render remote cursors as widget decorations in the editor
	$effect(() => {
		if (!editor || !remoteCursors || remoteCursors.length === 0) {
			// Clear cursors
			if (cursorElements.length > 0) {
				cursorElements.forEach(el => el.remove());
				cursorElements = [];
			}
			return;
		}

		// Remove old cursor elements
		cursorElements.forEach(el => el.remove());
		cursorElements = [];

		const view = editor.view;
		const docSize = view.state.doc.content.size;

		// Use the .rich-editor wrapper (position: relative) as the positioning reference
		const container = view.dom.closest('.rich-editor') as HTMLElement | null;
		if (!container) return;
		const containerRect = container.getBoundingClientRect();

		for (const rc of remoteCursors) {
			const headPos = Math.max(0, Math.min(rc.position, docSize));
			const hasSelection = rc.anchor !== undefined && rc.anchor !== rc.position;

			try {
				// Render selection highlight if there's a range
				if (hasSelection) {
					const anchorPos = Math.max(0, Math.min(rc.anchor!, docSize));
					const from = Math.min(headPos, anchorPos);
					const to = Math.max(headPos, anchorPos);

					// Use a native DOM Range to get per-visual-line rectangles
					try {
						const domFrom = view.domAtPos(from);
						const domTo = view.domAtPos(to);
						const range = document.createRange();
						range.setStart(domFrom.node, domFrom.offset);
						range.setEnd(domTo.node, domTo.offset);

						const rects = range.getClientRects();
						for (const rect of rects) {
							if (rect.width === 0 && rect.height === 0) continue;
							const highlight = document.createElement('div');
							highlight.className = 'remote-cursor-selection';
							highlight.style.cssText = `
								position: absolute;
								left: ${rect.left - containerRect.left}px;
								top: ${rect.top - containerRect.top}px;
								width: ${rect.width}px;
								height: ${rect.height}px;
								background: ${rc.color}20;
								border-radius: 2px;
								pointer-events: none;
								z-index: 40;
							`;
							container.appendChild(highlight);
							cursorElements.push(highlight);
						}
					} catch {
						// DOM range creation failed, skip selection highlight
					}
				}

				// Always render the cursor line at head position
				const coords = view.coordsAtPos(headPos);
				const cursor = document.createElement('div');
				cursor.className = 'remote-cursor-widget';
				cursor.style.cssText = `
					position: absolute;
					left: ${coords.left - containerRect.left}px;
					top: ${coords.top - containerRect.top}px;
					height: ${coords.bottom - coords.top}px;
					border-left: 2px solid ${rc.color};
					pointer-events: none;
					z-index: 50;
				`;

				const label = document.createElement('div');
				label.className = 'remote-cursor-label';
				label.style.cssText = `
					position: absolute;
					bottom: -16px;
					left: -1px;
					background: ${rc.color};
					color: white;
					font-size: 10px;
					font-weight: 600;
					padding: 1px 4px;
					border-radius: 0 3px 3px 3px;
					white-space: nowrap;
					line-height: 14px;
				`;
				label.textContent = rc.name;
				cursor.appendChild(label);

				container.appendChild(cursor);
				cursorElements.push(cursor);
			} catch {
				// Position out of range, skip
			}
		}

		return () => {
			cursorElements.forEach(el => el.remove());
			cursorElements = [];
		};
	});

	// Sync content from outside (e.g. real-time updates) without losing cursor.
	// Only sync when content prop has a non-empty value (skip for comment editors
	// where content="" is just the initial value, not an ongoing binding).
	$effect(() => {
		if (editor && !isFocused && content) {
			const current = sanitizeEditorOutput(editor.getHTML());
			if (current !== content) {
				editor.commands.setContent(content, { emitUpdate: false });
			}
		}
	});

	// Handle slash command image upload
	function handleSlashUpload(e: Event) {
		const { file, editor: targetEditor } = (e as CustomEvent).detail;
		if (targetEditor !== editor) return;
		void uploadFiles([file]);
	}

	function handleSlashFileUpload(e: Event) {
		const { files, editor: targetEditor } = (e as CustomEvent).detail;
		if (targetEditor !== editor) return;
		void uploadFiles(files as File[]);
	}

	onMount(() => {
		window.addEventListener('slash:upload-image', handleSlashUpload);
		window.addEventListener('slash:upload-files', handleSlashFileUpload);
		return () => {
			window.removeEventListener('slash:upload-image', handleSlashUpload);
			window.removeEventListener('slash:upload-files', handleSlashFileUpload);
		};
	});

	onDestroy(() => {
		cursorElements.forEach(el => el.remove());
		if (rewriteAnimationTimer) clearTimeout(rewriteAnimationTimer);
		editor?.destroy();
	});

	function toggleBold() { editor?.chain().focus().toggleBold().run(); }
	function toggleItalic() { editor?.chain().focus().toggleItalic().run(); }
	function toggleStrike() { editor?.chain().focus().toggleStrike().run(); }
	function toggleUnderline() { editor?.chain().focus().toggleUnderline().run(); }
	function toggleCode() { editor?.chain().focus().toggleCode().run(); }
	function toggleH1() { editor?.chain().focus().toggleHeading({ level: 1 }).run(); }
	function toggleH2() { editor?.chain().focus().toggleHeading({ level: 2 }).run(); }
	function toggleBulletList() { editor?.chain().focus().toggleBulletList().run(); }
	function toggleOrderedList() { editor?.chain().focus().toggleOrderedList().run(); }
	function toggleTaskList() { editor?.chain().focus().toggleTaskList().run(); }
	function toggleCodeBlock() { editor?.chain().focus().toggleCodeBlock().run(); }
	function toggleBlockquote() { editor?.chain().focus().toggleBlockquote().run(); }
	function undo() { editor?.chain().focus().undo().run(); }
	function redo() { editor?.chain().focus().redo().run(); }
	function setImageWidth(width: string | null) {
		editor?.chain().focus().updateAttributes('image', { width }).run();
	}

	function createIssueFromSelection() {
		if (!editor || !oncreateissue) return;
		const { from, to } = editor.state.selection;
		const selectedText = editor.state.doc.textBetween(from, to, ' ');
		if (selectedText.trim()) oncreateissue(selectedText.trim());
	}

	async function reworkSelection() {
		if (!editor || !onreworkselection || reworkingSelection) return;
		const { from, to } = editor.state.selection;
		const selectedText = editor.state.doc.textBetween(from, to, ' ').trim();
		const selection = selectedText ? { from, to, text: selectedText } : lastSelection;
		if (!selection) return;
		reworkingSelection = true;
		try {
			const replacement = await onreworkselection(selection.text);
			if (!replacement.trim()) return;
			editor.chain().focus().deleteRange({ from: selection.from, to: selection.to }).insertContent(replacement).run();
			rewriteJustApplied = true;
			if (rewriteAnimationTimer) clearTimeout(rewriteAnimationTimer);
			rewriteAnimationTimer = setTimeout(() => {
				rewriteJustApplied = false;
				rewriteAnimationTimer = null;
			}, 900);
		} finally {
			reworkingSelection = false;
		}
	}

	function handleSlashSelect(item: SlashMenuItem | undefined) {
		if (!item || !editor || !slashRange) return;
		// Delete the "/" + query text
		editor.chain().focus().deleteRange({ from: slashRange.from, to: slashRange.to }).run();
		// Execute the item action
		item.action(editor, { uploadUrl });
		slashActive = false;
		slashQuery = '';
		slashRange = null;
	}

	function handleMentionSelect(item: MentionItem | undefined) {
		if (!item || !editor || !mentionRange) return;
		const attrs = item.kind === 'user'
			? { id: item.id, label: item.name || item.email, kind: 'user' }
			: { id: item.id, label: `${item.identifier} ${item.title}`, kind: 'issue', identifier: item.identifier };
		editor.chain().focus()
			.deleteRange({ from: mentionRange.from, to: mentionRange.to })
			.insertContent({ type: 'mention', attrs })
			.insertContent(' ')
			.run();
		mentionActive = false;
	}

	function toggleLink() {
		if (!editor) return;
		if (editor.isActive('link')) {
			editor.chain().focus().unsetLink().run();
			linkInputVisible = false;
		} else {
			linkInputVisible = !linkInputVisible;
			linkUrl = '';
		}
	}

	function applyLink() {
		if (!editor || !linkUrl.trim()) return;
		const url = linkUrl.trim();
		if (!/^(https?:|mailto:)/i.test(url)) {
			linkUrl = '';
			return;
		}
		editor.chain().focus().extendMarkRange('link').setLink({ href: url }).run();
		linkInputVisible = false;
		linkUrl = '';
	}

	function cancelLink() {
		linkInputVisible = false;
		linkUrl = '';
		editor?.chain().focus().run();
	}

	function shouldShowBubble(props: { from: number; to: number; editor: any }): boolean {
		if (!props.editor.isFocused) return false;
		if (props.editor.isActive('image')) return true;
		if (props.from === props.to) return false;
		if (props.editor.isActive('codeBlock')) return false;
		return true;
	}

	function btnClass(active: boolean): string {
		return active
			? 'h-7 w-7 flex items-center justify-center rounded bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
			: 'h-7 w-7 flex items-center justify-center rounded text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)]';
	}

	// Show static toolbar: not bubbleMenu, not minimal
	let showStaticToolbar = $derived(editable && !minimal && !bubbleMenu);
</script>

<div class="w-full my-auto {borderless ? '' : 'rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)]'}">
	{#if showStaticToolbar}
		<!-- Toolbar -->
		<div class="flex items-center gap-0.5 border-b border-[var(--app-border)] px-2 py-1">
			<button type="button" onclick={toggleBold} class={btnClass(editor?.isActive('bold') ?? false)} title="Bold">
				<Bold size={14} />
			</button>
			<button type="button" onclick={toggleItalic} class={btnClass(editor?.isActive('italic') ?? false)} title="Italic">
				<Italic size={14} />
			</button>
			<button type="button" onclick={toggleStrike} class={btnClass(editor?.isActive('strike') ?? false)} title="Strikethrough">
				<Strikethrough size={14} />
			</button>
			<button type="button" onclick={toggleCode} class={btnClass(editor?.isActive('code') ?? false)} title="Inline code">
				<Code size={14} />
			</button>

			<Separator orientation="vertical" class="mx-1 h-4" />

			<button type="button" onclick={toggleH1} class={btnClass(editor?.isActive('heading', { level: 1 }) ?? false)} title="Heading 1">
				<Heading1 size={14} />
			</button>
			<button type="button" onclick={toggleH2} class={btnClass(editor?.isActive('heading', { level: 2 }) ?? false)} title="Heading 2">
				<Heading2 size={14} />
			</button>

			<Separator orientation="vertical" class="mx-1 h-4" />

			<button type="button" onclick={toggleBulletList} class={btnClass(editor?.isActive('bulletList') ?? false)} title="Bullet list">
				<List size={14} />
			</button>
			<button type="button" onclick={toggleOrderedList} class={btnClass(editor?.isActive('orderedList') ?? false)} title="Ordered list">
				<ListOrdered size={14} />
			</button>
			<button type="button" onclick={toggleTaskList} class={btnClass(editor?.isActive('taskList') ?? false)} title="Task list">
				<ListChecks size={14} />
			</button>

			<Separator orientation="vertical" class="mx-1 h-4" />

			<button type="button" onclick={toggleLink} class={btnClass(editor?.isActive('link') ?? false)} title="Link">
				<LinkIcon size={14} />
			</button>
			<button type="button" onclick={toggleCodeBlock} class={btnClass(editor?.isActive('codeBlock') ?? false)} title="Code block">
				<Code2 size={14} />
			</button>
			{#if uploadUrl}
				<button type="button" onclick={() => chooseFiles(true)} class={btnClass(false)} title="Upload image">
					<ImagePlus size={14} />
				</button>
				<button type="button" onclick={() => chooseFiles()} class={btnClass(false)} title="Attach files">
					<Paperclip size={14} />
				</button>
			{/if}

			<div class="flex-1"></div>

			<button type="button" onclick={undo} class={btnClass(false)} title="Undo">
				<Undo2 size={14} />
			</button>
			<button type="button" onclick={redo} class={btnClass(false)} title="Redo">
				<Redo2 size={14} />
			</button>
		</div>
	{/if}
	{#if editable && uploadUrl && bubbleMenu}
		<div class="flex items-center justify-end gap-0.5 px-1 py-0.5">
			<button type="button" onclick={() => chooseFiles(true)} class={btnClass(false)} title="Upload image" aria-label="Upload image">
				<ImagePlus size={14} />
			</button>
			<button type="button" onclick={() => chooseFiles()} class={btnClass(false)} title="Attach files" aria-label="Attach files">
				<Paperclip size={14} />
			</button>
		</div>
	{/if}

	<!-- Editor content -->
	{#if editor}
		<div
			class="rich-editor {reworkingSelection ? 'ai-rewrite-loading' : ''} {rewriteJustApplied ? 'ai-rewrite-applied' : ''}"
			style="position: relative; overflow: visible; {minHeight ? `--editor-min-height: ${minHeight}` : ''}"
			use:mentionInteractivity={{ slug: workspaceSlug, members, issues }}
		>
			<EditorContent {editor} />
			{#if reworkingSelection}
				<div class="ai-rewrite-overlay" aria-live="polite">
					<Sparkles size={14} class="ai-rewrite-spinner" />
					<span>Reworking selection...</span>
				</div>
			{/if}
		</div>
	{/if}

	<!-- Slash command menu -->
	{#if slashActive && editor && slashFlatItems.length > 0}
		<SlashCommandMenu
			groups={slashFilteredGroups}
			selectedIndex={slashSelectedIndex}
			position={slashPosition}
			onselect={handleSlashSelect}
			onclose={() => { slashActive = false; }}
		/>
	{/if}

	<!-- Mention menu -->
	{#if mentionActive && editor && (members.length > 0 || issues.length > 0)}
		<MentionList
			items={mentionFilteredItems}
			selectedIndex={mentionSelectedIndex}
			position={mentionPosition}
			onselect={handleMentionSelect}
			onclose={() => { mentionActive = false; }}
		/>
	{/if}

	{#if bubbleMenu && editor && editable}
		<BubbleMenu {editor} shouldShow={shouldShowBubble}>
			{#snippet children()}
				<div class="bubble-toolbar" role="toolbar" aria-label="Editor formatting" tabindex="-1" onpointerdown={(event) => event.preventDefault()}>
					{#if editor?.isActive('image')}
						<span class="bubble-image-label">Image size</span>
						{#each ['25%', '50%', '75%', '100%'] as width}
							<button type="button" onclick={() => setImageWidth(width)} class={btnClass(editor?.getAttributes('image').width === width)} title={`Set image width to ${width}`}>
								{width}
							</button>
						{/each}
						<button type="button" onclick={() => setImageWidth(null)} class={btnClass(!editor?.getAttributes('image').width)} title="Use original image size">Auto</button>
					{:else}
					<button type="button" onclick={toggleBold} class={btnClass(editor?.isActive('bold') ?? false)} title="Bold">
						<Bold size={14} />
					</button>
					<button type="button" onclick={toggleItalic} class={btnClass(editor?.isActive('italic') ?? false)} title="Italic">
						<Italic size={14} />
					</button>
					<button type="button" onclick={toggleStrike} class={btnClass(editor?.isActive('strike') ?? false)} title="Strikethrough">
						<Strikethrough size={14} />
					</button>
					<button type="button" onclick={toggleUnderline} class={btnClass(editor?.isActive('underline') ?? false)} title="Underline">
						<UnderlineIcon size={14} />
					</button>

					<div class="bubble-separator"></div>

					<button type="button" onclick={toggleLink} class={btnClass(editor?.isActive('link') ?? false)} title="Link">
						<LinkIcon size={14} />
					</button>
					<button type="button" onclick={toggleBlockquote} class={btnClass(editor?.isActive('blockquote') ?? false)} title="Blockquote">
						<Quote size={14} />
					</button>
					<button type="button" onclick={toggleCode} class={btnClass(editor?.isActive('code') ?? false)} title="Inline code">
						<Code size={14} />
					</button>
					<button type="button" onclick={toggleCodeBlock} class={btnClass(editor?.isActive('codeBlock') ?? false)} title="Code block">
						<Code2 size={14} />
					</button>
					<button type="button" onclick={toggleBulletList} class={btnClass(editor?.isActive('bulletList') ?? false)} title="Bullet list">
						<List size={14} />
					</button>

					{#if oncreateissue}
						<div class="bubble-separator"></div>
						<button type="button" onclick={createIssueFromSelection} class={btnClass(false)} title="Create issue from selection">
							<SquareArrowOutUpRight size={14} />
						</button>
					{/if}
					{#if onreworkselection}
						<div class="bubble-separator"></div>
						<button type="button" onpointerdown={(e) => e.preventDefault()} onclick={reworkSelection} class={btnClass(false)} title="Rework with AI" disabled={reworkingSelection}>
							<Sparkles size={14} class={reworkingSelection ? 'ai-rewrite-icon-loading' : ''} />
						</button>
					{/if}
					{/if}
				</div>
				{#if linkInputVisible}
					<div class="bubble-link-input">
						<!-- svelte-ignore a11y_autofocus -->
						<input
							type="url"
							bind:value={linkUrl}
							placeholder="https://..."
							autofocus
							onkeydown={(e) => {
								if (e.key === 'Enter') { e.preventDefault(); applyLink(); }
								if (e.key === 'Escape') { cancelLink(); }
							}}
							class="bubble-link-field"
						/>
					</div>
				{/if}
			{/snippet}
		</BubbleMenu>
	{/if}
</div>

<style>
	:global(.bubble-toolbar) {
		display: flex;
		align-items: center;
		gap: 2px;
		background: var(--color-bg-secondary);
		border: 1px solid var(--app-border);
		border-radius: 8px;
		padding: 4px;
		box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
	}
	:global(.bubble-separator) {
		width: 1px;
		height: 16px;
		background: var(--app-border);
		margin: 0 4px;
	}
	:global(.bubble-image-label) {
		padding: 0 4px;
		color: var(--color-text-tertiary);
		font-size: 11px;
		white-space: nowrap;
	}
	:global(.bubble-toolbar button) {
		min-width: 28px;
		padding: 0 5px;
		font-size: 11px;
	}
	:global(.bubble-link-input) {
		margin-top: 4px;
		background: var(--color-bg-secondary);
		border: 1px solid var(--app-border);
		border-radius: 8px;
		padding: 4px 8px;
		box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
	}
	:global(.bubble-link-field) {
		background: transparent;
		border: none;
		outline: none;
		color: var(--color-text-primary);
		font-size: 0.8rem;
		width: 200px;
	}
	:global(.rich-editor .tiptap) {
		min-height: 80px;
		padding: 0.5rem 0.75rem;
		font-size: 0.875rem;
		line-height: 1.5;
		color: var(--color-text-primary);
	}
	:global(.rich-editor.ai-rewrite-loading .tiptap) {
		opacity: 0.72;
		transition: opacity 120ms ease;
	}
	:global(.rich-editor.ai-rewrite-applied .tiptap) {
		animation: aiRewriteFlash 900ms ease-out;
	}
	:global(.ai-rewrite-overlay) {
		position: absolute;
		right: 8px;
		top: 8px;
		z-index: 30;
		display: inline-flex;
		align-items: center;
		gap: 6px;
		border: 1px solid color-mix(in srgb, var(--app-accent) 35%, var(--app-border));
		border-radius: 999px;
		background: color-mix(in srgb, var(--color-bg-secondary) 92%, var(--app-accent));
		padding: 4px 8px;
		color: var(--color-text-secondary);
		font-size: 11px;
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.22);
		animation: aiRewriteFloat 900ms ease-in-out infinite alternate;
	}
	:global(.ai-rewrite-spinner),
	:global(.ai-rewrite-icon-loading) {
		animation: aiRewriteSpin 1s linear infinite;
		color: var(--app-accent-light);
	}
	:global(.rich-editor .tiptap.borderless-editor) {
		min-height: var(--editor-min-height, 20px);
		padding: 0;
	}
	@keyframes aiRewriteSpin {
		from { transform: rotate(0deg); }
		to { transform: rotate(360deg); }
	}
	@keyframes aiRewriteFloat {
		from { transform: translateY(0); opacity: 0.82; }
		to { transform: translateY(-2px); opacity: 1; }
	}
	@keyframes aiRewriteFlash {
		0% {
			background: color-mix(in srgb, var(--app-accent) 18%, transparent);
			box-shadow: 0 0 0 0 color-mix(in srgb, var(--app-accent) 20%, transparent);
		}
		55% {
			background: color-mix(in srgb, var(--app-accent) 8%, transparent);
			box-shadow: 0 0 0 6px transparent;
		}
		100% {
			background: transparent;
			box-shadow: none;
		}
	}
	:global(.rich-editor .tiptap.compact-editor) {
		min-height: 24px;
		padding: 0.375rem 0.5rem;
		font-size: 0.8125rem;
	}
	:global(.rich-editor .tiptap.compact-editor p) {
		margin: 0;
	}
	:global(.rich-editor .tiptap p.is-editor-empty:first-child::before) {
		content: attr(data-placeholder);
		float: left;
		color: var(--color-text-tertiary);
		pointer-events: none;
		height: 0;
	}
	:global(.rich-editor .tiptap h1) {
		font-size: 1.25rem;
		font-weight: 600;
		margin: 0.75rem 0 0.25rem;
	}
	:global(.rich-editor .tiptap h2) {
		font-size: 1.1rem;
		font-weight: 600;
		margin: 0.5rem 0 0.25rem;
	}
	:global(.rich-editor .tiptap ul,
	.rich-editor .tiptap ol) {
		padding-left: 1.5rem;
		margin: 0.25rem 0;
	}
	:global(.rich-editor .tiptap ul) {
		list-style: disc;
	}
	:global(.rich-editor .tiptap ol) {
		list-style: decimal;
	}
	:global(.rich-editor .tiptap ul[data-type="taskList"]) {
		list-style: none;
		padding-left: 0;
	}
	:global(.rich-editor .tiptap ul[data-type="taskList"] li) {
		display: flex;
		align-items: flex-start;
		gap: 0.25rem;
		margin-bottom: 0.35rem;
	}
	:global(.rich-editor .tiptap ul[data-type="taskList"] li label) {
		display: flex;
		align-items: flex-start;
		gap: 0.2rem;
	}

	:global(.rich-editor .tiptap ul[data-type="taskList"] li input[type="checkbox"]) {
		appearance: none;
		-webkit-appearance: none;
		width: 15px;
		height: 15px;
		min-width: 15px;
		margin-top: 3px;
		border: 1.5px solid var(--app-border);
		border-radius: 3px;
		background: transparent;
		cursor: pointer;
		position: relative;
		transition: background-color 0.15s, border-color 0.15s;
	}

	:global(.rich-editor .tiptap ul[data-type="taskList"] li input[type="checkbox"]:checked) {
		background: var(--app-accent);
		border-color: var(--app-accent);
	}

	:global(.rich-editor .tiptap ul[data-type="taskList"] li input[type="checkbox"]:checked::after) {
		content: '';
		position: absolute;
		inset: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		background: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='white' stroke-width='3' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M20 6 9 17l-5-5'/%3E%3C/svg%3E") center/13px no-repeat;
	}

	:global(.rich-editor .tiptap ul[data-type="taskList"] li input[type="checkbox"]:hover) {
		border-color: var(--app-accent);
	}
	:global(.rich-editor .tiptap code) {
		background: var(--color-bg-tertiary);
		padding: 0.125rem 0.25rem;
		border-radius: 0.25rem;
		font-size: 0.8em;
	}
	:global(.rich-editor .tiptap pre) {
		background: var(--color-bg-tertiary);
		padding: 0.75rem 1rem;
		border-radius: 0.375rem;
		margin: 0.5rem 0;
		overflow-x: auto;
	}
	:global(.rich-editor .tiptap pre code) {
		background: none;
		padding: 0;
	}
	:global(.rich-editor .tiptap blockquote) {
		border-left: 3px solid var(--app-border);
		padding-left: 1rem;
		margin: 0.5rem 0;
		color: var(--color-text-secondary);
	}
	:global(.rich-editor .tiptap a) {
		color: var(--app-accent-light);
		text-decoration: underline;
	}
	:global(.rich-editor .tiptap hr) {
		border: none;
		border-top: 1px solid var(--app-border);
		margin: 1rem 0;
	}
	:global(.rich-editor .tiptap img) {
		max-width: 100%;
		height: auto;
		border-radius: 0.375rem;
		margin: 0.5rem 0;
	}
	:global(.rich-editor .tiptap .resizable-image-wrapper) {
		position: relative;
		display: inline-block;
		max-width: 100%;
		margin: 0.5rem 0;
		line-height: 0;
		vertical-align: middle;
	}
	:global(.rich-editor .tiptap .resizable-image-wrapper img) {
		display: block;
		max-width: 100%;
		margin: 0;
		pointer-events: none;
	}
	:global(.rich-editor .tiptap .resizable-image-wrapper.ProseMirror-selectednode),
	:global(.rich-editor .tiptap .resizable-image-wrapper.is-resizing) {
		border-radius: 0.375rem;
		outline: 2px solid var(--app-accent);
		outline-offset: 2px;
	}
	:global(.rich-editor .tiptap .image-resize-handle) {
		position: absolute;
		right: -7px;
		bottom: -7px;
		display: none;
		width: 15px;
		height: 15px;
		border: 2px solid var(--color-bg-secondary);
		border-radius: 4px;
		background: var(--app-accent);
		box-shadow: 0 1px 4px rgba(0, 0, 0, 0.35);
		cursor: nwse-resize;
		touch-action: none;
	}
	:global(.rich-editor .tiptap .resizable-image-wrapper.ProseMirror-selectednode .image-resize-handle),
	:global(.rich-editor .tiptap .resizable-image-wrapper.is-resizing .image-resize-handle) {
		display: block;
	}
	:global(.rich-editor .tiptap .image-resize-handle:focus-visible) {
		display: block;
		outline: 2px solid var(--color-text-primary);
		outline-offset: 2px;
	}
	:global(.rich-editor .tiptap .editor-upload-placeholder) {
		display: inline-flex;
		align-items: center;
		margin: 0 0.2rem;
		border-radius: 0.25rem;
		background: var(--color-bg-hover);
		padding: 0.1rem 0.35rem;
		color: var(--color-text-tertiary);
		font-size: 0.75rem;
		font-style: italic;
	}
</style>
