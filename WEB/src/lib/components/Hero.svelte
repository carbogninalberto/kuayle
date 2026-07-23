<script lang="ts">
  import { Button } from "$lib/components/ui/button";
  import ArrowRight from "@lucide/svelte/icons/arrow-right";
  import Github from "$lib/components/GithubIcon.svelte";
  import ScrollText from "@lucide/svelte/icons/scroll-text";
  import Users from "@lucide/svelte/icons/users";
  import Server from "@lucide/svelte/icons/server";
  import Terminal from "@lucide/svelte/icons/terminal";
  import { DEV_MACHINES_RELEASE_STATUS } from "$lib/config/releases";
  import { useLatestRelease } from "$lib/release.svelte";

  const release = useLatestRelease();

  const stats = [
    { icon: ScrollText, label: "Apache 2.0 license" },
    { icon: Users, label: "$0 per user" },
    { icon: Server, label: "Self-hosted" },
    { icon: Terminal, label: `Dev Machines · ${DEV_MACHINES_RELEASE_STATUS}` }
  ];
</script>

<section class="relative overflow-hidden pt-36 pb-20 sm:pt-44">
  <!-- Backdrop: grid + layered glow -->
  <div class="hero-grid absolute inset-0 -z-10"></div>
  <div
    class="animate-glow absolute -top-40 left-1/2 -z-10 h-[520px] w-[820px] -translate-x-1/2 rounded-full bg-brand-400/25 blur-[140px]"
  ></div>
  <div
    class="absolute top-24 -left-40 -z-10 h-[320px] w-[420px] rounded-full bg-brand-600/15 blur-[120px]"
  ></div>
  <div
    class="absolute top-40 -right-40 -z-10 h-[320px] w-[420px] rounded-full bg-brand-500/10 blur-[120px]"
  ></div>

  <div class="mx-auto max-w-6xl px-6 text-center">
    <!-- Release badge -->
    <div class="animate-fade-up flex justify-center" style="animation-delay: 0ms">
      <a
        href={release.releaseUrl}
        target="_blank"
        rel="noopener"
        class="group inline-flex items-center gap-2.5 rounded-full border border-brand-400/30 bg-brand-400/10 py-1.5 pr-4 pl-2.5 text-xs font-medium text-brand-200 transition-colors hover:border-brand-400/50 hover:bg-brand-400/15"
      >
        <span class="relative flex size-2">
          <span class="animate-pulse-dot absolute inline-flex size-2 rounded-full bg-brand-300"></span>
        </span>
        {release.version} is public — Apache 2.0
        <ArrowRight class="size-3 transition-transform group-hover:translate-x-0.5" />
      </a>
    </div>

    <h1
      class="animate-fade-up mx-auto mt-6 max-w-3xl text-5xl leading-[1.05] font-bold tracking-tight sm:text-6xl md:text-7xl"
      style="animation-delay: 100ms"
    >
      The issue tracker with <span class="gradient-text">no paid tier</span>
    </h1>

    <p
      class="animate-fade-up mx-auto mt-6 max-w-2xl text-lg text-muted-foreground sm:text-xl"
      style="animation-delay: 200ms"
    >
      Run Kuayle on your own infrastructure. Manage multi-assignee issues, cycles,
      projects, analytics and GitHub activity from one Apache 2.0-licensed
      codebase. Unreleased opt-in Dev Machines are documented separately from the public release.
    </p>

    <div
      class="animate-fade-up mt-10 flex flex-wrap items-center justify-center gap-3"
      style="animation-delay: 300ms"
    >
      <Button
        href="https://github.com/carbogninalberto/kuayle"
        target="_blank"
        rel="noopener"
        size="lg"
        class="h-11 bg-brand-400 px-6 text-base text-white shadow-lg shadow-brand-400/30 hover:bg-brand-500"
      >
        <Github />
        View source
      </Button>
      <Button
        href="/self-hosting"
        variant="outline"
        size="lg"
        class="h-11 px-6 text-base"
      >
        Self-host Kuayle
        <ArrowRight />
      </Button>
    </div>

    <!-- Quick facts -->
    <div
      class="animate-fade-up mt-8 flex flex-wrap items-center justify-center gap-2"
      style="animation-delay: 400ms"
    >
      {#each stats as stat (stat.label)}
        <span
          class="inline-flex items-center gap-1.5 rounded-full border border-border bg-card/60 px-3 py-1.5 text-xs text-muted-foreground"
        >
          <stat.icon class="size-3.5 text-brand-300" />
          {stat.label}
        </span>
      {/each}
    </div>

    <p
      class="animate-fade-up mt-6 text-sm text-muted-foreground"
      style="animation-delay: 500ms"
    >
      Press <kbd class="key h-6! min-w-6! text-xs">C</kbd> to create an issue.
      <kbd class="key h-6! min-w-6! text-xs">⌘</kbd><kbd
        class="key h-6! min-w-6! text-xs">K</kbd
      >
      to search issues and navigate.
    </p>

    <!-- Product screenshot -->
    <div
      class="animate-fade-up relative mx-auto mt-16 max-w-5xl"
      style="animation-delay: 600ms"
    >
      <div
        class="animate-glow absolute -inset-6 -z-10 rounded-3xl bg-gradient-to-tr from-brand-600/40 via-brand-400/25 to-transparent blur-2xl"
      ></div>
      <div
        class="animate-float ring-gradient relative overflow-hidden rounded-2xl border border-white/10 bg-card shadow-2xl shadow-black/60"
      >
        <div
          class="flex items-center gap-1.5 border-b border-white/5 bg-black/40 px-4 py-3"
        >
          <span class="size-3 rounded-full bg-[#ff5f57]/80"></span>
          <span class="size-3 rounded-full bg-[#febc2e]/80"></span>
          <span class="size-3 rounded-full bg-[#28c840]/80"></span>
          <span
            class="ml-3 hidden h-5 flex-1 items-center rounded-md bg-white/5 px-2 font-mono text-[10px] text-muted-foreground sm:flex"
          >
            kuayle.yourcompany.com
          </span>
        </div>
        <div class="relative overflow-hidden">
          <div class="beam"></div>
          <img
            src="/product-screenshot.png"
            srcset="/product-screenshot-720.png 720w, /product-screenshot-1440.png 1440w, /product-screenshot.png 3600w"
            sizes="(max-width: 640px) 100vw, (max-width: 1200px) 92vw, 1152px"
            alt="Kuayle issue tracker interface showing the board view with issues"
            width="3600"
            height="1886"
            class="w-full h-auto"
            loading="eager"
            fetchpriority="high"
          />
        </div>
      </div>
    </div>
  </div>
</section>
