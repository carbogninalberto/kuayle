import { agentTasks } from '$lib/api/agent-tasks';
import { taskTraceFilename } from './task-trace-name';

// Downloads the task's full debug trace as one JSON file, using the file name
// the server chose (kuayle-trace-<identifier>.json) and a safe local fallback.
export async function downloadTaskTrace(
	slug: string,
	id: string,
	identifier?: string | null
): Promise<void> {
	const { blob, filename } = await agentTasks.debugTrace(slug, id);
	const url = URL.createObjectURL(blob);
	const link = document.createElement('a');
	link.href = url;
	link.download = filename ?? taskTraceFilename(identifier, id);
	link.click();
	setTimeout(() => URL.revokeObjectURL(url), 1000);
}
