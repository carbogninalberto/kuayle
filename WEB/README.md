# Kuayle Marketing Site (WEB)

Static marketing/landing site for [Kuayle](https://kuayle.com), built with SvelteKit and the static adapter.

## Canonical origin

`https://kuayle.com` — configured centrally in `src/lib/config/site.ts`.

## Stack

- **SvelteKit** (prerendered static, `@sveltejs/adapter-static`)
- **Svelte 5** (runes mode)
- **Tailwind CSS v4**
- **shadcn-svelte** primitives (button, badge)
- **Lucide** icons

Agent implementation guidance lives in the repository's [WEB skill](../AGENTS/SKILLS/kuayle-web/SKILL.md).
The existing shadcn-svelte primitives are configured by [components.json](components.json);
run component additions from this package and preserve the site's custom theme.

## Scripts

| Command              | What it does                                           |
| -------------------- | ------------------------------------------------------ |
| `npm run dev`        | Start dev server                                       |
| `npm run build`      | Prerender static site to `build/`                      |
| `npm run preview`    | Preview the production build                           |
| `npm run check`      | Run `svelte-check` for type errors                     |
| `npm run validate-seo` | Run SEO validation on `build/` output               |
| `npm run validate`   | Build + validate (CI-ready)                            |
| `npm run test:unit`  | Node unit tests for the competitor registry, cost maths and release summaries |
| `npm run test:responsive` | Playwright checks: every route fits 320 px, navigation menus and sheet by keyboard, cookie controls, pricing calculator |

## SEO conventions

- **Centralized config**: `src/lib/config/site.ts` — canonical origin, defaults, `url()` helper.
- **Reusable SEO component**: `src/lib/components/Seo.svelte` — title, description, canonical, robots, full OG/Twitter metadata, JSON-LD.
- **JSON-LD**: Organization, WebSite, SoftwareApplication on homepage; BreadcrumbList + WebPage on all child pages; FAQPage on alternative pages, `/compare` and the Jira guide (questions must also be visible on the page).
- **Sitemap**: Generated at build time via `src/routes/sitemap.xml/+server.ts` using the route registry.
- **robots.txt**: Declares the sitemap location.
- **Image optimization**: All `<img>` tags carry intrinsic `width`/`height` and descriptive `alt`.

## Content conventions

- **Route registry**: `src/lib/data/routes.ts` — source of truth for sitemap entries, hubs, breadcrumbs, and shared metadata.
- **Content registries**: `src/lib/data/features.ts`, `self-hosting.ts`, `alternatives.ts` (roundups) — data-driven content for dynamic routes.
- **Competitor inventory**: `src/lib/data/competitors.ts` (28 tools, types and helpers in `competitor-types.ts`). Every external fact needs a first-party source (vendor pricing, docs or licence file) with its check date; unverifiable fields stay `null` and render as "Not documented". Tier A tools get an alternative page; Tier B appear only in matrices.
- **Kuayle facts**: `src/lib/data/kuayle-facts.ts` is the single source for Kuayle's side of every comparison and the homepage feature grid, verified against the code at the version it names. State gaps as gaps.
- **Alternative pages**: `/<competitor>-alternative` (param matcher `src/params/alternative.ts`), content in `alternative-pages.ts`, rendered by `AlternativePage.svelte`. Titles follow "Open Source <X> Alternative, Self-Hosted and Free (2026) | Kuayle" and work target keywords into H1, H2s and FAQ questions without stuffing.
- **Moved URLs**: `src/lib/data/moved-pages.ts` keeps prerendered stubs (canonical + immediate refresh) for old URLs; replace with host-level 301s when the hosting platform supports them.
- **Navigation**: navbar dropdowns, mobile sheet and footer columns are built from `src/lib/data/navigation.ts`, which reads the route registry.
- **Release version**: `src/lib/release.svelte.ts` — `useLatestRelease()` fetches the `UI/static/releases.json` manifest from the repository's main branch at runtime (same mechanism as the app). Static HTML renders `FALLBACK_VERSION`; bump it when the fallback becomes stale.
- **Silo architecture**: Features and Self-Hosting hubs have child pages using `PageLayout.svelte`. The Alternatives hub lists the root-level alternative pages, the Jira guide and the roundups; `/compare` is the full matrix.
- **Standalone pages**: `/open-source`, `/license`, `/security`, `/about`, `/roadmap` — each uses `StandalonePage.svelte` wrapper.
- **Breadcrumbs**: Every page includes a `<Breadcrumbs>` component with structured data.
- **CTA**: Every content page ends with a `<CtaSection>`.
- **Comparisons**: Neutral language, methodology notes, and separate visible dates for Kuayle details (verified against v0.2.15) and competitor details (checked November 6, 2026). Re-check sources before changing a reviewed date.
- **Self-hosting pages**: Aligned with the actual README and `selfhosting/` configs. No secrets exposed.

## Route map

```
/                                              Homepage
/features                                      Features hub
/features/[slug]                               Feature detail (10 pages)
/self-hosting                                  Self-hosting hub
/self-hosting/[slug]                           Self-hosting guide (8 pages)
/compare                                       Full comparison matrix (28 tools)
/compare/[slug]                                Moved-page stubs for the old kuayle-vs-* URLs (not in sitemap)
/alternatives                                  Alternatives hub
/alternatives/[slug]                           Roundups with matrices (2)
/[slug=alternative]                            Alternative pages, e.g. /linear-alternative (11)
/jira-data-center-end-of-life                  Jira Data Center EOL guide
/open-source                                   Open source philosophy
/license                                       Apache 2.0 license details
/security                                      Security practices
/about                                         About Kuayle / Bakney
/roadmap                                       Development roadmap
/privacy                                       Privacy policy
/sitemap.xml                                   XML sitemap
```

## Build output

All routes prerendered to static HTML in `build/`. The `strict: true` adapter option ensures missing pages fail the build.

## SEO validation

The script at `scripts/validate-seo.mjs` checks:

1. Every route produces an HTML file
2. `<title>`, `<meta name="description">`, `<link rel="canonical">` on every page
3. `<h1>` on every page
4. All indexable routes appear in `sitemap.xml`
5. JSON-LD blocks are valid JSON
6. Images have `alt` and intrinsic `width`/`height`
7. Internal links resolve to existing files
8. Bare fragment links (`#...`) on non-homepages are flagged
9. Moved-page stubs are canonical to, refresh to and link to their new URL
10. FAQ pages publish FAQPage JSON-LD whose questions are visible on the page

The homepage hero and chapters use HTML product mocks with sample data (`src/lib/components/home/`); they are `aria-hidden` with a text alternative and must only show behaviour the app really has. The homepage GitHub star count is fetched once at build time (`src/routes/+page.server.ts`) and hidden when unavailable. The legacy homepage screenshot has responsive 720 px and 1440 px variants. The product tour uses `static/tour-*-{720,1440,2160}.webp` (16:10). Social metadata uses the dedicated 1200 × 630 `static/social-card.png` asset.

Screenshots are captured from a local instance (`make dev` + `make seed`, dark theme, signed in as the seeded owner) with Playwright, then resized and optimized at the same pixel dimensions.
