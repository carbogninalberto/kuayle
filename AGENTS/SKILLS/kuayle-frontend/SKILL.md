---
name: kuayle-frontend
description: Implement Kuayle application UI and reusable Svelte components using the existing shadcn-svelte foundation; also supplies shared component rules for WEB.
---

# Frontend implementation

## Choose and compose components

1. Inspect the nearest feature and search [UI features](../../../UI/src/lib/features),
   [shared components](../../../UI/src/lib/components/shared), and
   [installed primitives](../../../UI/src/lib/components/ui). Prefer a suitable
   existing component over a duplicate. Check its actual implementation: legacy
   custom dialogs and controls are not templates for new handcrafted controls.
2. Use the local shadcn-svelte exports, e.g. `import { Button } from
   '$lib/components/ui/button/index.js'`; inspect each `index.ts` for available
   exports and the installed component for its props. Do not assume React shadcn
   APIs or replace Bits UI keyboard/focus behavior with custom event handling.
3. Compose product-specific components in the relevant feature directory or
   `src/lib/components/shared/`. Keep `src/lib/components/ui/` for primitives.
   When modifying a legacy control, use the applicable primitive within the task's
   scope and test preserved behavior; do not launch an unrelated bulk migration.
4. If no primitive/composition fits (for example an editor or terminal), record the
   missing capability and reason in the PR and near a reusable custom component.
   Reuse primitives for surrounding controls. Ordinary headings, paragraphs,
   sections, lists and content links remain semantic HTML. A primitive's own
   internal `<button>`/`<input>` implementation is expected.

Use [SelectField](../../../UI/src/lib/components/shared/SelectField.svelte) for
simple typed option lists. Forward trigger props through `child` snippets when
composing Button with Popover/Select/Dialog; never nest buttons. Shared issue
selectors accept a trigger snippet with those props. Run the
[control audit](../../../UI/scripts/audit-controls.mjs) after a control migration;
review [documented editor and terminal exceptions](../../../docs/kua-62-controls.md).

## Add a missing primitive

- The application is already initialized: [UI/components.json](../../../UI/components.json)
  selects Nova, zinc, TypeScript, and `$lib` aliases. Do not rerun `init` over its CSS.
- From `UI/`, use `npx shadcn-svelte@latest add <component>` only after checking the
  [official CLI documentation](https://www.shadcn-svelte.com/docs/cli) and compatibility
  with the installed Svelte/Tailwind versions. Add only the required component;
  inspect generated code and dependency/lockfile changes before committing.
  Avoid `--all` and indiscriminate overwrites of customized primitives.
- WEB has its own [configuration](../../../WEB/components.json),
  [primitives](../../../WEB/src/lib/components/ui) and [package](../../../WEB/package.json).
  Run the CLI from `WEB/` for that package. Do not import files across package roots.
  Configuration fields follow the [official reference](https://www.shadcn-svelte.com/docs/components-json).

## Integrate with Kuayle

- Use Svelte 5 runes (`$props`, `$state`, `$derived`, `$effect` where needed), typed
  props, snippets and event attributes consistent with adjacent code. Preserve
  component bindings and avoid effects that merely duplicate derived state.
- Reuse `cn` from the package's `src/lib/utils.ts`, primitive variants and semantic
  Tailwind tokens. [UI theme CSS](../../../UI/src/app.css) bridges app variables
  and shadcn tokens across themes; [WEB CSS](../../../WEB/src/app.css) owns the site
  palette. Check the affected theme variants; do not hardcode replacement colors.
- Keep requests in [UI API modules](../../../UI/src/lib/api) using the shared
  [client](../../../UI/src/lib/api/client.ts). Inspect feature `.svelte.ts` state,
  refresh/realtime handling and API types before changing contracts.
- Translate new app strings through [message catalogs](../../../UI/messages);
  generated `src/lib/paraglide/` is produced by tooling, not edited by hand.
- Preserve semantic labels, accessible icon-button names, visible keyboard focus,
  dialog focus return/Escape behavior, and disabled/pending states. Use button
  semantics for actions and links for navigation; avoid nested interactive elements.
- Define the changed behavior first; test relevant loading, empty, success, failure,
  validation, denied-access, keyboard and responsive states using the
  [testing skill](../kuayle-testing/SKILL.md). UI checks never replace API permissions.
