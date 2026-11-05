# KUA-67: diff panel controls and sidebar footer decision

The attempt diff panel (`UI/src/lib/features/assistant/DiffPanel.svelte`) shows the
aggregate total (file count, +additions, −deletions) in its header, and a toolbar
pinned above the scrolling file list holds search and collapse/expand-all.

## Decision: no fixed footer

- The panel is a narrow column beside the chat. A footer would take vertical space
  from the diff itself and sit far from the header summary the controls act on.
- Search and bulk collapse are used before reading, so a top toolbar (always visible
  because the list scrolls beneath it) is where the eye already is.
- The panel has too few controls to justify a second chrome region.

Revisit a footer if the panel gains several persistent actions (for example review
submit or file navigation).

## Behaviour rules

- Search filters files by path or line text and highlights matching lines; it never
  changes the collapsed set. A match count is shown while a query is active.
- Collapse/expand-all applies to the visible (filtered) files, and the button's
  label/icon reflect those files. It is disabled when nothing is visible.
- No matches shows an empty state with a Clear search action; the truncation warning
  stays visible there.

## Additional controls evaluated

| Control | Verdict |
| --- | --- |
| Next/previous match | Deferred. Needs scroll-to-line plus a cursor across collapsed files; the file filter and highlighting already narrow results. |
| Copy diff / copy path | Deferred; useful, low priority. |
| Wrap long lines | Deferred; horizontal scroll works today. |
| Files-only (hide hunks) view | Covered by collapse all. |
| Filter by added/removed/changed type | Deferred. |
