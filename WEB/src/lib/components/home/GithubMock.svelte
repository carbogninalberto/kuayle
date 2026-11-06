<script lang="ts">
	import MockFrame from './MockFrame.svelte';
</script>

<MockFrame label="Illustration of a GitHub pull request linked to issue ENG-207: the branch, commits and pull request appear on the issue, and merging moves it to Done." path="ENG-207 · activity">
	<div class="space-y-3 p-4 text-left text-xs">
		<ol class="space-y-2 border-l border-white/15 pl-4">
			<li class="relative"><span class="absolute top-1 -left-[1.3rem] size-2 rounded-full bg-zinc-400"></span><span class="font-mono text-muted-foreground">branch</span> <span class="text-foreground">kuayle/eng-207-carry-over</span> created → <span class="text-amber-200">In Progress</span></li>
			<li class="relative"><span class="absolute top-1 -left-[1.3rem] size-2 rounded-full bg-zinc-400"></span><span class="font-mono text-muted-foreground">commit</span> <span class="text-foreground">a1f3c9e</span> Keep sub-issues on carry-over</li>
			<li class="relative"><span class="absolute top-1 -left-[1.3rem] size-2 rounded-full bg-brand-300"></span><span class="font-mono text-muted-foreground">PR #142</span> <span class="text-foreground">opened</span> → <span class="text-brand-200">In Review</span></li>
			<li class="relative"><span class="absolute top-1 -left-[1.3rem] size-2 rounded-full bg-emerald-400"></span><span class="font-mono text-muted-foreground">PR #142</span> <span class="text-foreground">merged</span> → <span class="text-emerald-300">Done</span></li>
		</ol>
		<pre class="overflow-hidden rounded-md border border-white/10 bg-black/40 p-3 font-mono text-[0.65rem] leading-relaxed text-muted-foreground"><span class="text-brand-200">POST</span> /your-endpoint
X-Webhook-Signature: …
{`{ "event": "issue.updated", "payload": { … }, "timestamp": "…" }`}</pre>
	</div>
</MockFrame>
