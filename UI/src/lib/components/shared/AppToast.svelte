<script lang="ts">
	import { AlertTriangle, Check, Info, X, XCircle } from 'lucide-svelte';

	export type AppToastVariant = 'success' | 'error' | 'info' | 'warning';

	let {
		variant = 'info',
		title,
		description,
		closeToast
	}: {
		variant?: AppToastVariant;
		title: string;
		description?: string;
		closeToast?: () => void;
	} = $props();

	const role = $derived(variant === 'error' ? 'alert' : 'status');

	function dismiss() {
		closeToast?.();
	}
</script>

<div class={`app-toast app-toast-${variant}`} class:has-description={Boolean(description)} {role}>
	<div class="toast-icon" aria-hidden="true">
		{#if variant === 'success'}
			<Check size={14} strokeWidth={3} />
		{:else if variant === 'error'}
			<XCircle size={14} strokeWidth={2.5} />
		{:else if variant === 'warning'}
			<AlertTriangle size={14} strokeWidth={2.5} />
		{:else}
			<Info size={14} strokeWidth={2.5} />
		{/if}
	</div>

	<div class="content">
		<div class="header">
			<div class="title">{title}</div>
			<button class="close" type="button" aria-label="Close toast" onclick={dismiss}>
				<X size={16} />
			</button>
		</div>

		{#if description}
			<div class="description">{description}</div>
		{/if}
	</div>
</div>

<style>
	.app-toast {
		display: grid;
		box-sizing: border-box;
		grid-template-columns: 22px minmax(0, 1fr);
		align-items: start;
		width: 336px;
		min-height: 56px;
		max-width: calc(100vw - 32px);
		column-gap: 10px;
		padding: 16px;
		border: 1px solid var(--app-border);
		border-radius: 16px;
		background: var(--color-bg-secondary);
		color: var(--color-text-primary);
		box-shadow: 0 16px 40px rgb(0 0 0 / 24%);
	}

	.app-toast.has-description {
		min-height: 74px;
	}

	.toast-icon {
		display: flex;
		width: 22px;
		height: 22px;
		flex: 0 0 auto;
		align-items: center;
		justify-content: center;
		border-radius: 999px;
		color: white;
	}

	.app-toast-success .toast-icon {
		background: var(--color-success);
	}

	.app-toast-error .toast-icon {
		background: var(--color-error);
	}

	.app-toast-warning .toast-icon {
		background: var(--color-warning);
	}

	.app-toast-info .toast-icon {
		background: var(--app-accent);
		color: var(--app-accent-foreground);
	}

	.content {
		display: grid;
		min-width: 0;
		row-gap: 6px;
	}

	.header {
		display: flex;
		min-height: 20px;
		align-items: flex-start;
		justify-content: space-between;
		gap: 10px;
	}

	.title {
		overflow: hidden;
		font-size: 15px;
		font-weight: 600;
		line-height: 20px;
		letter-spacing: -0.02em;
		text-overflow: ellipsis;
	}

	.description {
		overflow: hidden;
		font-size: 13px;
		line-height: 18px;
		color: var(--color-text-secondary);
		text-overflow: ellipsis;
	}

	.close {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		margin: -2px -3px 0 0;
		padding: 2px;
		border: 0;
		border-radius: 6px;
		background: transparent;
		color: var(--color-text-tertiary);
		transition:
			background-color 120ms ease,
			color 120ms ease,
			opacity 120ms ease;
	}

	.close:hover {
		background: var(--color-bg-hover);
		color: var(--color-text-primary);
	}

	.close :global(svg) {
		display: block;
	}

	:global([data-sonner-toast].app-toast-shell) {
		width: 336px !important;
	}

	@media (max-width: 600px) {
		.app-toast {
			width: calc(100vw - 32px);
			min-height: 54px;
			padding: 15px;
			border-radius: 15px;
		}

		.app-toast.has-description {
			min-height: 72px;
		}

		:global([data-sonner-toast].app-toast-shell) {
			width: calc(100% - var(--mobile-offset-left) * 2) !important;
		}
	}
</style>
