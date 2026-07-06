<script lang="ts">
	import { Check, Copy, GitBranch, Link as LinkIcon, X } from 'lucide-svelte';
	import IssueStatusIcon from './IssueStatusIcon.svelte';

	let {
		identifier,
		title,
		href,
		url,
		branchName,
		status,
		statusCategory,
		statusColor,
		closeToast
	}: {
		identifier: string;
		title: string;
		href: string;
		url: string;
		branchName: string;
		status?: string;
		statusCategory?: string;
		statusColor?: string | null;
		closeToast?: () => void;
	} = $props();

	let copied = $state<string | null>(null);
	let copyTimer: ReturnType<typeof setTimeout> | undefined;

	function dismiss() {
		closeToast?.();
	}

	async function copy(text: string, label: string) {
		await navigator.clipboard.writeText(text);
		copied = label;
		clearTimeout(copyTimer);
		copyTimer = setTimeout(() => {
			copied = null;
		}, 1200);
	}
</script>

<div class="issue-created-toast">
	<div class="success-icon" aria-hidden="true">
		<Check size={14} strokeWidth={3} />
	</div>

	<div class="content">
		<div class="header">
			<div class="title">Issue created</div>
			<button class="close" type="button" aria-label="Close toast" onclick={dismiss}>
				<X size={16} />
			</button>
		</div>

		<a {href} class="issue-line" onclick={dismiss}>
			<span class="status-icon" aria-hidden="true">
				<IssueStatusIcon {status} category={statusCategory} color={statusColor} size={13} />
			</span>
			<span class="issue-text">{identifier} &ndash; {title}</span>
		</a>

		<div class="footer">
			<a {href} class="view-link" onclick={dismiss}>View issue</a>

			<div class="commands" aria-label="Issue commands">
				<button type="button" aria-label="Copy issue link" title="Copy issue link" onclick={() => copy(url, 'link')}>
					{#if copied === 'link'}
						<Check size={12} />
					{:else}
						<LinkIcon size={12} />
					{/if}
				</button>
				<button type="button" aria-label="Copy issue ID" title="Copy issue ID" onclick={() => copy(identifier, 'id')}>
					{#if copied === 'id'}
						<Check size={12} />
					{:else}
						<Copy size={12} />
					{/if}
				</button>
				<button
					type="button"
					aria-label="Copy branch name"
					title="Copy branch name"
					onclick={() => copy(branchName, 'branch')}
				>
					{#if copied === 'branch'}
						<Check size={12} />
					{:else}
						<GitBranch size={12} />
					{/if}
				</button>
			</div>
		</div>
	</div>
</div>

<style>
	.issue-created-toast {
		display: grid;
		box-sizing: border-box;
		grid-template-columns: 22px minmax(0, 1fr);
		align-items: start;
		width: 336px;
		height: 120px;
		max-width: calc(100vw - 32px);
		column-gap: 10px;
		padding: 16px;
		border: 1px solid var(--app-border);
		border-radius: 16px;
		background: var(--color-bg-secondary);
		color: var(--color-text-primary);
		box-shadow: 0 16px 40px rgb(0 0 0 / 24%);
	}

	.success-icon {
		display: flex;
		width: 22px;
		height: 22px;
		flex: 0 0 auto;
		align-items: center;
		justify-content: center;
		border-radius: 999px;
		background: var(--color-success);
		color: white;
	}

	.content {
		display: grid;
		grid-template-rows: 20px 26px 24px;
		row-gap: 8px;
		height: 86px;
		min-width: 0;
	}

	.header {
		display: flex;
		height: 20px;
		align-items: flex-start;
		justify-content: space-between;
		gap: 10px;
	}

	.title {
		font-size: 15px;
		font-weight: 600;
		line-height: 20px;
		letter-spacing: -0.02em;
	}

	.close,
	.commands button {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		border: 0;
		background: transparent;
		color: var(--color-text-tertiary);
		transition:
			background-color 120ms ease,
			color 120ms ease,
			opacity 120ms ease;
	}

	.close {
		margin: -2px -3px 0 0;
		padding: 2px;
		border-radius: 6px;
	}

	.close:hover,
	.commands button:hover {
		background: var(--color-bg-hover);
		color: var(--color-text-primary);
	}

	.issue-line {
		display: inline-flex;
		height: 26px;
		max-width: 100%;
		min-width: 0;
		align-items: center;
		gap: 8px;
		align-self: flex-start;
		box-sizing: border-box;
		margin-left: -6px;
		padding: 0 6px;
		border-radius: 7px;
		font-size: 13px;
		line-height: 26px;
		color: var(--color-text-primary);
		text-decoration: none;
		transition:
			background-color 120ms ease,
			color 120ms ease;
	}

	.issue-line:hover {
		background: var(--color-bg-hover);
		color: var(--color-text-primary);
	}

	.status-icon {
		display: inline-flex;
		width: 13px;
		height: 13px;
		flex: 0 0 auto;
		align-items: center;
		justify-content: center;
	}

	.issue-text {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.footer {
		display: flex;
		height: 24px;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}

	.view-link {
		display: inline-flex;
		height: 24px;
		align-items: center;
		font-size: 13px;
		font-weight: 500;
		line-height: 1;
		color: var(--app-accent-light);
		text-decoration: none;
	}

	.view-link:hover {
		color: var(--app-accent-hover);
	}

	.commands {
		display: flex;
		height: 18px;
		flex: 0 0 auto;
		align-items: center;
		gap: 11px;
		margin-left: auto;
		opacity: 0;
		visibility: hidden;
		transition: opacity 120ms ease;
	}

	.issue-created-toast:hover .commands,
	.issue-created-toast:focus-within .commands {
		opacity: 1;
		visibility: visible;
	}

	.commands button {
		width: 18px;
		height: 18px;
		flex: 0 0 18px;
		border-radius: 5px;
		padding: 0;
		line-height: 1;
	}

	.commands button :global(svg),
	.close :global(svg) {
		display: block;
	}

	@media (max-width: 600px) {
		.issue-created-toast {
			width: calc(100vw - 32px);
			height: 118px;
			padding: 15px;
			border-radius: 15px;
		}

		.commands {
			opacity: 1;
			visibility: visible;
			gap: 10px;
		}
	}

	:global([data-sonner-toast].issue-created-toast-shell) {
		height: 120px !important;
		width: 336px !important;
	}

	@media (max-width: 600px) {
		:global([data-sonner-toast].issue-created-toast-shell) {
			height: 118px !important;
			width: calc(100% - var(--mobile-offset-left) * 2) !important;
		}
	}
</style>
