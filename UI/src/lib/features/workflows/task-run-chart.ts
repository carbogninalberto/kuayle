import type { PipelineDefinition, TaskWorkflow, WorkflowGraph } from '$lib/api/workflows';
import { pipelineGraph } from './pipeline-editor.ts';
import { leftToRight, runChartGraph, runOverlay } from './workflow-graph.ts';

/** The workflow endpoint returns the current phase in the same node field for lead- and tick-driven pipelines. */
export function taskRunChart(run: Omit<TaskWorkflow, 'graph'> & { graph: WorkflowGraph | PipelineDefinition }) {
	const graph = run.graph.kind === 'pipeline' ? pipelineGraph(run.graph) : leftToRight(run.graph);
	const overlay = runOverlay({ ...run, graph });
	return { graph: runChartGraph(graph, overlay), overlay };
}
