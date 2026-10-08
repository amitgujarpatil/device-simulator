# Task: Restyle the DeviceSimulator Run screen to the "Graphite" design

You are updating the existing **DeviceSimulator** desktop UI (the Run screen with Tests, Device config, Counters, Pipeline steps, Phase 1/2 and Logs). Restyle and restructure it to match the **Graphite** design described in this document.

**This is a presentation-layer change.** Keep every existing feature, data binding, API call, IPC/event and state transition working exactly as it does today. Do not rename persisted fields or config keys. Do not change business logic.

---

## 0. Files that come with this prompt

| File | What it is | How to use it |
|---|---|---|
| `graphite-agent-prompt.md` | This spec | Source of truth for tokens, layout, components, states and copy |
| `graphite-reference.html` | Static HTML/CSS of the final design in its **Idle** state (opens in any browser) | Pixel reference. Copy exact values from it. **Do not paste it in as production code**: rebuild it with the project's own components and styling system. |
| `graphite-reference-desktop.png` | Screenshot at 1440 × 900 | Visual target for desktop |
| `graphite-reference-mobile.png` | Screenshot at 390 px wide | Visual target for narrow windows |

Suggested location in the repo: `docs/design/graphite/`.

Where this document and the reference disagree, **this document wins**. The reference only shows the Idle state. Sections 6–7 define the other states.

---

## 1. Goals and non-goals

**Goals**
- A calm, professional dark UI: neutral graphite surfaces and **one** teal accent. Purple is no longer the brand colour.
- Clear hierarchy: one primary action (**Start run**), secondary actions grouped, Pause/Stop disabled when they can't be used.
- **Remove duplicated data.** The old *Live Metrics* block repeats the four *Counters*. Delete it; the Counters tiles now carry the progress bars.
- Configuration is organised into **tabs** (Device / MQTT / Fetch) instead of stacked accordions.
- Consistent type: a sans-serif for labels and copy, a monospace for every number, IMEI, timestamp, path and topic.
- No emoji icons. Use outline line icons only.
- Keyboard and screen-reader accessible (section 8).

**Non-goals**
- No changes to how runs execute, how data is fetched, MQTT publishing, file batching, export formats, or settings storage.
- No new backend features. Where this spec mentions a behaviour the app doesn't support yet (for example, a "Validating…" state), implement only the visual part that maps onto what already exists, and leave a `TODO` comment.

---

## 2. What changes compared with the current UI (summary)

| Current UI | Graphite |
|---|---|
| Purple-on-black theme, purple accents everywhere | Graphite neutrals + single teal accent `#2EC4B6` |
| Logo "◉ DeviceSimulator" in the top bar | Teal logo tile at the top of the icon rail; plain text "DeviceSimulator" in the header |
| "Idle" chip next to the logo | Status pill after the breadcrumb `Tests › {test name}` |
| REGION dropdown + `IN → EU` chip in the top bar | "Region" select on the header's right side. The route now appears on the selected test card and in the Run status card. |
| DRY RUN toggle | "Dry run" switch in the header |
| START / STOP / PAUSE / RESET / Validate, all equal weight | `Validate`, `Reset` (secondary) · `[Pause │ Stop]` (grouped, disabled when idle) · **`Start run`** (primary, far right) |
| Icon rail with "EU" badge at the bottom | Same rail (56 px). The EU badge is removed because the region lives in the header. Keep it only if it does more than show the region. |
| TESTS list + "+" | "TESTS" section label + `New test` icon button; selected test shown as a card with chips |
| DEVICE accordion (label-left rows) | **Device tab** with three groups: *Route*, *Transport*, *Time window* |
| MQTT accordion | **MQTT tab** |
| FETCH `CROSS` accordion (API base, API token) | **Fetch tab** with a `CROSS` badge on the tab |
| Ring + Elapsed + Phase + LIVE METRICS bars | **Run status card**: ring, elapsed time, phase, and a Route / Window / History end summary. LIVE METRICS is **deleted**. |
| COUNTERS: 4 cards with emoji icons | 4 tiles: colour square, label, line icon, big mono number, caption, thin progress bar |
| PIPELINE STEPS: 4 separate cards joined by `›` | One joined strip of 4 cells numbered `01`–`04`, each with a status chip |
| Phase 1 / Phase 2 cards | Phase 1 card with 3 cells divided by hairlines; Phase 2 is a collapsible row |
| LOGS panel: ALL/INFO/WARN/ERR, ×, export buttons | Same features, restyled: segmented filter, **search field**, empty state, export footer |

---

## 3. Design tokens

Add these as CSS custom properties (or the equivalent in the project's theme system: Tailwind config, styled-components theme, MUI theme, etc.). Use **tokens, not raw hex**, in components.

```css
:root {
  /* Accent */
  --accent:            #2EC4B6;  /* primary button, active tab underline, switch "on", active rail icon, focus ring */
  --accent-on:         #06201D;  /* text/icon on top of --accent */
  --accent-link:       #5EEAD4;  /* links */
  --accent-link-hover: #99F6E4;
  --accent-soft-bg:    #16302C;  /* tinted badge background (CROSS, Running chip) */
  --accent-soft-fg:    #7EE2D6;  /* tinted badge text */
  --accent-soft-border:#1F4A44;

  /* Surfaces (darkest → lightest) */
  --bg-rail:   #0B0D10;  /* icon rail */
  --bg-app:    #0E1013;  /* page background + input fields */
  --bg-panel:  #121519;  /* header, config sidebar, logs sidebar */
  --bg-card:   #16191E;  /* cards, tiles, secondary buttons */
  --bg-raised: #1B1F25;  /* selected items, pills, active segment */

  /* Borders */
  --border-subtle: #1E2228;  /* panel dividers */
  --border:        #252A31;  /* card borders, progress tracks, hairline grids */
  --border-strong: #2E343C;  /* inputs, buttons, pills */

  /* Text */
  --text-1:     #E6E8EB;  /* primary */
  --text-pill:  #C3CAD3;  /* pill/badge text, tile labels */
  --text-2:     #A7AFB9;  /* field labels, secondary copy */
  --text-3:     #8A939E;  /* section labels, captions, meta */
  --text-muted: #7D8692;  /* disabled text, placeholders, decorative icons */

  /* Data series (fixed, never themed) */
  --data-packets:  var(--accent);
  --data-historic: #6CA8FF;
  --data-gps:      #5BD28A;
  --data-obd:      #F2A93B;

  /* Stream badges (Phase 1 A/B/C) */
  --stream-a-bg: #1A2638; --stream-a-fg: #8EBBFF;
  --stream-b-bg: #17301F; --stream-b-fg: #7FE0A6;
  --stream-c-bg: #33270F; --stream-c-fg: #F5BF63;

  /* Status (pills, chips, ring) */
  --status-idle:      #8A939E;
  --status-info:      #6CA8FF;                                   /* validating */
  --status-running:   var(--accent);
  --status-warn:      #F2A93B;  --warn-bg: #33270F;  --warn-border: #4A3814;  --warn-fg: #F5BF63;   /* paused, warnings, dry run */
  --status-success:   #5BD28A;  --success-bg: #17301F; --success-border: #23452D; --success-fg: #7FE0A6;
  --status-error:     #F87171;  --error-bg: #3A1A1C;  --error-border: #5A2629;  --error-fg: #FCA5A5;

  /* Radii */
  --r-xs: 4px;   /* small tags/chips */
  --r-sm: 6px;   /* inputs, buttons, segments */
  --r-md: 8px;   /* rail buttons, selected test card, segmented container */
  --r-lg: 10px;  /* tiles, pipeline strip, logo tile */
  --r-xl: 12px;  /* large cards */
  --r-pill: 999px;

  /* Fonts */
  --font-sans: 'IBM Plex Sans', system-ui, sans-serif;
  --font-mono: 'IBM Plex Mono', ui-monospace, monospace;
}
```

**Global basics**
```css
*, *::before, *::after { box-sizing: border-box; }
body { margin: 0; background: var(--bg-app); color: var(--text-1);
       font-family: var(--font-sans); font-size: 14px; line-height: 1.45; }
button, input, select { font: inherit; color: inherit; }
button { cursor: pointer; }
input::placeholder { color: var(--text-muted); }
:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
a { color: var(--accent-link); text-decoration: none; }
a:hover { color: var(--accent-link-hover); text-decoration: underline; }
```
`box-sizing: border-box` matters: the layout's flex bases (150 px tiles, 340 px sidebar) assume it. Without it, the counter tiles and pipeline cells wrap onto two rows at 1440 px.

### 3.1 Fonts
- **IBM Plex Sans**: weights 400, 500, 600.
- **IBM Plex Mono**: weights 400, 500.
- This is a desktop app, so **self-host** the fonts so they work offline: `npm i @fontsource/ibm-plex-sans @fontsource/ibm-plex-mono`, then import `400.css`, `500.css`, `600.css` / `400.css`, `500.css`. Google Fonts is acceptable only if the app is always online.

### 3.2 Type scale

| Role | Font | Size / weight | Extras |
|---|---|---|---|
| Brand "DeviceSimulator" | Sans | 15 / 600 | letter-spacing −0.01em |
| Section label (TESTS, ROUTE, COUNTERS, PIPELINE, LOGS, ELAPSED…) | Sans | 11 / 600 | UPPERCASE, letter-spacing 0.08em, `--text-3` |
| Card title (Phase 1, step names) | Sans | 14 / 600 | |
| Body / inputs / buttons / tabs | Sans | 13 / 400–500 | buttons and tabs 500, primary button 600 |
| Field label (left column) | Sans | 13 / 400 | `--text-2` |
| Caption / helper | Sans | 12 / 400 | `--text-3` |
| Chip / status text | Sans | 11–12 / 500 | |
| Elapsed timer | Mono | 36 / 500 | line-height 1, letter-spacing −0.02em |
| Counter value | Mono | 30 / 500 | line-height 1 |
| Ring percentage | Mono | 24 / 500 | |
| IMEI, dates, URLs, values in rows | Mono | 12.5–13 / 400 | |
| Meta (topic, file path, seq-id) | Mono | 11.5 / 400 | `--text-3` |
| Step index `01`–`04`, badges | Mono | 10–11 | |

Use `font-variant-numeric: tabular-nums` on anything that updates live (timer, counters, percentages) so digits don't jitter.

### 3.3 Spacing and sizing
- Base unit 4 px. Common gaps: 2, 4, 6, 8, 10, 12, 16, 20, 24, 32.
- Control heights: **36** for inputs, selects and header buttons; 34 for buttons inside the Pause/Stop group; **32** for small icon buttons (New test, close logs, export); **40** for rail buttons and switch rows; **42** for config tabs; **30** for log filter segments.
- Panel padding: 16 px. Main column padding: 20 px top, 24 px sides, 28 px bottom. Gap between main sections: 20 px. Gap between tiles: 12 px.

### 3.4 Icons
- Outline icons on a 24 × 24 viewBox, **stroke 1.75**, round caps and joins, `currentColor`. Sizes: 18 (rail), 16 (buttons, tiles, steps), 14–15 (chevrons, inline input icons).
- If the project already has an icon set, use it. Otherwise **Lucide** matches. Suggested names:

| Usage | Lucide icon |
|---|---|
| Logo tile | `activity` (on accent tile, colour `--accent-on`, stroke 2) |
| Rail: Run (active) | `play` |
| Rail: Tests | `list` |
| Rail: Routes | `arrow-left-right` |
| Rail: Triggers | `zap` |
| Rail: Devices | `box` |
| Rail: Dashboards | `layout-grid` |
| Rail: History | `clock` |
| Rail: Analytics | `trending-up` |
| Rail: Integrations | `diamond` (or keep the current icon) |
| Rail: Settings | `sliders-horizontal` |
| Validate | `circle-check` |
| Reset | `rotate-ccw` |
| Pause / Stop | `pause` / `square` |
| Start run | `play` (filled, 14 px) |
| New test | `plus` |
| Select chevron / breadcrumb | `chevron-down` / `chevron-right` |
| Date fields | `calendar` |
| Log search | `search` |
| Close logs | `x` |
| Logs empty state | `terminal` |
| Counter: Total packets | `box` |
| Counter: Historic | `history` |
| Counter: Live GPS | `map-pin` |
| Counter: Live OBD | `gauge` |
| Step: Fetch | `arrow-down-to-line` |
| Step: Split | `split` |
| Step: Batch | `layers` |
| Step: MQTT | `radio` |

**The rail labels above are guesses.** The current rail has icons without visible labels, so keep each rail item's current destination and use its real name for the `aria-label` and tooltip.

---

## 4. Layout

```
┌────┬──────────────────────────────────────────────────────────────────────────┐
│    │ HEADER  DeviceSimulator │ Tests › Main 27 Sep… (●Idle)   Region[▾] Dry run ○ │ Validate  Reset  [Pause│Stop]  ▶Start run │
│ R  ├───────────────┬──────────────────────────────────────────┬───────────────┤
│ A  │ CONFIG        │ MAIN                                     │ LOGS          │
│ I  │ 340px         │ flexible (min 560px)                     │ 300px         │
│ L  │ Tests         │ Run status card                          │ header + ×    │
│    │ Tabs          │ COUNTERS (4 tiles)                       │ filter + search│
│ 56 │ Device form   │ PIPELINE (4 cells)                       │ entries       │
│ px │ footer link   │ Phase 1 card (3 cells)                   │ export footer │
│    │               │ Phase 2 row                              │               │
└────┴───────────────┴──────────────────────────────────────────┴───────────────┘
```

- **App shell**: a row with the **rail** (56 px, full height) and a **content column** (header + body).
- **Body**: three columns. Config sidebar `flex: 0 0 340px`, main `flex: 1 1 auto; min-width: 560px`, logs sidebar `flex: 0 0 300px`.
- **Desktop behaviour (window ≥ 1280 px wide)**: lock the shell to the window height (`height: 100vh`). The header stays fixed, and **each of the three columns scrolls on its own** (`overflow-y: auto`). At 1440 × 900 everything fits without scrolling (see the desktop PNG).
- **900–1279 px**: the logs sidebar moves **below** the main column, full width of the body, height 320 px, still scrollable inside.
- **< 900 px**: everything stacks in document flow: rail becomes a horizontal row of icons at the top, the header wraps (actions on a second line), then Config, then Main, then Logs (see the mobile PNG). No horizontal page scroll.
- The reference HTML gets this responsiveness with `flex-wrap` and flex-basis values. Media queries are an equally good way to do it.

---

## 5. Component specifications

Values below come from `graphite-reference.html`. "Section label" means the 11 px uppercase style from 3.2.

### 5.1 Icon rail
- Width 56, padding 12 px 8 px, background `--bg-rail`, right border 1 px `--border-subtle`. Items stacked with 4 px gaps.
- **Logo tile**: 40 × 40, radius 10, background `--accent`, icon `--accent-on` 20 px, 8 px margin below.
- **Nav buttons**: real `<button>` or `<a>` elements, 40 × 40, radius 8, icon 18 px.
  - Inactive: transparent background and border, icon `--text-3`. Hover: background `--bg-card`, icon `--text-1`.
  - Active (current screen): background `--bg-raised`, 1 px border `--border-strong`, icon `--accent`, `aria-current="page"`.
  - Each needs an `aria-label` and a tooltip with the same text.

### 5.2 Header
- Background `--bg-panel`, bottom border `--border-subtle`, padding 12 px 20 px, flex with `space-between`, wraps on narrow windows.
- **Left group** (gap 14):
  1. "DeviceSimulator" (brand style).
  2. Vertical divider 1 × 20, `--border-strong`.
  3. Breadcrumb `<nav aria-label="Breadcrumb">`: link **Tests** (13 px, `--text-2`) → chevron-right 14 → current test name (13/500, `--text-1`, no wrap, ellipsis if long).
  4. **Status pill** (see 6.1): height 24, padding 0 10, radius pill, background `--bg-raised`, border `--border-strong`, 12 px `--text-pill`, 6 px dot.
  5. When Dry run is ON: an extra tag `DRY RUN` (mono 10.5, padding 1 6, radius 4, background `--warn-bg`, text `--warn-fg`).
- **Right group** (gap 8):
  1. Label "Region" (12 px `--text-3`) + **Select** (Region). Same options as today. Example value: `EU North — Ireland`.
  2. **Dry run** switch button: text "Dry run" (13 px `--text-2`) followed by a small switch (track 30 × 18, knob 14).
  3. Divider 1 × 24.
  4. **Validate**: secondary button, icon `circle-check`.
  5. **Reset**: secondary button, icon `rotate-ccw`.
  6. **Pause │ Stop** group: one bordered container (radius 6, overflow hidden) holding two buttons of height 34 with a 1 px divider between them.
  7. **Start run**: primary button, filled play icon 14, gap 8, label "Start run".

**Buttons**
| Variant | Spec |
|---|---|
| Primary | h 36, padding 0 16, radius 6, background `--accent`, text `--accent-on` 13/600, no border. Hover: lighten ~6%. Active: darken ~6%. |
| Secondary | h 36, padding 0 12, radius 6, background `--bg-card`, border `--border-strong`, text `--text-1` 13/500, icon 16 + gap 6. Hover: background `--bg-raised`. |
| Icon (small) | 32 × 32, radius 6, either secondary style or transparent (ghost) with icon `--text-3`. |
| Disabled (any) | text and icon `--text-muted`, cursor `not-allowed`, no hover change; use the real `disabled` attribute. |

### 5.3 Config sidebar (left, 340 px)
Background `--bg-panel`, right border `--border-subtle`. Top to bottom:

**a) Tests header**: padding 14 16 10. Section label "TESTS" on the left; **New test** icon button (32 × 32, secondary, `plus`, `aria-label="New test"`) on the right. Keeps the current "+" behaviour.

**b) Test list**: padding 0 12 14, bottom border `--border-subtle`.
- Each test is a full-width button: padding 10 12, radius 8, gap 6, column layout.
  - Name: 14/500, e.g. `Main 27 September (re-run)`.
  - Chips row: mono 11.5, `--text-2`, each chip padding 1 6, radius 4, background `--bg-app`, border `--border-strong`. Chips: route (`IN → EU`) and mode (`simulate`). Use the same values the old item showed.
- **Selected** item: background `--bg-raised`, border `--border-strong`, `aria-current="true"`. Unselected: transparent; hover `--bg-card`.
- If there are many tests, the list gets `max-height: 240px; overflow-y: auto`.

**c) Tabs**: `role="tablist"` with tabs **Device**, **MQTT**, **Fetch**. Padding 0 12, gap 4, bottom border `--border-subtle`.
- Tab: height 42, padding 0 10, 13/500. Selected: text `--text-1`, 2 px bottom border `--accent`, `aria-selected="true"`. Others: `--text-2`, transparent border.
- The Fetch tab shows the fetch-mode badge next to its label: mono 10, padding 1 5, radius 4, background `--accent-soft-bg`, text `--accent-soft-fg`, e.g. `CROSS`. Show whatever mode value is currently displayed in the old "FETCH CROSS" header.
- If the Fetch config has a problem (e.g. API token not set), show a 6 px `--status-warn` dot after the badge.
- Arrow keys move between tabs. The tab panel has `role="tabpanel"`.

**d) Device tab panel**: padding 16, groups separated by 20 px. Each group is a `<fieldset>` with a `<legend>` styled as a section label (10 px below it).

*Form row pattern* (used for every field): flex row, gap 12, align center. `<label>` is fixed at 96 px, 13 px, `--text-2`. The control fills the rest. Rows are 8 px apart.

*Inputs and selects*: height 36, background `--bg-app`, border 1 px `--border-strong`, radius 6, padding 0 10, 13 px `--text-1`. Selects use `appearance: none`, right padding 32 and a `chevron-down` 14 px (`--text-3`) placed 10 px from the right edge. Focus: border `--accent` plus the global focus ring. Mono font for IMEIs and dates.

**Group "Route"**
| Label | Control | Example value |
|---|---|---|
| Source region | Select | `IN Central — Pune` |
| Source IMEI | Text input, mono, `inputmode="numeric"` | `866308064386378` |
| Target region | Select | `EU North — Ireland` |
| Target IMEI | Text input, mono, numeric | `866308068006683` |
| Switch | Select | `Simulate` (same options as today) |

**Group "Transport"**: three **switch rows**, 2 px apart. Each row is one `<button role="switch" aria-checked>` with min-height 40, full width, label on the left (13 px `--text-1`) and the switch on the right.
| Row label | Maps to old control | Example state |
|---|---|---|
| Secure connection `(TLS)`, where "(TLS)" is in `--text-3` | CONNECTION: Secure (TLS) | Off |
| Encrypt payload | ENCRYPT | On |
| L1 enrich | L1 ENRICH | On |

*Switch visual*: track 34 × 20, radius pill, padding 2. **Off**: track `--border-strong`, knob 16 px `--text-3`, knob on the left. **On**: track `--accent`, knob `--bg-app`, knob on the right. Animate the knob position over 150 ms.

**Group "Time window"**: date-time fields. Mono, with a `calendar` icon 15 px on the right (input right padding 32). Keep the existing date-picker behaviour, and only restyle the trigger or input.
| Label | Example |
|---|---|
| From | `20 Sep 2026  00:00` |
| Until | `30 Sep 2026  00:00` |
| History end | `20 Sep 2026  06:30` |

**e) MQTT tab panel**: render **all existing MQTT fields** with the same form-row pattern and the same input, select and switch styles. Use mono for host, port, topic and client-id values. Group them with fieldset legends if there are more than ~5 fields (e.g. "Broker", "Auth", "Topics").

**f) Fetch tab panel**: same patterns.
- **API base**: text input, mono, full URL (e.g. `https://apis.intangles…`).
- **API token**: password input with a show/hide icon button (32 × 32, ghost, inside the field on the right). When empty, show helper text under the row: "Not set" in 12 px `--warn-fg`, aligned with the control column.
- Any other existing Fetch fields follow the same pattern.

**g) Footer**: padding 12 16, top border `--border-subtle`, right-aligned link **"Edit in Settings →"** (13/500, `--accent-link`). Same destination as today.

**h) Locked state**: while a run is Running or Paused, every config control and the test list are disabled (inputs show text `--text-2` on `--bg-panel`). At the top of the tab panel, show a one-line note: lock icon 14 + "Configuration is locked while a run is active." (12 px `--text-3`).

### 5.4 Main column
Padding 20 24 28, sections stacked with a 20 px gap.

**a) Run status card**: background `--bg-card`, border `--border`, radius 12, padding 20 24. A flex row that wraps, items centred, gap 20 px vertical / 32 px horizontal. Three parts:
1. **Progress ring**, 108 × 108. SVG circle with r = 46 and stroke 8. Track `--border`. The progress arc uses the status colour (6.1), `stroke-linecap: round`, rotated −90°. `stroke-dasharray = 289.03` (2π·46), `stroke-dashoffset = 289.03 × (1 − progress)`. Centred text: percentage in mono 24/500 (e.g. `0%`), then "overall" (11 px `--text-3`, 4 px above). Add `role="progressbar"` with `aria-valuenow`, `aria-valuemin="0"`, `aria-valuemax="100"` and `aria-label="Overall progress"`.
2. **Timer block** (min-width 160, gap 8): section label "ELAPSED"; value in mono 36/500 (`00:00.0`, format in 6.3); then a row with "Phase" (13 px `--text-2`) and a **phase pill** (height 22, padding 0 8, 12 px, same style as the status pill).
3. **Summary** (`flex: 1 1 260px`, padding-left 24, left border `--border`, rows 10 px apart). Each row has a label (92 px, 13 px `--text-3`) and a value. These values are derived from the selected test's config and are read-only:
   - **Route**: `IN Central — Pune → EU North — Ireland` (13/500; the arrow in `--text-3`)
   - **Window**: `20 Sep 2026 00:00 → 30 Sep 2026 00:00` (mono 12.5)
   - **History end**: `20 Sep 2026 06:30` (mono 12.5)

**b) Counters**: section label "COUNTERS", then a wrapping row of 4 tiles with 12 px gaps. Each tile is `flex: 1 1 150px`.
- Tile: background `--bg-card`, border `--border`, radius 10, padding 14 16, column layout with 10 px gaps:
  1. Header row: 8 × 8 colour square (radius 2) + label (13/500 `--text-pill`) on the left; icon 16 px in `--text-muted` on the right.
  2. Value: mono 30/500, tabular numbers, thousands separators (use the app's existing number formatting).
  3. Caption: 12 px `--text-3`.
  4. Progress bar: height 3, radius 2, track `--border`, fill in the series colour (rules in 6.4).

| Tile | Colour | Icon | Caption | Old source |
|---|---|---|---|---|
| Total packets | `--data-packets` | box | OBD + GPS fetched | COUNTERS › TOTAL PACKETS |
| Historic | `--data-historic` | history | Before history-end | COUNTERS › HISTORIC |
| Live GPS | `--data-gps` | map-pin | Phase 1 & 2 | COUNTERS › LIVE GPS |
| Live OBD | `--data-obd` | gauge | Phase 1 accumulate | COUNTERS › LIVE OBD |

**Delete the old "LIVE METRICS" block** (Packets / Historic / Live GPS / Live OBD bars). It duplicated these four numbers.

**c) Pipeline**: section label "PIPELINE", then an ordered list `<ol>` styled as one joined strip. Container: `display: flex; flex-wrap: wrap; gap: 1px; background: var(--border); border: 1px solid var(--border); border-radius: 10px; overflow: hidden`. The 1 px gap shows through as hairline dividers.
- Each step `<li>`: `flex: 1 1 150px`, background `--bg-card`, padding 14 16, column layout with 8 px gaps:
  1. Top row: step index (mono 11 `--text-3`, `01`–`04`) on the left, **status chip** (6.2) on the right.
  2. Title: icon 16 + name, 14/600, gap 8.
  3. Detail: 12 px `--text-2` label followed by a mono value in `--text-1`.
  4. While Running: a 2 px progress bar along the bottom edge of the cell, fill `--accent` (indeterminate shimmer if there's no percentage).

| # | Step | Icon | Detail label | Detail value (idle) |
|---|---|---|---|---|
| 01 | Fetch | arrow-down-to-line | Pages | — |
| 02 | Split | split | Hist / Live | — |
| 03 | Batch | layers | Files | — |
| 04 | MQTT | radio | Broker | — |

Show whatever values the old cards showed (`Pages: …`, `Hist/Live: …`, `Files: …`, `Broker: …`).

**d) Phase 1 card**: background `--bg-card`, border `--border`, radius 12, overflow hidden.
- Header: padding 14 18, bottom border `--border`. Title "Phase 1" (14/600) followed by "Concurrent streams" (13/400 `--text-2`, gap 10). Status chip on the right.
- Body: `display: flex; flex-wrap: wrap; gap: 1px; background: var(--border)` (hairline dividers). Three cells, each `flex: 1 1 200px`, background `--bg-card`, padding 16 18, column layout with 10 px gaps:
  1. Badge 22 × 22 (radius 6, mono 11, colours from the stream tokens) + name (13/600).
  2. Metric: label 13 px `--text-2` + mono value `--text-1`.
  3. Meta: mono 11.5 `--text-3`.

| Badge | Name | Metric label | Value (idle) | Meta |
|---|---|---|---|---|
| A | Historic upload | Batch | `0 / —` | `seq-id:1 · encrypted` |
| B | GPS L1 MQTT | Published | `0` | `topic: TARGET/obd · l:"1"` |
| C | OBD accumulation | Rows | `0` | `live_obd_TARGET.db` |

The meta values are live data from the existing UI. Bind them; don't hard-code.

**e) Phase 2**: a card with the same style. Its header is one full-width `<button aria-expanded>`: chevron-right 16 (`--text-3`, rotates 90° when expanded) + "Phase 2" (14/600) + "Sequential" (13/400 `--text-2`), status chip on the right. Padding 14 18. When expanded, render the existing Phase 2 content using the same cell pattern as Phase 1. Collapsed by default. Expand it automatically when Phase 2 starts.

### 5.5 Logs sidebar (right, 300 px)
Background `--bg-panel`, left border `--border-subtle`, column layout.

1. **Header** (padding 14 16 10): section label "LOGS" + **count badge** (mono 11, padding 0 6, radius 4, background `--bg-raised`, border `--border-strong`, text `--text-pill`) showing the total number of entries. On the right, a **Close** icon button (32 × 32 ghost, `x`, `aria-label="Close logs"`).
2. **Controls** (padding 0 16 14, gap 10, bottom border `--border-subtle`):
   - **Segmented filter** `role="group" aria-label="Filter by level"`: container padding 3, gap 2, background `--bg-app`, border `--border`, radius 8. Four equal buttons (`flex: 1`, height 30, radius 6, 12/500): **All · Info · Warn · Error**, using `aria-pressed`. Selected: background `--bg-raised`, border `--border-strong`, text `--text-1`. Others: transparent, `--text-2`. These map to the old ALL/INFO/WARN/ERR.
   - **Search field**: `type="search"`, height 36, `search` icon 15 placed 10 px from the left (left padding 32), placeholder "Filter messages", visually hidden label "Filter log messages". Case-insensitive substring filter on the message text. This is new; if wiring it is non-trivial, add it as client-side filtering of the loaded entries.
3. **Entries** (fills the remaining height, scrolls): see 6.5. **Empty state**, centred, 10 px gaps: 44 × 44 tile (radius 10, background `--bg-card`, border `--border`, `terminal` icon 20 `--text-3`), "No log output yet" (13/500), "Entries stream here as soon as the run starts." (12 px `--text-3`, max-width 220, centred).
4. **Footer** (padding 12 16, top border `--border-subtle`): "Export" (12 px `--text-3`, pushed left with `margin-right: auto`) + three small buttons **.txt**, **.json**, **.csv** (height 32, padding 0 10, secondary style, mono 12). Same export behaviour as today.

**Closing the panel** hides the logs column so the main column grows. Add a **Logs** icon button (36 × 36 secondary, `terminal` icon, `aria-label="Show logs"`, with the count as a small badge) in the header right group before **Validate**. It is visible only while the panel is closed. Remember the open/closed state in the same place the app keeps other UI preferences.

---

## 6. States and behaviour

### 6.1 Run status (header pill, phase pill, ring colour)
Map these to the app's existing run states. Use the closest match and skip states the app doesn't have.

| State | Pill text | Dot | Pill bg / border / text | Ring arc colour |
|---|---|---|---|---|
| Idle | Idle | `--status-idle` | `--bg-raised` / `--border-strong` / `--text-pill` | (no arc, 0%) |
| Validating | Validating… | `--status-info` | `--bg-raised` / `--border-strong` / `--text-pill` | — |
| Running | Running | `--accent`, **pulsing** (opacity 1 → 0.35, 1.2 s, infinite) | `--accent-soft-bg` / `--accent-soft-border` / `--accent-soft-fg` | `--accent` |
| Paused | Paused | `--status-warn` | `--warn-bg` / `--warn-border` / `--warn-fg` | `--status-warn` |
| Completed | Completed | `--status-success` | `--success-bg` / `--success-border` / `--success-fg` | `--status-success` |
| Stopped | Stopped | `--status-idle` | neutral (as Idle) | `--text-3` |
| Failed | Failed | `--status-error` | `--error-bg` / `--error-border` / `--error-fg` | `--status-error` |

**Phase pill** text: `Idle` → `Phase 1` → `Phase 2` → `Done` (or the app's existing phase names). It uses the same colours as the status pill.

Respect `prefers-reduced-motion`: no pulsing and no shimmer; use static colours.

### 6.2 Step and phase status chips
11/500, padding 1 7, radius 4, 1 px border.
| Status | Background | Border | Text |
|---|---|---|---|
| Pending | `--bg-raised` | `--border-strong` | `--text-2` |
| Running | `--accent-soft-bg` | `--accent-soft-border` | `--accent-soft-fg` |
| Done | `--success-bg` | `--success-border` | `--success-fg` |
| Skipped | transparent | `--border` | `--text-muted` |
| Error | `--error-bg` | `--error-border` | `--error-fg` |
When a step is Running or Done, also colour its title icon (accent or success).

### 6.3 Controls by run state

| State | Validate | Reset | Pause | Stop | Primary button | Region / Dry run / Config |
|---|---|---|---|---|---|---|
| Idle | ✅ | ✅ | disabled | disabled | **Start run** ✅ | editable |
| Validating | disabled, label "Validating…" with a small spinner | disabled | disabled | disabled | Start run disabled | locked |
| Running | disabled | disabled | ✅ | ✅ | "Running…" disabled | locked |
| Paused | disabled | disabled | disabled | ✅ | **Resume** ✅ (play icon) | locked |
| Completed / Stopped / Failed | ✅ | ✅ (clears counters, steps and timer back to Idle) | disabled | disabled | **Start run** ✅ | editable |

- **Stop** asks for confirmation if the app already does; don't add new dialogs otherwise.
- **Elapsed** format: `mm:ss.t` under an hour, `h:mm:ss.t` after. Update every 100 ms while Running, freeze while Paused.
- Keyboard shortcuts are optional. If you add them, don't use bare single-letter keys globally.

### 6.4 Counter progress bars
- **Total packets**: fill = overall run progress (same value as the ring).
- **Historic / Live GPS / Live OBD**: fill = that counter ÷ Total packets (its share). With Total = 0, show the empty track.
- Animate width changes over 200 ms (skip with reduced motion).

### 6.5 Log entries
- One row per entry. Grid columns `[time 72px][level 44px][message 1fr]`, gap 8, padding 4 16, mono 12 px, line-height 1.6. Messages wrap (`word-break: break-word`).
- Time `HH:MM:SS.s` in `--text-muted`. Level tag: `INFO` in `--text-2`, `WARN` in `--warn-fg`, `ERR` in `--error-fg`. Message in `--text-1`.
- WARN rows get background `#1F1A10`; ERR rows get `#241315`. Row hover: `--bg-card`.
- **Auto-scroll** to the newest entry. If the user scrolls up, stop auto-scrolling and show a floating **"Jump to latest"** pill at the bottom of the list (height 28, radius pill, `--bg-raised`, border `--border-strong`, 12 px). Clicking it scrolls down and resumes auto-scroll.
- The level filter and the search combine (AND). The count badge always shows the total, not the filtered count.
- The list should virtualise or cap rendered rows if logs can get large (thousands of lines).

### 6.6 Validation feedback
- Keep the existing validation logic. Show problems **inline**: the field border turns `--status-error`, and a 12 px `--error-fg` message appears under the row (indented to the control column). If the field is on another tab, add a 6 px `--status-error` dot on that tab label.
- Also send validation results to the log as INFO, WARN or ERR entries.

### 6.7 Dry run
When ON: the switch is on (`--accent`), the header shows the `DRY RUN` tag, and the primary button label becomes **"Start dry run"**.

---

## 7. Hover, focus and motion
- Every interactive element has a visible `:focus-visible` ring (2 px `--accent`, offset 2).
- Hover states are subtle: background moves one surface step up (`--bg-card` → `--bg-raised`). Primary button: lighten.
- Transitions: 120–150 ms ease-out on background, border and colour; 150 ms on the switch knob. No bounce, no glow, no gradients, no drop shadows on cards.

---

## 8. Accessibility requirements
- Use real elements: `<button>` for actions and toggles, `<a>` for navigation, `<label for>` on every input and select, `<fieldset>` + `<legend>` for the form groups, `<ol>` for the pipeline.
- Toggles: `role="switch"` + `aria-checked`. Tabs: `role="tablist"`, `role="tab"`, `aria-selected`, `aria-controls`, `role="tabpanel"`. Log filters: `aria-pressed`. Phase 2 toggle: `aria-expanded`. Current rail item: `aria-current="page"`. Selected test: `aria-current="true"`.
- Icon-only buttons need `aria-label`: rail items, New test, Close logs, Show logs, show/hide token.
- Disabled controls use the `disabled` attribute.
- Contrast: `--text-1`, `--text-pill`, `--text-2`, `--text-3` and `--accent-link` are all at least 4.5:1 on every surface token. `--text-muted` passes on `--bg-app`, `--bg-panel` and `--bg-card` but **not** on `--bg-raised` (4.49:1), so use it only for disabled text, placeholders and decorative icons. Don't use it for readable copy on raised surfaces.
- Live regions: announce run status changes with `aria-live="polite"` on the status pill. Do **not** make the log list a live region.
- Status must never rely on colour alone. Every status also has a text label (pill or chip).

---

## 9. Copy deck (exact strings)

| Where | Text |
|---|---|
| Brand | DeviceSimulator |
| Breadcrumb | Tests › {test name} |
| Header | Region · Dry run · Validate · Reset · Pause · Stop · Start run (Start dry run / Resume / Running…) |
| Sidebar | TESTS · New test (aria) · Device · MQTT · Fetch · ROUTE · Source region · Source IMEI · Target region · Target IMEI · Switch · TRANSPORT · Secure connection (TLS) · Encrypt payload · L1 enrich · TIME WINDOW · From · Until · History end · Edit in Settings → |
| Fetch tab | API base · API token · Not set |
| Lock note | Configuration is locked while a run is active. |
| Run card | overall · ELAPSED · Phase · Route · Window · History end |
| Counters | COUNTERS · Total packets / OBD + GPS fetched · Historic / Before history-end · Live GPS / Phase 1 & 2 · Live OBD / Phase 1 accumulate |
| Pipeline | PIPELINE · Fetch / Pages · Split / Hist / Live · Batch / Files · MQTT / Broker |
| Phases | Phase 1 · Concurrent streams · Historic upload / Batch · GPS L1 MQTT / Published · OBD accumulation / Rows · Phase 2 · Sequential |
| Status | Idle · Validating… · Running · Paused · Completed · Stopped · Failed · Pending · Done · Skipped · Error |
| Logs | LOGS · All · Info · Warn · Error · Filter messages · No log output yet · Entries stream here as soon as the run starts. · Jump to latest · Export · .txt · .json · .csv · Close logs / Show logs (aria) |

Use `—` (em dash) for "no value yet" and `→` for route/range arrows.

---

## 10. Implementation plan (do it in this order)
1. **Survey the code.** Find the Run screen and its parts (top bar, rail, tests list, device/MQTT/fetch sections, ring/elapsed/phase, live metrics, counters, pipeline, phases, logs), the styling approach, and where run state lives. List the existing state values for run status, phase and step status so you can map them to section 6.
2. **Tokens and fonts.** Add the section 3 tokens to the global theme and self-host IBM Plex Sans/Mono. Add the global basics, including `box-sizing: border-box`.
3. **Primitives.** Create or restyle shared components: `Button` (primary / secondary / ghost / icon), `Select`, `TextInput` (mono variant, trailing icon slot), `Switch`, `Tabs`, `SegmentedControl`, `StatusPill`, `StatusChip`, `SectionLabel`, `Card`, `FormRow`. Every screen element below should be built from these.
4. **Layout shell.** Rail + header + three-column body with the responsive rules in section 4.
5. **Rebuild each region**: header → config sidebar (with tabs) → run status card → counters → pipeline → phases → logs. Bind each one to the **existing** state and handlers.
6. **Delete** the Live Metrics block and its now-unused styles. Remove emoji icons.
7. **States**: implement sections 6.1–6.7 against the real state machine.
8. **Clean up**: remove dead purple theme styles, unused CSS and old accordion components that nothing uses any more.
9. **Verify** against the reference PNGs at 1440 × 900 and 390 px wide, then run the checklist below.

---

## 11. Acceptance checklist
- [ ] At 1440 × 900 the Idle screen matches `graphite-reference-desktop.png`. In particular, the 4 counter tiles, 4 pipeline cells and 3 Phase 1 cells each sit on **one row**.
- [ ] At 390 px wide the layout stacks as in `graphite-reference-mobile.png`, with no horizontal scroll.
- [ ] At 1280 px and wider the header stays put and the three columns scroll independently.
- [ ] Only one accent colour (`#2EC4B6`) appears in chrome. Data colours appear only in counters, stream badges and status.
- [ ] Live Metrics is gone. Every number appears once.
- [ ] No emoji anywhere; all icons are 1.75-stroke outline icons.
- [ ] Numbers, IMEIs, times, paths and topics use IBM Plex Mono with tabular numbers.
- [ ] Start run is the only primary button. Pause and Stop are disabled when Idle. The button states follow the 6.3 table.
- [ ] Config is locked during Running and Paused, with the lock note visible.
- [ ] Device / MQTT / Fetch tabs contain **all** fields that existed before. Nothing is lost.
- [ ] Log filter, search, auto-scroll + "Jump to latest", close/re-open, and .txt/.json/.csv export all work.
- [ ] Every control can be reached and operated by keyboard with a visible focus ring. Switches, tabs and filters expose the correct ARIA state.
- [ ] `prefers-reduced-motion` turns off the pulse, shimmer and bar animations.
- [ ] No business-logic, API or config-schema changes in the diff.
