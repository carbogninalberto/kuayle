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
	import { Extension, InputRule } from '@tiptap/core';
	import { Plugin } from 'prosemirror-state';
	import { common, createLowlight } from 'lowlight';
	import { Button } from '$lib/components/ui/button';
	import { Separator } from '$lib/components/ui/separator';
	import {
		Bold,
		Italic,
		Strikethrough,
		Code,
		Heading1,
		Heading2,
		List,
		ListOrdered,
		ListChecks,
		Link as LinkIcon,
		Code2,
		Undo2,
		Redo2
	} from 'lucide-svelte';
	import { sanitizeEditorOutput } from '$lib/security/sanitize';

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
		remoteCursors,
		onfocus: onFocusProp,
		onblur: onBlurProp,
		oncursorchange
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
		remoteCursors?: Array<{ name: string; color: string; position: number }>;
		onfocus?: () => void;
		onblur?: () => void;
		oncursorchange?: (position: number) => void;
	} = $props();

	let editor = $state<Editor | null>(null);
	let isFocused = $state(false);
	let linkInputVisible = $state(false);
	let linkUrl = $state('');
	let cursorElements: HTMLElement[] = [];

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

	async function uploadImage(file: File): Promise<string | null> {
		if (!uploadUrl) return null;
		const form = new FormData();
		form.append('file', file);
		try {
			const res = await fetch(uploadUrl, { method: 'POST', body: form, credentials: 'include' });
			if (!res.ok) return null;
			const data = await res.json();
			return data.data?.url ?? data.url ?? null;
		} catch {
			return null;
		}
	}

	function createImagePasteHandler() {
		if (!uploadUrl) return null;
		return Extension.create({
			name: 'imagePaste',
			addProseMirrorPlugins() {
				const editorRef = this.editor;
				return [
					new Plugin({
						props: {
							handlePaste(view: any, event: ClipboardEvent) {
								const items = event.clipboardData?.items;
								if (!items) return false;
								for (const item of items) {
									if (item.type.startsWith('image/')) {
										event.preventDefault();
										const file = item.getAsFile();
										if (file) {
											uploadImage(file).then(url => {
												if (url) {
													editorRef.chain().focus().setImage({ src: url }).run();
												}
											});
										}
										return true;
									}
								}
								return false;
							},
							handleDrop(view: any, event: DragEvent) {
								const files = event.dataTransfer?.files;
								if (!files || files.length === 0) return false;
								for (const file of files) {
									if (file.type.startsWith('image/')) {
										event.preventDefault();
										uploadImage(file).then(url => {
											if (url) {
												editorRef.chain().focus().setImage({ src: url }).run();
											}
										});
										return true;
									}
								}
								return false;
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

		const extensions = [
			StarterKit.configure({
				codeBlock: false,
			}),
			Placeholder.configure({ placeholder }),
			TaskList,
			TaskItem.configure({ nested: true }),
			Link.configure({
				openOnClick: false,
				HTMLAttributes: { class: 'text-[var(--app-accent-light)] underline' }
			}),
			CodeBlockLowlight.configure({ lowlight }),
			Image.configure({ inline: true, allowBase64: false }),
			TaskListShortcut,
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
				oncursorchange?.(e.state.selection.head);
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
			const pos = Math.max(0, Math.min(rc.position, docSize));
			try {
				const coords = view.coordsAtPos(pos);

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

	// Sync content from outside (e.g. real-time updates) without losing cursor
	$effect(() => {
		if (editor && !isFocused && content !== undefined) {
			const current = sanitizeEditorOutput(editor.getHTML());
			if (current !== content) {
				editor.commands.setContent(content, false);
			}
		}
	});

	onDestroy(() => {
		cursorElements.forEach(el => el.remove());
		editor?.destroy();
	});

	function toggleBold() { editor?.chain().focus().toggleBold().run(); }
	function toggleItalic() { editor?.chain().focus().toggleItalic().run(); }
	function toggleStrike() { editor?.chain().focus().toggleStrike().run(); }
	function toggleCode() { editor?.chain().focus().toggleCode().run(); }
	function toggleH1() { editor?.chain().focus().toggleHeading({ level: 1 }).run(); }
	function toggleH2() { editor?.chain().focus().toggleHeading({ level: 2 }).run(); }
	function toggleBulletList() { editor?.chain().focus().toggleBulletList().run(); }
	function toggleOrderedList() { editor?.chain().focus().toggleOrderedList().run(); }
	function toggleTaskList() { editor?.chain().focus().toggleTaskList().run(); }
	function toggleCodeBlock() { editor?.chain().focus().toggleCodeBlock().run(); }
	function undo() { editor?.chain().focus().undo().run(); }
	function redo() { editor?.chain().focus().redo().run(); }

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
		if (props.from === props.to) return false;
		if (props.editor.isActive('codeBlock')) return false;
		if (props.editor.isActive('image')) return false;
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

			<div class="flex-1"></div>

			<button type="button" onclick={undo} class={btnClass(false)} title="Undo">
				<Undo2 size={14} />
			</button>
			<button type="button" onclick={redo} class={btnClass(false)} title="Redo">
				<Redo2 size={14} />
			</button>
		</div>
	{/if}

	<!-- Editor content -->
	{#if editor}
		<div class="rich-editor" style="position: relative; overflow: visible; {minHeight ? `--editor-min-height: ${minHeight}` : ''}">
			<EditorContent {editor} />
		</div>
	{/if}

	{#if bubbleMenu && editor && editable && isFocused}
		<BubbleMenu {editor} shouldShow={shouldShowBubble}>
			{#snippet children()}
				<div class="bubble-toolbar">
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

					<div class="bubble-separator"></div>

					<button type="button" onclick={toggleH1} class={btnClass(editor?.isActive('heading', { level: 1 }) ?? false)} title="Heading 1">
						<Heading1 size={14} />
					</button>
					<button type="button" onclick={toggleH2} class={btnClass(editor?.isActive('heading', { level: 2 }) ?? false)} title="Heading 2">
						<Heading2 size={14} />
					</button>

					<div class="bubble-separator"></div>

					<button type="button" onclick={toggleLink} class={btnClass(editor?.isActive('link') ?? false)} title="Link">
						<LinkIcon size={14} />
					</button>
					<button type="button" onclick={toggleCodeBlock} class={btnClass(editor?.isActive('codeBlock') ?? false)} title="Code block">
						<Code2 size={14} />
					</button>
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
	:global(.rich-editor .tiptap.borderless-editor) {
		min-height: var(--editor-min-height, 20px);
		padding: 0;
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
		gap: 0.5rem;
	}
	:global(.rich-editor .tiptap ul[data-type="taskList"] li input) {
		margin-top: 0.25rem;
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
</style>
