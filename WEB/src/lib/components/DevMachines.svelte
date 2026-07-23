<script lang="ts">
	import { reveal } from '$lib/actions/reveal';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import Check from '@lucide/svelte/icons/check';
	import Globe from '@lucide/svelte/icons/globe';
	import CodeXml from '@lucide/svelte/icons/code-xml';
	import SquareTerminal from '@lucide/svelte/icons/square-terminal';
	import Bot from '@lucide/svelte/icons/bot';
	import { DEV_MACHINES_RELEASE_STATUS } from '$lib/config/releases';

	const bullets = [
		'code-server IDE and a native xterm terminal in your browser',
		'Claude Code, OpenCode, Codex or custom CLI agents on issue worktrees',
		'Authenticated gateway routing — no public ports, one-time launch tickets',
		'Workspace policies for concurrency, runtime, providers and idle pause'
	];

	const services = [
		{ icon: CodeXml, name: 'IDE', detail: 'code-server :8080' },
		{ icon: SquareTerminal, name: 'Terminal', detail: 'ttyd · tmux' },
		{ icon: Globe, name: 'Browser', detail: 'Chrome · KasmVNC' },
		{ icon: Bot, name: 'Agent', detail: 'opencode · autonomous' }
	];

	const logLines = [
		{ text: 'worktree ready /workspace/tasks/ENG-123', type: 'ok' },
		{ text: 'agent container started · opencode', type: 'ok' },
		{ text: 'editing BE/internal/handler/issue.go', type: 'run' },
		{ text: 'go test ./... — passed', type: 'run' },
		{ text: 'pushed kuayle/eng-123 → PR #42 opened', type: 'ok' }
	];
</script>

<section id="dev-machines" class="relative overflow-hidden py-24 sm:py-32">
	<div class="absolute top-1/3 right-0 -z-10 h-[420px] w-[560px] rounded-full bg-brand-600/15 blur-[140px]"></div>

	<div class="mx-auto max-w-6xl px-6">
		<div class="grid items-center gap-14 lg:grid-cols-2">
			<div use:reveal class="min-w-0">
				<p class="text-sm font-semibold tracking-widest text-brand-300 uppercase">
					Dev Machines · {DEV_MACHINES_RELEASE_STATUS}
				</p>
				<h2 class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">Agentic coding, on your own metal</h2>
				<p class="mt-5 text-lg leading-relaxed text-muted-foreground">
					Create an isolated multi-container environment that can combine a developer workspace, browser and on-demand
					agent runs. Machines can attach issue worktrees. Agent runs can report commits and pull requests in their
					normalized results; manual IDE and terminal commits are not automatically attached to issues.
				</p>
				<ul class="mt-7 space-y-3.5">
					{#each bullets as bullet (bullet)}
						<li class="flex items-start gap-3 text-sm leading-relaxed text-muted-foreground">
							<Check class="mt-0.5 size-4 shrink-0 text-brand-300" />
							<span>{bullet}</span>
						</li>
					{/each}
				</ul>
				<div class="mt-8 flex flex-wrap items-center gap-4">
					<a
						href="/features/dev-machines"
						class="inline-flex items-center gap-1.5 text-sm font-medium text-brand-300 transition-colors hover:text-brand-200"
					>
						How Dev Machines work <ArrowRight class="size-3.5" />
					</a>
					<span class="text-xs text-muted-foreground">
						{DEV_MACHINES_RELEASE_STATUS} · opt-in subsystem · disabled by default ·
						<a href="/self-hosting/dev-machines" class="underline underline-offset-4 hover:text-foreground"
							>setup guide</a
						>
					</span>
				</div>
			</div>

			<!-- Machine dashboard mock -->
			<div use:reveal={{ delay: 150 }} class="relative min-w-0">
				<div
					class="animate-glow absolute -inset-4 -z-10 rounded-3xl bg-gradient-to-bl from-brand-600/30 via-brand-400/15 to-transparent blur-2xl"
				></div>
				<div
					class="ring-gradient noise-overlay overflow-hidden rounded-2xl border border-white/10 bg-[#0c0c12] shadow-2xl shadow-black/60"
				>
					<!-- Machine header -->
					<div class="flex items-center justify-between gap-3 border-b border-white/5 px-5 py-3.5">
						<div class="flex min-w-0 items-center gap-2.5">
							<span class="relative flex size-2 shrink-0">
								<span class="animate-pulse-dot absolute size-2 rounded-full bg-emerald-400"></span>
							</span>
							<span class="truncate font-mono text-sm text-foreground">amber-falcon</span>
							<span
								class="hidden shrink-0 rounded-full border border-white/10 bg-white/5 px-2 py-0.5 text-[10px] text-muted-foreground min-[480px]:inline-flex"
								>medium · 4 vCPU · 8 GB</span
							>
						</div>
						<span
							class="shrink-0 rounded-full border border-emerald-400/25 bg-emerald-400/10 px-2.5 py-0.5 text-[10px] font-medium text-emerald-300"
							>running</span
						>
					</div>

					<!-- Services -->
					<div class="grid grid-cols-2 gap-px bg-white/5">
						{#each services as service (service.name)}
							<div class="flex items-center gap-3 bg-[#0c0c12] px-5 py-3.5">
								<service.icon class="size-4 shrink-0 text-brand-300" />
								<div class="min-w-0">
									<p class="text-xs font-medium text-foreground">{service.name}</p>
									<p class="truncate font-mono text-[10px] text-muted-foreground">{service.detail}</p>
								</div>
								<span class="ml-auto size-1.5 shrink-0 rounded-full bg-emerald-400/80"></span>
							</div>
						{/each}
					</div>

					<!-- Agent run log -->
					<div class="border-t border-white/5 bg-black/50 px-5 py-4">
						<p class="mb-2.5 font-mono text-[10px] tracking-widest text-muted-foreground uppercase">
							agent run · eng-123
						</p>
						<div class="space-y-1.5 font-mono text-[11px] leading-relaxed">
							{#each logLines as line, i (line.text)}
								<p class="log-line flex items-center gap-2" style="animation-delay: {400 + i * 350}ms">
									{#if line.type === 'ok'}
										<Check class="size-3 shrink-0 text-emerald-400" />
										<span class="truncate text-emerald-200/80">{line.text}</span>
									{:else}
										<ArrowRight class="size-3 shrink-0 text-brand-300" />
										<span class="truncate text-muted-foreground">{line.text}</span>
									{/if}
								</p>
							{/each}
							<p class="log-line flex items-center gap-2" style="animation-delay: {400 + logLines.length * 350}ms">
								<span
									class="ml-5 inline-block h-3.5 w-1.5 bg-brand-300"
									style="animation: caret-blink 1.2s step-end infinite"
								></span>
							</p>
						</div>
					</div>

					<!-- Routing footer -->
					<div class="border-t border-white/5 px-5 py-3">
						<p class="truncate font-mono text-[10px] text-muted-foreground">
							<span class="text-brand-300/80">0123456789abcdef0123</span>.kuayle-machines.example.net — routed via
							Machine Gateway
						</p>
					</div>
				</div>
			</div>
		</div>
	</div>
</section>
