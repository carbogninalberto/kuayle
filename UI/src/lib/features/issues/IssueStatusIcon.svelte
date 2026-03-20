<script lang="ts">
	import type { IssueStatus } from '$lib/types/issue';
	import type { StatusCategory } from '$lib/types/team-status';
	import { Circle, CircleDashed, Contrast, ClockFading, CheckCircle2, XCircle } from 'lucide-svelte';

	let {
		status,
		category,
		color,
		size = 16
	}: {
		status?: IssueStatus | string;
		category?: StatusCategory | string;
		color?: string | null;
		size?: number;
	} = $props();

	// Map legacy status slugs to categories
	const STATUS_TO_CATEGORY: Record<string, StatusCategory> = {
		backlog: 'backlog',
		todo: 'unstarted',
		in_progress: 'started',
		in_review: 'started',
		done: 'completed',
		cancelled: 'cancelled',
	};

	// Per-slug icon overrides (for statuses that share a category but need different icons)
	const STATUS_ICONS: Record<string, any> = {
		in_progress: Contrast,
		in_review: ClockFading,
	};

	const resolvedCategory = $derived(
		category ?? (status ? STATUS_TO_CATEGORY[status] ?? 'backlog' : 'backlog')
	);

	const categoryIcons: Record<string, any> = {
		backlog: CircleDashed,
		unstarted: Circle,
		started: Contrast,
		completed: CheckCircle2,
		cancelled: XCircle,
	};

	const defaultColors: Record<string, string> = {
		backlog: 'text-[var(--color-text-tertiary)]',
		unstarted: 'text-[var(--color-text-secondary)]',
		started: 'text-yellow-500',
		completed: 'text-[var(--color-success)]',
		cancelled: 'text-[var(--color-text-tertiary)]',
	};

	// Use slug-specific icon if available, otherwise fall back to category icon
	const Icon = $derived(
		(status && STATUS_ICONS[status]) ?? categoryIcons[resolvedCategory] ?? CircleDashed
	);
	const colorClass = $derived(color ? '' : (defaultColors[resolvedCategory] ?? defaultColors.backlog));
	const colorStyle = $derived(color ? `color: ${color}` : '');
</script>

<span class={colorClass} style={colorStyle}>
	<Icon {size} />
</span>
