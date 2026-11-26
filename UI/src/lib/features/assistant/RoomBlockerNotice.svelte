<script lang="ts">
	import type { TaskMessage } from '$lib/api/agent-tasks';
	import { Button } from '$lib/components/ui/button/index.js';
	import { m } from '$lib/features/i18n/messages';
	import type { ConversationState } from './conversation-state.svelte';
	import { firstLine } from './thread-rows';

	let { post, chat, onfollow }: { post: TaskMessage; chat: ConversationState; onfollow?: () => void } = $props();
	const href = $derived('/' + chat.slug + '/assistant?conversation=' + encodeURIComponent(chat.id) +
		'&agent=' + encodeURIComponent(post.task_id) + '#room-post-' + post.seq);
	const action = $derived(post.data.action === 'split' ? m['sharedComponents.filter_builder.sub_issues']() : post.data.action);
	const subject = $derived(firstLine(post.body || post.data.issues?.map((issue) => issue.title).join(', '), 120));
</script>

<p class="text-sm">{m['tasks.room_waiting_on']({ role: '@' + post.author_role })}</p>
<Button {href} size="sm" variant="link" class="h-auto max-w-full justify-start whitespace-normal break-words px-0 text-left underline"
	onclick={onfollow}><span>#{post.seq} · {[action, subject].filter(Boolean).join(' · ')}</span></Button>
