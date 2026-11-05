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

- Search filters files by path or line text and highlights matching lines. A match
  count is shown while a query is active.
- While a query is active, matching files are shown expanded so the highlights are
  visible, even files the user collapsed. A file collapsed during a search stays
  collapsed only until the query changes; clearing the query restores the user's own
  collapsed set unchanged.
- Collapse/expand-all applies to every file and is disabled while a query is
  active, so it never changes hidden state the user cannot see.
- No matches shows an empty state with a Clear search action.
- A truncated diff only holds a prefix: the header totals read "At least N files"
  and are described as partial, the truncation warning stays visible, and an empty
  search says it covered only the loaded part of the diff.

## Additional controls evaluated

| Control | Verdict |
| --- | --- |
| Next/previous match | Deferred. Needs scroll-to-line plus a cursor across collapsed files; the file filter and highlighting already narrow results. |
| Copy diff / copy path | Deferred; useful, low priority. |
| Wrap long lines | Deferred; horizontal scroll works today. |
| Files-only (hide hunks) view | Covered by collapse all. |
| Filter by added/removed/changed type | Deferred. |
