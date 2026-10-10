# ReelStash Admin — redesign notes

Visual language: Usha theme, compact density, ruled metric strips, hairline panels, dot + text statuses, solid
dialogs and drawers, right-side detail drawers.

This is also the point where the admin became its own web app. `reelstash-admin` (`admin/`) and
`reelstash-app` (`app/`, the media library) are separate Vite projects with their own
`package.json`, build, tests, Dockerfile and nginx config. They share no source; they meet only over
the HTTP API and the cross-origin links configured by `REELSTASH_MEDIA_URLS` / `REELSTASH_ADMIN_URLS`.
`admin/` also carries the Files and Downloads workspaces, because they are deployed on the admin
origin (`admin/nginx/admin.conf`).

## 1. Audit of the previous console

### Backend permission model

`internal/httpapi/routes_admin.go` mounts every `/api/admin/*` route under one gate, `requireAdmin`
(`user.Role == "ADMIN"`, else `403 admin privileges required`). There is no finer role. The session
(`GET /api/me`) returns `isAdmin`, which the console reads the way the middleware does. The UI keeps a
`requires` field per navigation entry so a finer role can slot in later.

At the time of the audit there was **no audit log** in the backend and no admin write recorded a reason; both exist now (section 5).

### Information architecture (before)

| Group | Pages |
| --- | --- |
| Overview | Dashboard, Analytics |
| Library | Libraries, Videos, Review, Duplicates |
| Metadata | People, Studios, Collections / series, Categories & tags, Providers |
| Storage | Disks (+ disk page), Scan rules, Organizer, Rename pipeline |
| System | Setup wizard, Jobs, Automation, Housekeeping, Appearance, Users & access, Plugins, Notifications |

Twenty-three sidebar entries in five groups. Library scope was a switcher portalled into the top bar by a
slot component; page titles and actions were portalled into it as well.

### Problems found

**Bugs and unsafe actions**
- A user's **role was a `<select>` that saved on change** and **Enabled was a switch that saved on
  click**, with no confirmation. You could disable the account you were signed in with and lock
  everyone out by disabling or demoting the last administrator; the backend only refuses to demote
  yourself.
- **Library access saved on every checkbox tick** (one request per tick, no undo).
- Turning **password sign-in off** was a bare switch, even when no other sign-in method was running.
- `window.confirm` was used for *Remove missing titles*, plugin uninstall and provider removal;
  *Clear finished jobs*, *Confirm metadata* (bulk), *Confirm as-is*, *Keep best copy*, *Organize*,
  *Apply template* and the *Watch for new files* and *Automatic matching* switches had **no
  confirmation at all**, although several rewrite files or change what future scans do.
- People and studio names in Metadata linked to **`/performers/:id` on the admin origin**, which does
  not exist there (404 in the split deployment). They now go through `mediaHref`.
- `Button variant="danger"` is not a Spatika variant; those buttons rendered as ordinary primary
  buttons.
- Polling: the dashboard polled jobs every 4 s and the overview every 15 s even with the live event
  stream connected, health was polled by both the dashboard and the footer, the footer polled `/healthz`
  every 5 s forever, and Duplicates polled the scan every 2 s whether or not one was running.
- A signed-in non-administrator was silently redirected to `/files`; there was no explanation.
- The first-run wizard replaced the dashboard whenever no disk existed, hiding the rest of the console.
- No retry on failed loads, generic `Failed to load X` toasts, no skeletons, spinners instead.

**UX**
- "System" mixed first-run setup, operations (jobs, automation, housekeeping), settings (appearance)
  and access control (users, plugins, notifications) in one eight-item group.
- No linkable detail pages for users or libraries; both were dialogs or inline controls.
- Filters, sort and page of the Videos table lived in the URL, but nothing else did, and there were no
  saved views or column choices.
- Five different card styles, gradient hovers, glass dialogs and a glass top bar, coloured stat cards.
- The dashboard opened with a greeting and four stat cards; the "needs attention" list came after.
- System health lived in a side sheet only.

## 2. Information architecture (after)

```
Overview      Dashboard               /admin
              Analytics               /admin/analytics
Library       Libraries               /admin/libraries, /admin/libraries/:id
              Videos                  /admin/videos
              Review queue            /admin/review
              Duplicates              /admin/duplicates
Catalog       Metadata                /admin/metadata/{performers,studios,series,categories}
              Providers               /admin/metadata/providers
Storage       Storage                 /admin/storage, /admin/storage/:id, /admin/scan-rules
              Organizer               /admin/organizer, /admin/rename
Operations    Jobs                    /admin/jobs
              Automation              /admin/automation, /admin/housekeeping
              System status           /admin/system
Access &      Users & access          /admin/users, /admin/users/:id
settings      Plugins                 /admin/plugins
              Notifications           /admin/notifications
              Appearance              /admin/appearance
(hidden)      Setup guide             /admin/setup   (dashboard action and ⌘K)
```

Eighteen entries in six groups that follow what an administrator is trying to do. Sibling views of
one thing (People / Studios / Collections / Categories, Disks / Scan rules, Folder rules / Rename
pipeline, Schedules / Housekeeping) share one sidebar entry and switch with page tabs; every tab is
still a route and a palette entry, and all previous URLs still work. The sidebar fits without
scrolling from 800 px of viewport height.

New: **System status** (a page, replacing a side sheet), **user, library and disk detail pages**,
**saved views**, **column choices** and one **confirm pattern**.

One navigation model (`src/console/lib/navigation.js`) drives the sidebar, the phone tab bar, the ⌘K
palette, the breadcrumbs, page titles and the `g` + letter shortcuts.

## 3. Design system

`src/console/styles/console.css` — tokens (`--console-*`), shell, sidebar, top bar, page header,
metric strip, panels, tables, states, drawer, settings rows, forms, progress and meters. Imported
last, so it wins over the styles the shared title and people editors bring.

`src/console/components/` — the only building blocks pages use:

| Component | Use |
| --- | --- |
| `PageHeader`, `PageStack`, `ProfileHeader` | Title, purpose, one primary action; identity header of a detail page |
| `MetricStrip` | Ruled KPI row (a figure links to the page that explains it) |
| `Panel`, `PanelLink` | Framed object: table, chart, feed |
| `SkeletonRows`, `ErrorState`, `EmptyState` | Loading / failure (retry, permission-aware) / empty |
| `Status`, `Tag`, `ProgressBar`, `Meter` | Dot + text status, never colour alone; progress with the number beside it |
| `TableToolbar`, `TablePagination`, `SortHeader`, `BulkBar`, `rowActivation` | Search, server-side paging, sort, bulk actions, clickable keyboard-accessible rows |
| `SegmentedTabs`, `SectionTabs`, `RangeTabs` | ARIA tablist; page tabs that are routes; chart ranges |
| `SavedViews`, `useColumnVisibility` | Named filter views and column choices, kept per browser |
| `DetailDrawer`, `DetailList`, `CopyButton` | Right-side inspector, definition lists, copyable IDs |
| `MoreMenu` | Secondary and destructive actions |
| `ConfirmProvider` / `useConfirm` | What happens, who is affected, reversibility, optional reason, typed confirmation |
| `FormDialog`, `Field` | Labelled form, hint, inline error after the first submit, focus on the first error, unsaved-change guard, Save disabled until something changes |
| `SettingsGroup`, `SettingsRow` | Grouped configuration with descriptions |

`src/console/lib/` — navigation model, `useResource` (latest-request-wins loading with optional
polling), `useUrlState`, `useJobs` (live event stream, polling only as a fallback), `signals` (health
and review count, loaded once for the shell and the dashboard), error wording, formatting.

## 4. Rules

- One primary button per view. Destructive actions live in a menu or a danger zone.
- Every production-affecting change goes through `useConfirm`. Irreversible ones, and ones that
  affect everyone, require typed confirmation (delete a library or disk, uninstall a plugin, remove
  missing titles, clean up library folders, make someone an administrator).
- The console blocks what the backend does not: disabling yourself, and disabling or demoting the last
  enabled administrator.
- Lists: server-side pagination where the API has it (videos, people), debounced search, filters, sort
  and page in the URL. Where the API returns everything (users, jobs, most catalog lists) the console
  filters and pages locally and says so.
- Numbers use tabular figures and time/number cells do not wrap. Byte counts are integers; sizes are
  formatted from them. ReelStash has no money amounts.
- Toggles that change production (password sign-in, file watching, automatic matching, auth plugins)
  are confirmed. Nothing here has a slider.
- Forms keep what was typed when a save fails, and the message says what failed and what to do next.
- Reads are cheap and not audit-logged (there is no audit log), but polling is still kept to what is
  visible: events first, slow polls as the fallback, pauses while the tab is hidden.
- Accessibility: visible focus, labelled controls, tabs with ARIA, a `role="status"` for live regions,
  severity spelled out for screen readers, no colour-only status.

## 5. Gaps

The first version of this redesign left out what the backend could not do. Those are now built, on the
backend first and then in the console:

| Gap | Now |
| --- | --- |
| Audit log | Table `admin_audit_event` (migration 0049). Middleware on `/api/admin` records every successful non-read request: actor, action, target, reason, detail. `GET /api/admin/audit` pages and filters it. Console: **Audit log** page, and an **Activity** tab on every user. Reads are not recorded. |
| Reasons on admin writes | The `X-Admin-Reason` header, stored on the audit event. Confirm dialogs ask for one (required for deleting a user, library or disk and for *Remove missing titles*; optional elsewhere). |
| User by ID | `GET /api/admin/users/{id}`; the user page no longer scans the list. |
| Deleting a user | `DELETE /api/admin/users/{id}`, cascading to sessions, history and access. Typed confirmation and a required reason. |
| Sessions | `GET`/`DELETE /api/admin/users/{id}/sessions[/{publicId}]`. Console: **Sessions** tab with per-session and "sign out everywhere". |
| Last-admin and self guards | Enforced by the server (409/400), not only the console: you cannot disable, demote or delete yourself, nor the last enabled administrator. |
| Server paging | Users (`limit`, `offset`, `q`, `role`, `status`, `method`, `sort`, `dir`, with `total` and `counts`) and jobs (`offset`, `total`). The palette searches people on the server. |
| Webhook history | Table `webhook_delivery` (latest 50 per webhook). Console: *Delivery history* on each webhook. |

Still open:

- **Finer admin roles.** There is one `ADMIN` role. Splitting it (for example read-only or users-only
  administrators) first needs a decision about what each role may do; every `/api/admin` route is
  currently gated by that single check.
- **Per-user activity.** Analytics stays aggregate on purpose; the console shows changes made *to* an
  account (audit), never what a person watched.
- **Server paging for libraries, disks and most catalog lists.** Those lists are small and still load whole.

## 6. Verification

- `npm run build` and `npm test` in `admin/` (typecheck: the project is plain JavaScript, there is no
  TypeScript step). `app/` builds and passes its own tests independently.
- Visual checks in the browser pane at 1440, 1280, 1024, 840 and 375 px, light (Usha) and dark
  (Midnight), against a mock API served from outside the repo, because the Go backend needs the shared
  PostgreSQL container, which is not running here.
- Unit and component tests for the navigation model (shortcut uniqueness, route resolution), the
  account guard, error wording, formatting, the confirm pattern, forms, the shell (shortcuts, palette,
  non-admin state without admin requests), the dashboard's attention list, jobs, users and housekeeping.
