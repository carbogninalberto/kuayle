import { agentInfo } from '$lib/features/agents/agents';
import { nodeTypeLabel } from '$lib/features/workflows/labels';
import type { TimelineStep } from './agent-timeline';

// How a step of a task's timeline is named, wherever it is shown.

type Named = Pick<TimelineStep, 'label' | 'kind' | 'nodeType' | 'harness' | 'profile'>;

const harnessName = (step: Named) => (step.harness ? agentInfo(step.harness).name : '');

/** The step's name in its workflow, else the harness that ran it or its kind. */
export function stepTitle(step: Named): string {
	return step.label || (step.kind === 'agent' && harnessName(step)) || nodeTypeLabel(step.nodeType);
}

/** Who ran it: the profile, or the harness when the step is not named after it. */
export function stepAgent(step: Named): string {
	return step.profile ?? (step.label ? harnessName(step) : '');
}
