import type { RunPrompt } from '$lib/api/agent-tasks';

// The prompt an attempt received as sections to scan: one per source, in the
// order Kuayle assembled it, each with its size.

export interface PromptSection {
	key: string;
	source: string;
	channel: string;
	text: string;
	bytes: number;
	/** The part was cut when it was recorded. */
	truncated: boolean;
}

const encoder = new TextEncoder();

export function promptSections(prompt: RunPrompt | null | undefined): PromptSection[] {
	return (prompt?.sources ?? [])
		.map((part, index) => ({
			key: `${index}-${part.source}`,
			source: part.source,
			channel: part.channel,
			text: part.text ?? '',
			bytes: encoder.encode(part.text ?? '').length,
			truncated: !!part.truncated
		}))
		.filter((section) => section.text.trim() !== '');
}

export function formatSize(bytes: number): string {
	if (bytes < 1000) return `${bytes} B`;
	const kilobytes = bytes / 1000;
	return `${kilobytes < 10 ? Math.round(kilobytes * 10) / 10 : Math.round(kilobytes)} kB`;
}

/** A step's runs, told apart by the step and their place in it: "Implement · 2". */
export function runLabels(step: string, runs: { id: string }[]): string[] {
	return runs.map((_, index) => [step, String(index + 1)].filter(Boolean).join(' · '));
}
