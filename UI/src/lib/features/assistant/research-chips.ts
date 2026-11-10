import { FileSearch, FolderTree, Globe, Network } from '@lucide/svelte';
import { m } from '$lib/features/i18n/messages';
import { registerToolChip } from './tool-chips';
import RepoScanChip from './RepoScanChip.svelte';
import SubagentsChip from './SubagentsChip.svelte';
import WebSearchChip from './WebSearchChip.svelte';

// Step chips for the research tools (KUA-93). Imported once by ToolCalls.
registerToolChip('web_search', { label: m['assistant.tool_web_search'], icon: Globe, renderer: WebSearchChip });
registerToolChip('repo_list_files', {
	label: m['assistant.tool_repo_list_files'],
	icon: FolderTree,
	renderer: RepoScanChip
});
registerToolChip('repo_glob', { label: m['assistant.tool_repo_glob'], icon: FolderTree, renderer: RepoScanChip });
registerToolChip('repo_grep', { label: m['assistant.tool_repo_grep'], icon: FileSearch, renderer: RepoScanChip });
registerToolChip('repo_read_file', {
	label: m['assistant.tool_repo_read_file'],
	icon: FileSearch,
	renderer: RepoScanChip
});
registerToolChip('repo_symbols', { label: m['assistant.tool_repo_symbols'], icon: FileSearch, renderer: RepoScanChip });
registerToolChip('spawn_research_subagents', {
	label: m['assistant.tool_spawn_research_subagents'],
	icon: Network,
	renderer: SubagentsChip
});
