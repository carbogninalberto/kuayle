---
name: kuayle-web
description: Maintain Kuayle WEB marketing pages, route registries, SEO, assets and responsive behavior without conflating the static site with the UI application.
---

# WEB maintenance

- Start with [WEB/README.md](../../../WEB/README.md) and the affected route or
  content registry. `WEB/` is a separate SvelteKit package with forced Svelte 5
  runes and static prerendering configured in [vite.config.ts](../../../WEB/vite.config.ts)
  and [layout](../../../WEB/src/routes/+layout.ts). Preserve static rendering;
  browser-only behavior must not break build-time rendering.
- Follow the shared [frontend component rules](../kuayle-frontend/SKILL.md).
  Reuse [WEB components](../../../WEB/src/lib/components), including its own button
  and badge primitives, and compose within WEB. Preserve the site's CSS tokens,
  responsive layouts, accessible controls and reduced-motion behavior.
- For new pages, update [route registry](../../../WEB/src/lib/data/routes.ts) and
  the applicable [content registry](../../../WEB/src/lib/data). Reuse PageLayout
  or StandalonePage, breadcrumbs and CTA components according to the existing
  page family. Keep sitemap entries, navigation and route metadata in sync.
- Use [site config](../../../WEB/src/lib/config/site.ts) and
  [Seo.svelte](../../../WEB/src/lib/components/Seo.svelte) for canonical URLs,
  metadata, social cards and structured data. Preserve crawlable prerendered
  content, a meaningful h1, and working internal links.
- Check [content modification data](../../../WEB/src/lib/data/content-modified.json)
  when changing indexed content. Keep comparison claims, reviewed dates and
  self-hosting instructions accurate against the implementation and
  [selfhosting files](../../../selfhosting). Verify external facts when updating
  them; do not fabricate claims or refresh dates without reviewing content.
- Keep image dimensions and descriptive alt text, appropriate responsive assets,
  and the existing social-card contract. Do not include generated build output.
  [Release display](../../../WEB/src/lib/release.svelte.ts) consumes the app's
  release manifest; changing marketing content does not authorize a release or
  version bump.
- Run the WEB checks in the [testing skill](../kuayle-testing/SKILL.md), including
  build before SEO and responsive tests. If routes change, inspect the responsive
  test's expected sitemap count and update it to the intended set; retain its
  overflow assertion. Review affected pages on desktop and narrow mobile widths.
  Update WEB/README.md and this skill when their conventions change.
