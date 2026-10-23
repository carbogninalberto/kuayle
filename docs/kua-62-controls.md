# KUA-62 control migration audit

Scope: every Svelte route, feature and shared component in `UI/src` and `WEB/src`, including controls already implemented with shadcn-svelte. Baseline: `089eeb1`. The table records the original locations; line numbers refer to that baseline.

## Component choices and preserved contracts

- Text, search, password, numeric and color fields use Input; multiline fields use Textarea. Preserve binding, validation, maximum lengths, pending/disabled state and input events. Password reveal composes Input and Button and is keyboard reachable.
- Native selects become SelectField, a composition of Select.Root/Trigger/Value/Content/Item. It preserves typed values (including numeric priority), labels, form names, required/disabled state, typeahead, arrow navigation and focus return.
- Checkboxes use Checkbox with checked bindings or onCheckedChange. Team privacy acknowledgements, cycle carry-over, issue selection, version-source mode and cookie consent retain their domain state.
- Buttons use local Button variants, preserving submit/reset types and handlers. Popover and selector triggers forward primitive props through child snippets, avoiding nested buttons. Issue rows group independent controls with a Button for keyboard activation.
- Dates use the installed Calendar/RangeCalendar popovers. Clearing a single date is a separate button. The range picker no longer renders the old X action, which never cleared dates and only dismissed the popover.
- File uploads use a bound Input with type=file; rich-editor uploads reset its value before opening so the same file can be selected again. Drag/drop and clipboard upload pipelines are unchanged.
- Dev Machine choices use RadioGroup for arrow-key navigation. Confirmation and cookie preference dialogs compose AlertDialog/Dialog for keyboard focus and dismissal.

## Upstream refresh

Registry: shadcn-svelte CLI 1.7.0, Nova style, fetched 2026-10-22. Refreshed button, input, textarea, checkbox, switch, radio-group, select, command, calendar, range-calendar and popover plus registry dependencies. WEB received its own Checkbox and Dialog; packages remain independent.

Official references: [CLI](https://shadcn-svelte.com/docs/cli), [changelog](https://shadcn-svelte.com/docs/changelog), [Select](https://shadcn-svelte.com/docs/components/select), [Input](https://shadcn-svelte.com/docs/components/input).

The UI foundation upgrades to Bits UI 2.20.0, Svelte 5.57.2, @lucide/svelte 1.61.0, Tailwind 4.3.3, tailwind-merge 3.7.0, tailwind-variants 3.3.1, @internationalized/date 3.12.4 and svelte-sonner 1.2.1. The deprecated lucide-svelte package is removed; application icons use @lucide/svelte. Both lockfiles are updated. The 18 team glyphs removed upstream are preserved from Lucide 0.577.0 as licensed SVG nodes rendered by the current Icon primitive, so stored selections and the picker remain compatible. Kuayle's compact popover spacing, top-positioned application Dialog and overflow constraints are retained after reviewing registry changes. Unrelated framework-major migrations (SvelteKit/Vite/TypeScript) and editor/table package upgrades are outside this UI-component refresh.

### Kuayle customizations to keep on every refresh

The registry refresh above overwrote some customizations; KUA-63 restored them.
Re-apply or re-check these after any `shadcn-svelte add` overwrite:

- `plain` variant on Button (`buttonClass`, also used by AlertDialog Action/Cancel and calendar navigation), Input, Textarea and Select.Trigger: renders the element without primitive styling so crafted product controls keep their own classes.
- Password: margin utilities move to the wrapper and the reveal toggle is a plain, full-height overlay with `tabindex={-1}`, so it stays centered on the input and out of the tab order.
- Dialog: `bg-background` content and a `bg-black/50` overlay without backdrop blur.
- Separator: vertical separators use `h-full` instead of upstream `self-stretch`.
- Calendar and RangeCalendar: `locale` defaults to the Paraglide `getLocale()`.
- Slider: the thumb receives the Root's `aria-label` and `aria-valuetext`, so each snapped slider has an accessible name and announces its value with units (Dev Machine resource sliders).

## Explicit native internals and exceptions

- `components/ui/` contains the upstream native button, input and textarea elements behind the canonical component API; these are the intended accessible implementation, not application-level exceptions.
- RichEditor uses Tiptap/ProseMirror for contenteditable text, task-list checkbox node views, collaborative cursors and image resize handles. shadcn-svelte has no equivalent editor/node-view lifecycle. The imperative image resize button stays inside its ProseMirror node view; surrounding toolbar, search and upload controls use primitives.
- IssueVersionHistory renders sanitized, stored rich-text HTML. Its task-list inputs are disabled previews of versioned content, not editable form controls. The sanitizer expression is intentionally unchanged.
- TerminalSession delegates its terminal input, selection and keyboard emulation to xterm. Its surrounding actions use Button; replacing its hidden input would break terminal emulation.
- Browser date/color/file capabilities remain behind styled Input or Calendar compositions where appropriate. Semantic content links, labels, layout containers and drag handles remain HTML.

## Baseline locations and replacements

| File | Original line(s): control → replacement |
| --- | --- |
| [UI/src/lib/components/layout/CommandPalette.svelte](../UI/src/lib/components/layout/CommandPalette.svelte) | 224, 255, 293: button → Button; 242: input[text] → Input |
| [UI/src/lib/components/layout/Sidebar.svelte](../UI/src/lib/components/layout/Sidebar.svelte) | 665, 674, 745, 805, 841, 1091, 1106, 1262, 1272: button → Button; 1196: button[button] → Button |
| [UI/src/lib/components/layout/SidebarToggle.svelte](../UI/src/lib/components/layout/SidebarToggle.svelte) | 9: button → Button |
| [UI/src/lib/components/layout/WorkspaceSwitcher.svelte](../UI/src/lib/components/layout/WorkspaceSwitcher.svelte) | 81, 98, 114: button → Button; 135, 149: input[text] → Input |
| [UI/src/lib/components/shared/AppToast.svelte](../UI/src/lib/components/shared/AppToast.svelte) | 43: button[button] → Button |
| [UI/src/lib/components/shared/ConfirmDialog.svelte](../UI/src/lib/components/shared/ConfirmDialog.svelte) | 39, 45: button → Button |
| [UI/src/lib/components/shared/DatePickerPopover.svelte](../UI/src/lib/components/shared/DatePickerPopover.svelte) | 67: button → Button |
| [UI/src/lib/components/shared/DateRangePickerPopover.svelte](../UI/src/lib/components/shared/DateRangePickerPopover.svelte) | 82: button → Button |
| [UI/src/lib/components/shared/DueDatePickerPanel.svelte](../UI/src/lib/components/shared/DueDatePickerPanel.svelte) | 74, 91, 101: button[button] → Button |
| [UI/src/lib/components/shared/EmptyState.svelte](../UI/src/lib/components/shared/EmptyState.svelte) | 19: button → Button |
| [UI/src/lib/components/shared/ErrorState.svelte](../UI/src/lib/components/shared/ErrorState.svelte) | 11: button → Button |
| [UI/src/lib/components/shared/FilterBar.svelte](../UI/src/lib/components/shared/FilterBar.svelte) | 41, 50, 57, 70, 78, 85, 97: button → Button |
| [UI/src/lib/components/shared/FilterBuilder.svelte](../UI/src/lib/components/shared/FilterBuilder.svelte) | 283: input[text] → Input; 394, 401, 411, 425, 432, 442, 456, 462, 472, 484, 498, 504, 514, 526, 540, 547, 564, 578, 585, 597, 610, 625, 634, 651: button → Button |
| [UI/src/lib/components/shared/ReleaseNotice.svelte](../UI/src/lib/components/shared/ReleaseNotice.svelte) | 244: button[button] → Button |
| [UI/src/lib/components/shared/RichEditor.svelte](../UI/src/lib/components/shared/RichEditor.svelte) | 956, 959, 962, 965, 971, 974, 980, 983, 986, 992, 995, 999, 1002, 1009, 1012, 1019, 1022, 1074, 1078, 1080, 1083, 1086, 1089, 1095, 1098, 1101, 1104, 1107, 1113, 1119: button[button] → Button; 1128: input[url] → Input |
| [UI/src/lib/components/shared/SaveViewDialog.svelte](../UI/src/lib/components/shared/SaveViewDialog.svelte) | 159: button[button] → Button |
| [UI/src/lib/components/shared/ShareLinkDialog.svelte](../UI/src/lib/components/shared/ShareLinkDialog.svelte) | 114, 153, 169, 175: button → Button |
| [UI/src/lib/components/shared/mention/MentionList.svelte](../UI/src/lib/components/shared/mention/MentionList.svelte) | 76, 103: button[button] → Button |
| [UI/src/lib/components/shared/slash-command/SlashCommandMenu.svelte](../UI/src/lib/components/shared/slash-command/SlashCommandMenu.svelte) | 93: button[button] → Button |
| [UI/src/lib/components/ui/password/password.svelte](../UI/src/lib/components/ui/password/password.svelte) | 39: button[button] → Button |
| [UI/src/lib/features/analytics/DistributionChart.svelte](../UI/src/lib/features/analytics/DistributionChart.svelte) | 187, 195: button → Button |
| [UI/src/lib/features/auth/LoginForm.svelte](../UI/src/lib/features/auth/LoginForm.svelte) | 45: button[submit] → Button |
| [UI/src/lib/features/cycles/CompleteCycleDialog.svelte](../UI/src/lib/features/cycles/CompleteCycleDialog.svelte) | 72: input[checkbox] → Checkbox |
| [UI/src/lib/features/cycles/CycleTimelineRow.svelte](../UI/src/lib/features/cycles/CycleTimelineRow.svelte) | 161, 174, 187, 201: button → Button |
| [UI/src/lib/features/dev-machines/IssueMachinePickerDialog.svelte](../UI/src/lib/features/dev-machines/IssueMachinePickerDialog.svelte) | 192: button[button] → Button |
| [UI/src/lib/features/dev-machines/TerminalDock.svelte](../UI/src/lib/features/dev-machines/TerminalDock.svelte) | 67, 82, 92, 106: button[button] → Button |
| [UI/src/lib/features/issues/AddRelationDialog.svelte](../UI/src/lib/features/issues/AddRelationDialog.svelte) | 212, 275: button → Button; 235: button[button] → Button; 253: input[text] → Input |
| [UI/src/lib/features/issues/BulkActionBar.svelte](../UI/src/lib/features/issues/BulkActionBar.svelte) | 679, 690, 699, 711, 735, 765, 782, 833, 841, 859, 886, 905, 919, 942, 955, 972: button → Button; 755: input[text] → Input |
| [UI/src/lib/features/issues/CreateIssueDialog.svelte](../UI/src/lib/features/issues/CreateIssueDialog.svelte) | 355, 367, 373, 381, 445, 459, 474, 495, 518, 539: button → Button; 402: input[text] → Input; 562, 571: button[button] → Button |
| [UI/src/lib/features/issues/FullPageIssueView.svelte](../UI/src/lib/features/issues/FullPageIssueView.svelte) | 762, 771, 778, 785, 792, 878, 885, 917, 1002, 1052, 1104, 1145, 1149, 1207, 1255, 1273, 1293, 1311, 1340, 1387, 1399, 1425, 1429, 1443, 1460, 1465, 1490: button → Button; 801, 812, 821, 829, 837, 845, 932: button[button] → Button; 906: input[text] → Input |
| [UI/src/lib/features/issues/IssueContextMenu.svelte](../UI/src/lib/features/issues/IssueContextMenu.svelte) | 458, 479: button → Button; 545: input[checkbox] → Checkbox |
| [UI/src/lib/features/issues/IssueCreatedToast.svelte](../UI/src/lib/features/issues/IssueCreatedToast.svelte) | 52, 68, 75, 82: button[button] → Button |
| [UI/src/lib/features/issues/IssueDetail.svelte](../UI/src/lib/features/issues/IssueDetail.svelte) | 148: button[button] → Button; 185, 200, 213, 221, 272, 314: button → Button; 250: input[text] → Input; 256: button[submit] → Button |
| [UI/src/lib/features/issues/IssueForm.svelte](../UI/src/lib/features/issues/IssueForm.svelte) | 38: input[text] → Input; 48: textarea → Textarea; 86: button[button] → Button; 93: button[submit] → Button |
| [UI/src/lib/features/issues/IssueGitHubActivity.svelte](../UI/src/lib/features/issues/IssueGitHubActivity.svelte) | 71, 118: button → Button |
| [UI/src/lib/features/issues/IssueGroupHeader.svelte](../UI/src/lib/features/issues/IssueGroupHeader.svelte) | 102: button → Button |
| [UI/src/lib/features/issues/IssueListLoadMore.svelte](../UI/src/lib/features/issues/IssueListLoadMore.svelte) | 24: button[button] → Button |
| [UI/src/lib/features/issues/IssuePickerDialog.svelte](../UI/src/lib/features/issues/IssuePickerDialog.svelte) | 256: input[search] → Input; 280: button[button] → Button |
| [UI/src/lib/features/issues/IssueRelations.svelte](../UI/src/lib/features/issues/IssueRelations.svelte) | 105, 139: button → Button |
| [UI/src/lib/features/issues/IssueRow.svelte](../UI/src/lib/features/issues/IssueRow.svelte) | 256, 297, 481, 492: button → Button; 318: input[text] → Input |
| [UI/src/lib/features/issues/IssueVersionHistory.svelte](../UI/src/lib/features/issues/IssueVersionHistory.svelte) | 64, 77, 79: button → Button; 84: input[checkbox] → Checkbox |
| [UI/src/lib/features/issues/PublicIssueDetail.svelte](../UI/src/lib/features/issues/PublicIssueDetail.svelte) | 78, 97, 105, 114: button → Button |
| [UI/src/lib/features/issues/PublicIssueRow.svelte](../UI/src/lib/features/issues/PublicIssueRow.svelte) | 24: button → Button |
| [UI/src/lib/features/issues/ScopedIssueList.svelte](../UI/src/lib/features/issues/ScopedIssueList.svelte) | 289: button → Button |
| [UI/src/lib/features/issues/SubIssuesList.svelte](../UI/src/lib/features/issues/SubIssuesList.svelte) | 139, 154, 185, 209: button[button] → Button; 312: button → Button |
| [UI/src/lib/features/labels/LabelDialog.svelte](../UI/src/lib/features/labels/LabelDialog.svelte) | 78: button[button] → Button; 87: input[color] → Input |
| [UI/src/lib/features/projects/GanttChart.svelte](../UI/src/lib/features/projects/GanttChart.svelte) | 417, 428, 445, 457: button → Button |
| [UI/src/lib/features/teams/CreateTeamDialog.svelte](../UI/src/lib/features/teams/CreateTeamDialog.svelte) | 107, 110: input[checkbox] → Checkbox |
| [UI/src/lib/features/teams/TeamPrivacySettings.svelte](../UI/src/lib/features/teams/TeamPrivacySettings.svelte) | 108: input[checkbox] → Checkbox |
| [UI/src/lib/features/workspaces/WorkspaceImportDialog.svelte](../UI/src/lib/features/workspaces/WorkspaceImportDialog.svelte) | 92: input[file] → Input; 139, 143, 148: input → Input |
| [UI/src/routes/[workspaceSlug]/inbox/+page.svelte](../UI/src/routes/[workspaceSlug]/inbox/+page.svelte) | 342, 520, 533: button → Button |
| [UI/src/routes/[workspaceSlug]/machines/[machineId]/+page.svelte](../UI/src/routes/[workspaceSlug]/machines/[machineId]/+page.svelte) | 675, 699: button[button] → Button |
| [UI/src/routes/[workspaceSlug]/my-views/+page.svelte](../UI/src/routes/[workspaceSlug]/my-views/+page.svelte) | 105: button → Button |
| [UI/src/routes/[workspaceSlug]/projects/+page.svelte](../UI/src/routes/[workspaceSlug]/projects/+page.svelte) | 101: button → Button |
| [UI/src/routes/[workspaceSlug]/projects/[projectId]/+page.svelte](../UI/src/routes/[workspaceSlug]/projects/[projectId]/+page.svelte) | 292, 328: button → Button |
| [UI/src/routes/[workspaceSlug]/settings/+layout.svelte](../UI/src/routes/[workspaceSlug]/settings/+layout.svelte) | 170: button[button] → Button |
| [UI/src/routes/[workspaceSlug]/settings/+page.svelte](../UI/src/routes/[workspaceSlug]/settings/+page.svelte) | 143, 297: input[text] → Input; 171: input[url] → Input |
| [UI/src/routes/[workspaceSlug]/settings/ai/+page.svelte](../UI/src/routes/[workspaceSlug]/settings/ai/+page.svelte) | 97: input[url] → Input; 112: input[text] → Input; 129: input[password] → Input; 146, 163: textarea → Textarea |
| [UI/src/routes/[workspaceSlug]/settings/github/+page.svelte](../UI/src/routes/[workspaceSlug]/settings/github/+page.svelte) | 277, 314, 343, 358, 445: button → Button |
| [UI/src/routes/[workspaceSlug]/settings/labels/+page.svelte](../UI/src/routes/[workspaceSlug]/settings/labels/+page.svelte) | 73, 113, 120: button → Button |
| [UI/src/routes/[workspaceSlug]/settings/licenses/+page.svelte](../UI/src/routes/[workspaceSlug]/settings/licenses/+page.svelte) | 77: input → Input |
| [UI/src/routes/[workspaceSlug]/settings/members/+page.svelte](../UI/src/routes/[workspaceSlug]/settings/members/+page.svelte) | 181, 217, 225, 237, 258, 272, 305, 362, 372: button → Button; 337: input → Input; 390: input[email] → Input; 430, 444: input[number] → Input |
| [UI/src/routes/[workspaceSlug]/settings/preferences/+page.svelte](../UI/src/routes/[workspaceSlug]/settings/preferences/+page.svelte) | 416, 424: button → Button; 449, 547, 566: button[button] → Button |
| [UI/src/routes/[workspaceSlug]/settings/profile/+page.svelte](../UI/src/routes/[workspaceSlug]/settings/profile/+page.svelte) | 98, 112: input[text] → Input; 126: input[url] → Input |
| [UI/src/routes/[workspaceSlug]/settings/teams/[teamId]/+page.svelte](../UI/src/routes/[workspaceSlug]/settings/teams/[teamId]/+page.svelte) | 473, 502, 511, 532, 553, 585, 613: button[button] → Button; 495: input → Input; 669: textarea → Textarea |
| [UI/src/routes/[workspaceSlug]/settings/teams/[teamId]/statuses/+page.svelte](../UI/src/routes/[workspaceSlug]/settings/teams/[teamId]/statuses/+page.svelte) | 359, 367, 405, 458, 468, 474, 491, 498, 528, 538, 545: button → Button; 447, 516: input[text] → Input |
| [UI/src/routes/[workspaceSlug]/settings/templates/+page.svelte](../UI/src/routes/[workspaceSlug]/settings/templates/+page.svelte) | 104: button → Button; 158: input[text] → Input |
| [UI/src/routes/[workspaceSlug]/settings/tokens/+page.svelte](../UI/src/routes/[workspaceSlug]/settings/tokens/+page.svelte) | 250: button → Button; 439: button[button] → Button |
| [UI/src/routes/[workspaceSlug]/settings/version/+page.svelte](../UI/src/routes/[workspaceSlug]/settings/version/+page.svelte) | 165: button[button] → Button |
| [UI/src/routes/[workspaceSlug]/settings/webhooks/+page.svelte](../UI/src/routes/[workspaceSlug]/settings/webhooks/+page.svelte) | 103: button → Button; 198: button[button] → Button |
| [UI/src/routes/[workspaceSlug]/teams/[teamId]/+page.svelte](../UI/src/routes/[workspaceSlug]/teams/[teamId]/+page.svelte) | 428, 441, 451: button → Button |
| [UI/src/routes/[workspaceSlug]/teams/[teamId]/cycles/+page.svelte](../UI/src/routes/[workspaceSlug]/teams/[teamId]/cycles/+page.svelte) | 306, 440, 495: button → Button |
| [UI/src/routes/[workspaceSlug]/teams/[teamId]/cycles/[cycleId]/+page.svelte](../UI/src/routes/[workspaceSlug]/teams/[teamId]/cycles/[cycleId]/+page.svelte) | 208, 215: button → Button |
| [UI/src/routes/[workspaceSlug]/teams/[teamId]/projects/+page.svelte](../UI/src/routes/[workspaceSlug]/teams/[teamId]/projects/+page.svelte) | 118: button → Button |
| [UI/src/routes/[workspaceSlug]/teams/[teamId]/triage/+page.svelte](../UI/src/routes/[workspaceSlug]/teams/[teamId]/triage/+page.svelte) | 130: button → Button |
| [UI/src/routes/[workspaceSlug]/teams/[teamId]/views/+page.svelte](../UI/src/routes/[workspaceSlug]/teams/[teamId]/views/+page.svelte) | 120: button → Button |
| [UI/src/routes/[workspaceSlug]/views/[viewId]/+page.svelte](../UI/src/routes/[workspaceSlug]/views/[viewId]/+page.svelte) | 181, 204, 245, 255: button → Button; 189: input[text] → Input |
| [UI/src/routes/invite/[token]/+page.svelte](../UI/src/routes/invite/[token]/+page.svelte) | 105, 116, 141: button → Button |
| [UI/src/routes/login/+page.svelte](../UI/src/routes/login/+page.svelte) | 121: button[submit] → Button; 135: button → Button; 160, 172, 197: button[button] → Button |
| [UI/src/routes/share/[token]/+page.svelte](../UI/src/routes/share/[token]/+page.svelte) | 218: input[text] → Input; 231, 245, 261, 274, 289, 309, 341: button → Button |
| [UI/src/routes/workspace-setup/+page.svelte](../UI/src/routes/workspace-setup/+page.svelte) | 72, 76: input → Input |
| [WEB/src/lib/components/CookieBanner.svelte](../WEB/src/lib/components/CookieBanner.svelte) | 52: button[button] → Button; 72, 82, 97, 112: input[checkbox] → Checkbox |
| [WEB/src/lib/components/Deploy.svelte](../WEB/src/lib/components/Deploy.svelte) | 52: button → Button |
| [WEB/src/lib/components/Nav.svelte](../WEB/src/lib/components/Nav.svelte) | 71: button → Button |
| [WEB/src/routes/privacy/+page.svelte](../WEB/src/routes/privacy/+page.svelte) | 220: button[button] → Button |

Additional native selects: `settings/members/+page.svelte` (invitation and link roles), `IssueVersionHistory.svelte` (change selection), `TeamPrivacySettings.svelte` (member selection), and `IssueForm.svelte` (team/status/priority). All use SelectField. Existing Input, Password, Checkbox, Switch, Select, Command, Calendar, RadioGroup and selector wrappers were inspected alongside this inventory.

## Verification

Run `node scripts/audit-controls.mjs` from UI for an AST-based sweep of both packages. It ignores stored HTML strings and permits native internals only inside primitives; it is not a substitute for interaction or visual checks. Required checks and final results are recorded in the PR, including the original-versus-migrated keyboard regression, full UI browser suite, WEB responsive/consent checks, source audit, type checks, unit tests and production builds.
