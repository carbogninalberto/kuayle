<script lang="ts">
	import { Check, Trash2 } from 'lucide-svelte';
	import IssueStatusIcon from './IssueStatusIcon.svelte';

	let {
		identifier,
		title,
		status,
		statusCategory,
		statusColor,
		count = 1
	}: {
		identifier?: string;
		title?: string;
		status?: string;
		statusCategory?: string;
		statusColor?: string | null;
		count?: number;
	} = $props();

	const isSingleIssue = $derived(Boolean(identifier && title && count === 1));
</script>

<div class="issue-deleted-toast">
	<div class="deleted-icon" aria-hidden="true">
		<Check size={14} strokeWidth={3} />
	</div>

	<div class="content">
		<div class="title">{count === 1 ? 'Issue deleted' : 'Issues deleted'}</div>

		<div class="issue-line">
			{#if isSingleIssue}
				<span class="status-icon" aria-hidden="true">
					<IssueStatusIcon {status} category={statusCategory} color={statusColor} size={13} />
				</span>
				<span class="issue-text">{identifier} &ndash; {title}</span>
			{:else}
				<span class="status-icon multi" aria-hidden="true">
					<Trash2 size={12} />
				</span>
				<span class="issue-text">Deleted {count} issues</span>
			{/if}
		</div>
	</div>
</div>

<style>
	.issue-deleted-toast {
		display: grid;
		box-sizing: border-box;
		grid-template-columns: 22px minmax(0, 1fr);
		align-items: start;
		width: 336px;
		height: 82px;
		max-width: calc(100vw - 32px);
		column-gap: 10px;
		padding: 16px;
		border: 1px solid var(--app-border);
		border-radius: 16px;
		background: var(--color-bg-secondary);
		color: var(--color-text-primary);
		box-shadow: 0 16px 40px rgb(0 0 0 / 24%);
	}

	.deleted-icon {
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
		grid-template-rows: 20px 26px;
		row-gap: 4px;
		height: 50px;
		min-width: 0;
	}

	.title {
		font-size: 15px;
		font-weight: 600;
		line-height: 20px;
		letter-spacing: -0.02em;
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
	}

	.status-icon {
		display: inline-flex;
		width: 13px;
		height: 13px;
		flex: 0 0 auto;
		align-items: center;
		justify-content: center;
	}

	.status-icon.multi {
		color: var(--color-text-tertiary);
	}

	.issue-text {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	:global([data-sonner-toast].issue-deleted-toast-shell) {
		height: 82px !important;
		width: 336px !important;
	}

	@media (max-width: 600px) {
		.issue-deleted-toast {
			width: calc(100vw - 32px);
			height: 80px;
			padding: 15px;
			border-radius: 15px;
		}

		:global([data-sonner-toast].issue-deleted-toast-shell) {
			height: 80px !important;
			width: calc(100% - var(--mobile-offset-left) * 2) !important;
		}
	}
</style>
