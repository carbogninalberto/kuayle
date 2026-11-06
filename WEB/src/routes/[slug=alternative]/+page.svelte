<script lang="ts">
	import AlternativePage from '$lib/components/AlternativePage.svelte';
	import { contentModifiedAt } from '$lib/config/site';
	import { alternativePath } from '$lib/data/competitor-types';

	let { data } = $props();
</script>

<AlternativePage
	content={data.content}
	competitor={data.competitor}
	others={data.others}
	modifiedAt={contentModifiedAt(alternativePath(data.competitor))}
/>
