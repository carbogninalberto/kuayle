<script lang="ts">
	import { Circle, CircleCheck, CircleDot, Route } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import type { PlanStep, PlanStatus } from './agent-trace';
	import { m } from '$lib/paraglide/messages.js';
	let { steps }: { steps: PlanStep[] } = $props();
	const done = $derived(steps.filter((step) => step.status === 'done').length);
	const lineStatus = (status: PlanStatus) =>
		status === 'pending' ? 'pending' : status === 'active' ? 'running' : undefined;
</script>

<!-- Official AI Elements Plan composition, from the harness's own plan events. -->
<Trace.Root class="min-w-0">
	<Trace.Collapsible open>
		<Trace.CollapsibleTrigger>
			<Trace.Line status={done < steps.length ? 'running' : undefined}>
				<Trace.Icon><Route /></Trace.Icon>
				<Trace.Title>{m['tasks.plan']()}</Trace.Title>
				<Trace.Description>{m['tasks.plan_progress']({ done, total: steps.length })}</Trace.Description>
			</Trace.Line>
		</Trace.CollapsibleTrigger>
		<Trace.CollapsibleContent>
			<Trace.Root>
				{#each steps as step, index (index)}
					<Trace.Line status={lineStatus(step.status)}>
						<Trace.Icon
							>{#if step.status === 'done'}<CircleCheck />{:else if step.status === 'active'}<CircleDot />{:else}<Circle
								/>{/if}</Trace.Icon
						>
						<Trace.Title class="whitespace-normal break-words">{step.label}</Trace.Title>
					</Trace.Line>
				{/each}
			</Trace.Root>
		</Trace.CollapsibleContent>
	</Trace.Collapsible>
</Trace.Root>
