import { test, expect, type Page } from '@playwright/test';

const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
const conversation = {
	id: 'chat-1',
	title: 'Images',
	workspace_id: workspace.id,
	creator_id: 'user-1',
	created_at: '2026-10-26T10:00:00Z',
	updated_at: '2026-10-26T10:00:00Z'
};
// A 1x1 PNG.
const png = Buffer.from(
	'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFBQIAX8jx0gAAAABJRU5ErkJggg==',
	'base64'
);

async function fixture(page: Page) {
	const state = {
		uploads: [] as string[],
		removed: [] as string[],
		sends: [] as Array<{ content: string; attachment_ids?: string[] }>,
		uploadGate: null as Promise<void> | null,
		failNextUpload: 0,
		messages: [] as any[],
		patches: [] as any[],
		vision: { answered: true, saw_image: true, answer: 'Red.' } as Record<string, unknown>
	};
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.routeWebSocket('**/conversations/chat-1/stream*', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const url = new URL(route.request().url());
		const path = url.pathname;
		const method = route.request().method();
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user-1', name: 'Owner', display_name: 'Owner', email: 'owner@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path.endsWith('/assistant/status'))
			return route.fulfill({ json: { configured: true, provider: 'openai_compatible', model: 'vision-model' } });
		if (path.endsWith('/ai-settings/vision-check')) return route.fulfill({ json: state.vision });
		if (path.endsWith('/ai-settings')) {
			if (method === 'PATCH') state.patches.push(route.request().postDataJSON());
			return route.fulfill({
				json: {
					provider: 'openai_compatible',
					base_url: 'https://api.example.test/v1',
					model: 'vision-model',
					has_api_key: true,
					description_expand_prompt: 'p',
					default_prompt: 'p',
					issue_copy_prompt: 'c',
					default_issue_copy_prompt: 'c',
					assistant_vision: state.patches.at(-1)?.assistant_vision ?? false
				}
			});
		}
		if (path.endsWith('/conversations')) return route.fulfill({ json: method === 'POST' ? conversation : [conversation] });
		if (path.includes('/conversations/chat-1/attachments')) {
			const id = path.split('/attachments/')[1];
			if (method === 'DELETE') {
				state.removed.push(id);
				return route.fulfill({ status: 204 });
			}
			if (method === 'GET') return route.fulfill({ body: png, contentType: 'image/png' });
			const body = route.request().postDataBuffer()?.toString('latin1') ?? '';
			const filename = /filename="([^"]+)"/.exec(body)?.[1] ?? 'image.png';
			if (state.uploadGate) await state.uploadGate;
			if (state.failNextUpload) {
				state.failNextUpload--;
				return route.fulfill({ status: 415, json: { error: { code: 'ATTACHMENT_UNSUPPORTED' } } });
			}
			state.uploads.push(filename);
			return route.fulfill({
				status: 201,
				json: {
					id: `attachment-${state.uploads.length}`,
					conversation_id: 'chat-1',
					filename,
					content_type: 'image/png',
					size: png.length,
					width: 1,
					height: 1,
					position: 0,
					created_at: conversation.created_at
				}
			});
		}
		if (path.endsWith('/conversations/chat-1/messages')) {
			const sent = route.request().postDataJSON();
			state.sends.push(sent);
			const user = {
				id: 'message-1',
				conversation_id: 'chat-1',
				sequence: 1,
				request_id: sent.request_id,
				parent_id: null,
				role: 'user',
				content: sent.content,
				status: 'completed',
				provider: '',
				model: '',
				created_at: conversation.created_at,
				updated_at: conversation.created_at,
				attachments: (sent.attachment_ids ?? []).map((id: string, position: number) => ({
					id,
					conversation_id: 'chat-1',
					message_id: 'message-1',
					filename: state.uploads[Number(id.split('-')[1]) - 1],
					content_type: 'image/png',
					size: png.length,
					width: 1,
					height: 1,
					position,
					created_at: conversation.created_at
				}))
			};
			const reply = { ...user, id: 'message-2', sequence: 2, role: 'assistant', content: 'I see it.', attachments: undefined };
			state.messages = [user, reply];
			return route.fulfill({ status: 202, json: reply });
		}
		if (path.endsWith('/conversations/chat-1'))
			return route.fulfill({
				json: { conversation, messages: state.messages, tools: [], approvals: [], cursor: 0, has_more: false }
			});
		return route.fulfill({ json: [] });
	});
	return state;
}

const file = (name: string, mimeType = 'image/png') => ({ name, mimeType, buffer: png });

test('attaches images by button, paste and drop, uploads them and sends their IDs', async ({ page }, testInfo) => {
	const state = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	const composer = page.getByRole('textbox', { name: 'Message' });
	const send = page.getByRole('button', { name: 'Send message' });
	await expect(page.getByRole('button', { name: 'Attach images' })).toBeEnabled();

	let release!: () => void;
	state.uploadGate = new Promise((resolve) => (release = resolve));
	await page.locator('input[type="file"]').setInputFiles([file('first.png')]);
	const attached = page.getByRole('list', { name: 'Attached images' });
	await expect(attached.getByText('first.png')).toBeVisible();
	await expect(attached.getByRole('progressbar', { name: 'Uploading first.png' })).toBeVisible();
	await composer.fill('What is wrong here?');
	await expect(send).toBeDisabled();
	release();
	state.uploadGate = null;
	await expect(attached.getByRole('progressbar')).toHaveCount(0);
	await expect(send).toBeEnabled();

	// Paste and drag-and-drop use the same upload flow.
	await composer.evaluate((element, bytes) => {
		const data = new DataTransfer();
		data.items.add(new File([new Uint8Array(bytes)], 'pasted.png', { type: 'image/png' }));
		element.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
	}, [...png]);
	await page.getByRole('form', { name: 'Message composer' }).evaluate((element, bytes) => {
		const data = new DataTransfer();
		data.items.add(new File([new Uint8Array(bytes)], 'dropped.png', { type: 'image/png' }));
		element.dispatchEvent(new DragEvent('dragover', { dataTransfer: data, bubbles: true, cancelable: true }));
		element.dispatchEvent(new DragEvent('drop', { dataTransfer: data, bubbles: true, cancelable: true }));
	}, [...png]);
	await expect.poll(() => state.uploads).toEqual(['first.png', 'pasted.png', 'dropped.png']);

	// Remove is keyboard reachable and discards the upload on the server.
	await attached.getByRole('button', { name: 'Remove pasted.png' }).focus();
	await page.keyboard.press('Enter');
	await expect(attached.getByText('pasted.png')).toHaveCount(0);
	await expect.poll(() => state.removed).toEqual(['attachment-2']);

	await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('composer.png') });
	await send.click();
	await expect.poll(() => state.sends).toEqual([
		expect.objectContaining({ content: 'What is wrong here?', attachment_ids: ['attachment-1', 'attachment-3'] })
	]);
	await expect(page.getByRole('list', { name: 'Attached images' })).toHaveCount(1);

	// The sent message shows thumbnails that open in a dialog.
	const open = page.getByRole('button', { name: 'Open image first.png' });
	await expect(open).toBeVisible();
	await open.focus();
	await page.keyboard.press('Enter');
	const dialog = page.getByRole('dialog', { name: 'first.png' });
	await expect(dialog).toBeVisible();
	await expect(dialog.getByRole('img', { name: 'first.png' })).toBeVisible();
	await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('lightbox.png') });
	await page.keyboard.press('Escape');
	await expect(dialog).toHaveCount(0);
	await expect(open).toBeFocused();
});

test('explains refused files and failed uploads before anything is sent', async ({ page }) => {
	const state = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	const input = page.locator('input[type="file"]');
	await input.setInputFiles([file('notes.pdf', 'application/pdf')]);
	await expect(page.getByRole('alert').filter({ hasText: 'only PNG, JPEG, WebP and GIF' })).toBeVisible();
	expect(state.uploads).toEqual([]);

	state.failNextUpload = 1;
	await input.setInputFiles([file('disguised.png')]);
	const attached = page.getByRole('list', { name: 'Attached images' });
	await expect(attached.getByRole('alert')).toContainText('disguised.png');
	await expect(page.getByRole('button', { name: 'Send message' })).toBeDisabled();
	await attached.getByRole('button', { name: 'Retry uploading disguised.png' }).click();
	await expect.poll(() => state.uploads).toEqual(['disguised.png']);
	await expect(attached.getByRole('alert')).toHaveCount(0);
	// An image alone can be sent.
	await expect(page.getByRole('button', { name: 'Send message' })).toBeEnabled();

	await input.setInputFiles(Array.from({ length: 6 }, (_, i) => file(`many-${i}.png`)));
	await expect(page.getByRole('alert').filter({ hasText: 'At most 6 images' })).toBeVisible();
	await expect(attached.getByRole('listitem')).toHaveCount(6);
});

test('the owner enables image input and checks whether the model sees images', async ({ page }) => {
	const state = await fixture(page);
	await page.goto('/test/settings/ai');
	const toggle = page.getByRole('switch', { name: 'Model can see images' });
	await expect(toggle).not.toBeChecked();
	await page.getByRole('button', { name: 'Check' }).click();
	await expect(page.getByRole('status').filter({ hasText: 'recognised the test image: Red.' })).toBeVisible();
	state.vision = { answered: false, saw_image: false, error: 'The provider refused the image request (status 400).' };
	await page.getByRole('button', { name: 'Check' }).click();
	await expect(page.getByRole('status').filter({ hasText: 'did not answer the image request' })).toBeVisible();
	await toggle.click();
	await page.getByRole('button', { name: 'Save AI settings' }).click();
	await expect.poll(() => state.patches.at(-1)?.assistant_vision).toBe(true);
	await expect(toggle).toBeChecked();
});
