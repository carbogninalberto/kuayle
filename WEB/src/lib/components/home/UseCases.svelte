<script lang="ts">
	import { reveal } from '$lib/actions/reveal';

	const cases = [
		{
			who: 'Software teams',
			setup: 'Team statuses · cycles · GitHub App',
			body: 'Branches, commits and pull requests attach to issues and move them through your statuses. Cycles carry unfinished work forward.'
		},
		{
			who: 'Agencies and studios',
			setup: 'Private team per client',
			body: 'Give each client a private team and invite guests where needed. Private teams turn off sharing features such as public links in that workspace, so use a separate workspace when you need both.'
		},
		{
			who: 'Regulated and on-prem teams',
			setup: 'Your server · your PostgreSQL',
			body: 'Issues, comments and files stay in your own database and storage, inside your network and backup policy.'
		},
		{
			who: 'Solo founders',
			setup: 'Opt-in Dev Machines',
			body: 'Plan the work, then run Claude Code, Codex or opencode on an issue worktree in a container you host.'
		},
		{
			who: 'Open source maintainers',
			setup: 'Public share links · triage inbox',
			body: 'Triage incoming issues, share read-only links to selected issues, and pay nothing as contributors join.'
		},
		{
			who: 'Growing companies',
			setup: 'Multiple workspaces · roles',
			body: 'Add people without a seat bill. Separate workspaces per business unit on one instance, with owner, admin, member and guest roles.'
		}
	];
</script>

<section id="who" class="py-20 sm:py-28" aria-labelledby="who-heading">
	<div class="mx-auto max-w-6xl px-6">
		<div class="max-w-2xl" use:reveal>
			<p class="font-mono text-xs tracking-widest text-brand-300 uppercase">Who it’s for</p>
			<h2 id="who-heading" class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">Built for teams that want to own their tracker</h2>
		</div>
		<ul class="mt-10 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
			{#each cases as item, i}
				<li class="rounded-xl border border-border bg-card/50 p-5" use:reveal={{ delay: i * 40 }}>
					<p class="font-mono text-[0.7rem] text-muted-foreground">{item.setup}</p>
					<h3 class="mt-2 font-semibold">{item.who}</h3>
					<p class="mt-2 text-sm leading-relaxed text-muted-foreground">{item.body}</p>
				</li>
			{/each}
		</ul>
	</div>
</section>
