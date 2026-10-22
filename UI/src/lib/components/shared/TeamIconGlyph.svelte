<script lang="ts">
	import * as LucideIcons from '@lucide/svelte';
	import type { LucideIcon, LucideProps } from '@lucide/svelte';
	import { legacyTeamIconNodes } from './legacy-team-icons';

	let { name, ...props }: LucideProps & { name: string } = $props();
	const iconMap = LucideIcons as unknown as Record<string, LucideIcon>;
	const legacyNode = $derived(Object.hasOwn(legacyTeamIconNodes, name) ? legacyTeamIconNodes[name] : undefined);
	const Icon = $derived(typeof iconMap[name] === 'function' ? iconMap[name] : LucideIcons.SquareUser);
</script>

{#if legacyNode}
	<LucideIcons.Icon iconNode={legacyNode} {...props} />
{:else}
	<Icon {...props} />
{/if}
