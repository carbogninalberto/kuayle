<script lang="ts">
	import MockFrame from './MockFrame.svelte';

	const teams = [
		{ name: 'Engineering', visibility: 'Public', members: 14 },
		{ name: 'Security', visibility: 'Private', members: 3 },
		{ name: 'Client: Northwind', visibility: 'Private', members: 5 }
	];
	const roles = ['Owner', 'Admin', 'Member', 'Guest'];
</script>

<MockFrame label="Illustration of Kuayle workspace settings with public and private teams and the owner, admin, member and guest roles." path="settings / teams">
	<div class="space-y-3 p-4 text-left text-xs">
		<div class="rounded-md border border-white/10">
			{#each teams as team, i}
				<div class="flex items-center justify-between px-3 py-2 {i > 0 ? 'border-t border-white/10' : ''}">
					<span class="text-foreground">{team.name}</span>
					<span class="flex items-center gap-3">
						<span class="font-mono text-[0.6rem] text-muted-foreground">{team.members} members</span>
						<span class="rounded border px-1.5 py-0.5 text-[0.6rem] {team.visibility === 'Private' ? 'border-brand-400/40 text-brand-200' : 'border-white/15 text-muted-foreground'}">{team.visibility}</span>
					</span>
				</div>
			{/each}
		</div>
		<div>
			<p class="text-[0.65rem] text-muted-foreground">Workspace roles</p>
			<div class="mt-1.5 flex flex-wrap gap-1.5">
				{#each roles as role}
					<span class="rounded-md border border-dashed border-white/15 px-2 py-1 text-foreground">{role}</span>
				{/each}
			</div>
		</div>
		<div class="rounded-md border border-white/10 bg-black/30 p-2 font-mono text-[0.65rem] text-muted-foreground">
			Personal access token · scopes: issues:read · expires in 90 days
		</div>
	</div>
</MockFrame>
