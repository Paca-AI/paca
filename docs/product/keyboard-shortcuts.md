# Keyboard Shortcut System

Status: **implemented**

A single, discoverable keyboard shortcut system for the web app covering three
levels: navigating between sidebar pages, switching views inside an interaction
page, and acting on the task under the mouse cursor. Every binding is
`Ctrl`/`⌘ + key` (optionally `+ Shift`) — no leader/sequence chords. Shortcuts
are shown inside the app via a `Mod+/` cheat-sheet dialog, tooltips, and a
right-click context menu on tasks.

## Why not literal first-letter everywhere

Browsers and macOS reserve a large slice of `Mod+<letter>` combos before page
JavaScript ever sees the keydown, and a few more are so deeply ingrained
(select-all, copy, paste) that reusing them — even where technically
overridable — would be bad UX. The keymap below was built by excluding:

- **Hard-blocked, unpreventable on any browser**: `Mod+T`/`N`/`W` (tab/window
  control), `Mod+1`–`9` (switch tab), `Mod+L`/`K` (address bar / omnibox).
- **macOS application-menu items**, intercepted before the page: `⌘Q`, `⌘M`
  (minimize), and `⌘H` alone (hide app — `⌘⇧H` is fine, it's a different,
  page-reachable binding).
- **DevTools bindings** on the Shift tier: `Mod+Shift+I/J/C/K/M/T/N/B/D/O/P/S/R/U/G/V/Z`.
- **Global select-all/copy/paste/cut/undo** (`Mod+A/C/V/X/Z`) — technically
  overridable, but claiming them page-wide breaks a near-universal
  expectation.

`Mod+[` / `Mod+]` (previous/next view) knowingly overlap Safari's `⌘[`/`⌘]`
back/forward-history shortcut on macOS. Accepted because the override only
takes effect on an interaction page (see "Event handling rules" below —
`page.*` shortcuts no-op, and leave the browser default alone, everywhere
else), so it can only ever shadow history navigation while already on a
backlog/sprint/timeline view.

Two features from an earlier leader-key draft (`G` then a letter) were
dropped once shortcuts moved to flat `Mod+key` combos, because they depend on
digits, which are unusably reserved (`Mod+1`–`9` always switches browser
tabs):
- **Jump directly to sprint N** — only "jump to the active sprint" survived;
  reaching another sprint is a sidebar click away.
- **Switch directly to view N** — replaced by `Mod+[` / `Mod+]` cycling.

## Keymap

### Navigation ("go to") — global, works anywhere when not typing

| Shortcut | Action | Route |
|---|---|---|
| `Mod+Shift+H` | Home / all projects | `/home` |
| `Mod+Shift+L` | Timeline | `/projects/:id/interactions/timeline` |
| `Mod+I` | Product backlog | `/projects/:id/interactions/backlog` |
| `Mod+Shift+A` | Active sprint (first open sprint in sidebar order) | `/projects/:id/interactions/sprints/:sprintId` |
| `Mod+G` | Agents | `/projects/:id/agents` |
| `Mod+Shift+X` | Chats | `/projects/:id/chats` |
| `Mod+Shift+W` | Automation (workflows) | `/projects/:id/automation` |
| `Mod+J` | Team | `/projects/:id/team` |
| `Mod+D` | Docs | `/projects/:id/docs` |
| `Mod+Shift+,` | Project settings | `/projects/:id/settings` |
| `Mod+/` | Keyboard shortcuts help dialog | — |
| `Mod+B` | Toggle sidebar (**pre-existing**, `ui/sidebar.tsx`) | — |
| `Esc` | Close topmost dialog/search | — |

Project-scoped targets are only active when inside a project and are
permission-filtered with the same rules the sidebar already applies
(`ANON_HIDDEN_SEGMENTS`, `sprints.read`) — see `runGotoAction` in
`lib/shortcuts/provider.tsx`.

### Interaction page — views (backlog, sprint, timeline)

| Shortcut | Action |
|---|---|
| `Mod+[` / `Mod+]` | Previous / next view tab (cycles) |
| `Mod+F` | Focus task search |
| `Mod+Shift+F` | Toggle view settings panel (filters, columns, sort) |
| `Mod+Enter` | Open/focus the first "add task" row in the view |

### Task under cursor (pointer over a card or row; board and table views)

| Shortcut | Action | Existing UI it triggers |
|---|---|---|
| `Mod+O` | Open task details | `onTaskClick` → `TaskDetailModal` |
| `Mod+E` | Epic picker | epic popover |
| `Mod+←` / `Mod+→` | Move task to previous/next column | same code path as drag-and-drop |
| `Mod+⌫` | Delete task (confirm dialog) | `deleteTask` (interaction-api.ts) |

Status, Assignee, Type, and Priority have **no keyboard shortcut** — they're
click-only (card popover) or right-click-only (context menu submenu); see
below. They were dropped from the keymap by request, and the controlled
open-state that existed solely to let `Mod+S/A/Y/P` open those pickers was
removed with them (those popovers are uncontrolled again).

Notes:

- "Column" means whatever `column_by` the active view groups by (status,
  sprint, assignee, type) — exactly what drag-and-drop does today. Board's
  `moveTaskToColumnDef` and List's `moveTaskToGroupDef` reuse the same
  `buildColumnDropUpdate` helper the drag handlers call, so keyboard and
  mouse stay in sync by construction.
- All edit shortcuts respect `canEdit`; read-only and anonymous users keep
  navigation and `Mod+O` only.
- Roadmap view has no columns, so move-left/right are inactive there.
- Hovering is tracked via `mouseenter`/`mouseleave` on the card/row root — see
  "Architecture" below for why keyboard focus isn't wired up yet.

### Right-click context menu on tasks

Every `TaskCard` and `TaskRow` is wrapped in `TaskContextMenu`
(`components/projects/interactions/task-context-menu.tsx`). It offers more
actions than the keyboard does — Status, Assignee, Type, and Priority
submenus are click-only (no shortcut hint next to them, since those keys
were retired); items that do have a shortcut show it via `Hint`/`KbdChord`:

- Open task `Mod+O`
- Move to → submenu of the current view's columns
- Status → submenu (no shortcut)
- Assignee → submenu (no shortcut)
- Type → submenu (no shortcut)
- Priority → submenu (no shortcut)
- Epic → submenu, `Mod+E`
- Copy task ID, Copy link — no shortcut
- Delete `Mod+⌫` (destructive styling, gated on `canEdit`)

Submenus reuse the same option lists the card popovers already render
(statuses, members, types, priority levels, epics).

### Rich-text editors (task description, docs)

| Shortcut | Action |
|---|---|
| `Mod+S` | Save immediately, instead of waiting for blur/the 3s debounce |

Handled locally by each BlockNote editor's own `onKeyDown` — not part of
`matchShortcut`/`provider.tsx` — since it only makes sense while focus is
inside one of them:

- `components/projects/interactions/task-detail/description-section.tsx` —
  task description. Calls the same `save()` already used by
  `onBlur`; a no-op if there's nothing pending (`pendingRef.current`).
- `components/projects/docs/doc-editor.tsx` — project docs. Pre-existing;
  also duplicated as a `window` keydown listener in the `$docId.tsx` route
  (not cleaned up here, out of scope for the description-editor addition).

Both editors are `contentEditable`, so the global listener's
`isTypingTarget` guard (see "Event handling rules" below) already skips
them — there's no conflict with the rest of the keymap. This reuses the `S`
key for an unrelated purpose from the retired `Mod+S` "Status picker"
binding mentioned above; that binding no longer exists in `matchShortcut` at
all, so there's nothing to collide with.

Listed in the `Mod+/` help dialog under its own "Description & docs" group
(`editor.save` in `SHORTCUT_DISPLAY`) purely for discoverability, the same
way `general.sidebar`/`general.close` are listed despite not going through
`matchShortcut` either.

## Discoverability

1. **`Mod+/` help dialog** (`components/shortcuts/shortcut-help-dialog.tsx`) —
   lists every shortcut, grouped (Go to / General / Page and views / Task
   under cursor / Description & docs), generated from `SHORTCUT_DISPLAY` in
   `keymap.ts`. Also reachable from the user menu ("Keyboard shortcuts").
2. **Context menu hints** — every menu item shows its key via
   `ContextMenuShortcut` + `KbdChord`.
3. **Search button tooltip** — shows the `Mod+F` hint.
4. **`Kbd`/`KbdChord`** (`components/ui/kbd.tsx`) — shared keycap chips, platform
   aware (`⌘` on macOS via `isMacPlatform()`, `Ctrl` elsewhere).

## Architecture

```
lib/shortcuts/
  keymap.ts               # matchShortcut() + SHORTCUT_DISPLAY registry —
                          # single source of truth for bindings and labels
  provider.tsx             # <ShortcutProvider> mounted in _authenticated
                          # layout; one window keydown listener, resolves
                          # goto actions (router/permissions/sprints) and
                          # dispatches page/task actions from the stores below
  page-shortcut-store.ts   # zustand: current interaction page's callbacks
                          # (prevView/nextView/focusSearch/toggleViewSettings/
                          # focusCreateTask) — InteractionLayout registers on
                          # mount, clears on unmount
  hovered-task-store.ts    # zustand: currently-hovered task's callbacks
                          # (open/openStatus/openAssignee/…/moveLeft/
                          # moveRight/delete) — TaskCard/TaskRow register on
                          # mouseenter, clear on mouseleave
  help-dialog-store.ts     # zustand: open state for the help dialog, so the
                          # user menu can open it without prop drilling
components/ui/kbd.tsx                  # keycap chip
components/ui/context-menu.tsx         # Base UI wrapper (@base-ui/react/context-menu),
                                       # mirrors ui/dropdown-menu.tsx conventions
components/shortcuts/shortcut-help-dialog.tsx
components/projects/interactions/task-context-menu.tsx
```

### Event handling rules (`provider.tsx`)

- **Typing guard**: skip when `e.isComposing || e.key === "Process"` (IME —
  the app ships ja/ko/zh-CN locales), or when the event target is inside
  `input, textarea, select, [contenteditable="true"]` (covers BlockNote
  editors).
- **Plain-vs-Shift dispatch**: `matchShortcut` looks up `PLAIN_ACTIONS` when
  `!e.shiftKey`, `SHIFT_ACTIONS` when `e.shiftKey` — so `Mod+Shift+O` does
  *not* accidentally fire the plain `Mod+O` binding.
- **No sequence/leader state** — every combo resolves in one keydown, so
  there's no pending-chord timeout or cancel-on-blur logic to maintain.
- **`preventDefault`** only once a shortcut both matches *and* resolves to a
  live handler: `general.help`/`goto.*` always claim the key once matched
  (they always do something), but `page.*`/`task.*` only call
  `preventDefault` after confirming `usePageShortcutStore`/
  `useHoveredTaskStore` actually has an active registration — otherwise the
  browser default (e.g. native find-in-page on `Mod+F`) is left alone. This
  keeps `page.*`/`task.*` scoped to interaction pages / a hovered task
  instead of shadowing browser shortcuts app-wide on pages where they'd be a
  no-op anyway.
- **Scope dispatch**: `goto.*` handled inline (needs router/permissions/
  sprints); `page.*` reads `usePageShortcutStore.getState().active`; `task.*`
  reads `useHoveredTaskStore.getState().active`. Because the two scopes use
  disjoint keys from `goto`, there's no runtime ambiguity to resolve.

### Task scope wiring

`TaskCard`/`TaskRow` only need controlled open-state for the **epic** picker
now — a local `useState` flag flipped by the hover-store registration.
Status/assignee/type/priority stay uncontrolled `Popover`/`DropdownMenu`
(`TaskTypeSelector`, used by `TaskRow` and `AddTaskRow`, is back to its
original uncontrolled-only form too), since nothing needs to open them
programmatically once those shortcuts were removed. On `mouseenter`, a
`useEffect` registers a `TaskShortcutActions` object into
`hovered-task-store` whose callbacks call `onClick`/`setEpicOpen(true)`/
`onDelete`/`onMoveLeft`/`onMoveRight`; `mouseleave` clears the registration.

`moveLeft`/`moveRight` are supplied by the parent (`BoardView`/`ListGroup`),
which already knows the current view's column/group order — `TaskCard`/
`TaskRow` themselves have no notion of "adjacent column."

**Accessibility gap**: task-scope shortcuts currently require mouse hover;
keyboard-only users can't reach them (cards aren't focusable and focus isn't
treated as "hovered"). This was deferred to keep the change scoped — flagged
as a follow-up.

### i18n

`shortcuts.json` (new namespace, registered in `i18n/react-i18next.d.ts`) and
the `board.taskContextMenu` block in `projects.json` are translated in all 9
supported locales (en, es, fr, ja, ko, pt-BR, ru, vi, zh-CN). Key *names*
(`Mod`, `Shift`, literal letters) are not translated; `Kbd`/`KbdChord` render
the same glyphs regardless of locale.

## Tests

`lib/shortcuts/keymap.test.ts` covers `matchShortcut` (no-modifier/Alt
rejection, plain vs. Shift-tier dispatch, the Shift-doesn't-leak-into-plain
case, `/`/`?` for help, unmapped combos) and `chordKeycaps`/`formatChord`
(macOS vs. non-macOS rendering).
