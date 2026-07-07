import { toast } from 'svelte-sonner';
import AppToast, { type AppToastVariant } from '$lib/components/shared/AppToast.svelte';

type ToastOptions = {
	description?: string;
	duration?: number;
};

const DEFAULT_DURATION: Record<AppToastVariant, number> = {
	success: 4000,
	error: 6000,
	info: 4000,
	warning: 5000
};

function showToast(variant: AppToastVariant, title: string, options: ToastOptions = {}) {
	toast.custom(AppToast, {
		class: 'app-toast-shell',
		duration: options.duration ?? DEFAULT_DURATION[variant],
		componentProps: {
			variant,
			title,
			description: options.description
		}
	});
}

function getErrorMessage(error: unknown, fallback: string): string {
	if (error && typeof error === 'object') {
		const maybeError = error as { error?: { message?: unknown }; message?: unknown };
		if (typeof maybeError.error?.message === 'string' && maybeError.error.message) {
			return maybeError.error.message;
		}

		if (typeof maybeError.message === 'string' && maybeError.message) {
			return maybeError.message;
		}
	}

	return fallback;
}

export function showSuccess(title: string, options?: ToastOptions) {
	showToast('success', title, options);
}

export function showError(title: string, options?: ToastOptions) {
	showToast('error', title, options);
}

export function showInfo(title: string, options?: ToastOptions) {
	showToast('info', title, options);
}

export function showWarning(title: string, options?: ToastOptions) {
	showToast('warning', title, options);
}

export function showApiError(error: unknown, fallback: string, options?: ToastOptions) {
	showError(getErrorMessage(error, fallback), options);
}

export const appToast = {
	success: showSuccess,
	error: showError,
	info: showInfo,
	warning: showWarning,
	apiError: showApiError
};
