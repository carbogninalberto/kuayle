import type { Component } from 'svelte';
import type { ConversationToolCall } from '$lib/api/conversations';
import { m } from '$lib/paraglide/messages.js';
import { humanizeToolName } from './shell-rules';

// How one tool call is shown as a step chip in the transcript.
//
// Every tool call the backend records is rendered: a name without an entry
// here still gets a generic chip (a readable label derived from its name,
// the input and the result), so new backend tools appear automatically.
// A tool can register a friendlier label, an icon, or a whole renderer that
// replaces the generic chip body:
//
//   registerToolChip('web_search', {
//     label: () => m['assistant.tool_web_search'](),
//     icon: Globe,
//     renderer: WebSearchChip // receives { call, slug }
//   });
//
// Renderers only display recorded data; tool input and output are untrusted
// and must be shown as escaped text, never as HTML.
export type ToolChipRendererProps = { call: ConversationToolCall; slug: string };
export interface ToolChip {
	label?: () => string;
	icon?: Component;
	renderer?: Component<ToolChipRendererProps>;
}

const builtIn: Record<string, () => string> = {
	list_teams: m['assistant.tool_list_teams'],
	search_issues: m['assistant.tool_search_issues'],
	get_issue: m['assistant.tool_get_issue'],
	create_issue: m['assistant.tool_create_issue'],
	update_issue: m['assistant.tool_update_issue'],
	delegate_issue: m['assistant.tool_delegate_issue'],
	plan_issues: m['assistant.tool_plan_issues'],
	link_issues: m['assistant.tool_link_issues'],
	get_agent_work: m['assistant.tool_get_agent_work'],
	merge_pull_request: m['assistant.tool_merge_pull_request'],
	accept_agent_work: m['assistant.tool_accept_agent_work'],
	request_agent_review: m['assistant.tool_request_agent_review'],
	follow_up_task: m['assistant.tool_follow_up_task'],
	start_preview: m['assistant.tool_start_preview'],
	stop_preview: m['assistant.tool_stop_preview'],
	expose_port: m['assistant.tool_expose_port'],
	list_ports: m['assistant.tool_list_ports'],
	run_check: m['assistant.tool_run_check'],
	message_task: m['assistant.tool_message_task'],
	get_task_messages: m['assistant.tool_get_task_messages'],
	load_skill: m['assistant.tool_load_skill'],
	propose_skill: m['assistant.tool_propose_skill'],
	propose_tool: m['assistant.tool_propose_tool'],
	get_tool_run: m['assistant.tool_get_tool_run'],
	get_instance_version: m['assistant.tool_get_instance_version'],
	upgrade_instance: m['assistant.tool_upgrade_instance'],
	get_pull_request: m['assistant.tool_get_pull_request'],
	get_pull_request_checks: m['assistant.tool_get_pull_request_checks'],
	list_pull_request_comments: m['assistant.tool_list_pull_request_comments'],
	reply_to_review_comment: m['assistant.tool_reply_to_review_comment'],
	resolve_review_thread: m['assistant.tool_resolve_review_thread'],
	update_pull_request_branch: m['assistant.tool_update_pull_request_branch'],
	mark_pull_request_ready: m['assistant.tool_mark_pull_request_ready'],
	list_releases: m['assistant.tool_list_releases'],
	get_release: m['assistant.tool_get_release'],
	draft_release_notes: m['assistant.tool_draft_release_notes'],
	create_release: m['assistant.tool_create_release'],
	get_workflow_run: m['assistant.tool_get_workflow_run'],
	ship_pull_request: m['assistant.tool_ship_pull_request']
};

const registry = new Map<string, ToolChip>();

/** Registers (or replaces) how the tool named `name` is shown. */
export function registerToolChip(name: string, chip: ToolChip) {
	registry.set(name, chip);
}

export function toolChip(name: string): ToolChip | undefined {
	return registry.get(name);
}

export function toolLabel(name: string) {
	const registered = registry.get(name)?.label;
	if (registered) return registered();
	const known = builtIn[name];
	// Custom tools keep their exact name: it is what an admin configured.
	if (known) return known();
	return name.startsWith('custom_') ? name : humanizeToolName(name);
}
