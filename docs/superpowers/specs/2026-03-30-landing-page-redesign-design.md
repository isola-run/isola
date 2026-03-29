# Landing Page Redesign

## Overview

Redesign the isola.run landing page from a hero-only splash into a full single-page site with feature highlights, a Python SDK code example, and a how-it-works flow. Static HTML/CSS only, no JS, same design system.

## Technical Constraints

- Static HTML/CSS in `docs/` directory, hosted on GitHub Pages
- Single `index.html` + `styles.css` (no build process)
- Google Fonts: Comfortaa (400, 500, 600, 700)
- CSS custom properties for all design tokens (defined in `:root`)
- Responsive breakpoints: 640px (mobile), 980px (tablet)
- No JavaScript -- syntax highlighting via CSS `<span>` classes
- Monospace font stack for code: `ui-monospace, "SF Mono", "Cascadia Code", Consolas, monospace`

## Design Tokens (unchanged)

```css
--bg: #060914;
--text: #f6f7ff;
--muted: #d7def6;
--muted-soft: rgba(246, 247, 255, 0.72);
--line: rgba(194, 207, 255, 0.18);
--purple: #7d3ecd;
--indigo: #2e499a;
--cyan: #7ab6e3;
--shadow: 0 34px 100px rgba(0, 0, 0, 0.34);
```

## Page Structure

Six sections in vertical scroll order:

### 1. Header (unchanged)

- Left: `Isola` brand link (`.brand` class)
- Right: `GitHub` pill link (`.repo-link` class with glassmorphism)
- No changes to markup or styling

### 2. Hero (refined)

**Changes from current:**
- Remove `min-height: calc(100vh - 108px)` so content below is visible without scrolling on desktop
- Add a 1-2 sentence description using the existing `.hero-text` class (currently defined in CSS but unused in HTML)

**Content:**
- `h1`: "Secure Sandboxing for Kubernetes"
- `.hero-meta`: "gVisor · Snapshotting · Open source · Developer-first SDKs"
- `.hero-text` (new in HTML): "Run untrusted code in gVisor-isolated containers with instant snapshot and restore. Kubernetes-native, open source, Python SDK."
- `.hero-visual`: Logo with existing shadow/border treatment

**Layout**: Same 2-column grid (`0.82fr / 1fr`), collapses to single column at 980px.

### 3. Features

A section with a `var(--line)` top border, containing 4 cards in a CSS grid.

**Grid layout:**
- `grid-template-columns: repeat(auto-fit, minmax(260px, 1fr))` -- flows naturally from 4 columns on wide screens to 2 on tablet to 1 on mobile
- Gap: `20px`

**Card styling** (glassmorphism, consistent with `.repo-link`):
- `background: rgba(14, 20, 37, 0.74)`
- `backdrop-filter: blur(16px)`
- `border: 1px solid var(--line)`
- `border-radius: 16px`
- `padding: ~24px`
- `box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.08)`

**Card content** (text only, no icons):
- Title: bold, `var(--text)` color
- Description: `var(--muted)` color, 1 line

**Cards:**

| Title | Description |
|-------|-------------|
| gVisor Isolation | Hardware-virtualized sandboxing via gVisor with Kubernetes-native pod scheduling |
| Rootfs Snapshots | Snapshot filesystem changes and restore instantly. Snapshots are stored in a shared bucket, accessible on demand from any node. |
| Python SDK | Create sandboxes, run commands, and manage snapshots in a few lines of code |
| Open Source | Apache 2.0 licensed, built on Kubernetes primitives, no vendor lock-in |

### 4. Code Example

A full-width code block with a small label above it.

**Label**: "Python" in muted-soft text, small font size (~0.82rem), positioned above the code block.

**Code block styling:**
- Same glassmorphism base as feature cards
- `background: rgba(14, 20, 37, 0.74)`
- `backdrop-filter: blur(16px)`
- `border: 1px solid var(--line)`
- `border-radius: 16px`
- `padding: ~24px 28px`
- `overflow-x: auto` for horizontal scroll on small screens
- Font: `ui-monospace, "SF Mono", "Cascadia Code", Consolas, monospace`
- Font size: `~0.88rem`
- Line height: `1.7`

**Syntax highlighting** (CSS-only via `<span>` classes):
- `.kw` (keywords: `from`, `import`, `with`, `as`, `print`): `var(--purple)` or a lighter purple
- `.str` (strings): `var(--cyan)`
- `.comment`: `var(--muted-soft)`
- `.fn` (function/method calls): `var(--text)` (no special color, keeps it clean)

**Code content:**

```python
from isola import Isola

client = Isola()

with client.sandboxes.create(image="alpine:3.21") as sandbox:
    result = sandbox.commands.run("echo", "hello world")
    print(result.stdout)  # "hello world\n"

    # Snapshot the filesystem changes
    client.rootfs_snapshots.create(
        sandbox_id=sandbox.id,
        snapshot_name="my-snapshot",
    )

# Restore from snapshot on any node
with client.sandboxes.create(
    image="alpine:3.21",
    rootfs_snapshot_source="my-snapshot",
) as restored:
    restored.commands.run("cat", "/data/results.json")
```

### 5. How It Works

A horizontal 3-step flow.

**Layout:**
- Desktop: 3-column grid with equal columns
- Steps connected by `border-right: 1px solid var(--line)` on each step except the last
- Mobile (<=640px): stacked vertically, border switches to `border-bottom` on each step except the last

**Each step:**
- Large number (`01`, `02`, `03`) in accent color -- gradient from `var(--purple)` through `var(--indigo)` to `var(--cyan)` across the three steps
- Title: bold, `var(--text)`
- Description: `var(--muted)`, 1 sentence

**Steps:**

| # | Title | Description |
|---|-------|-------------|
| 01 | Create | Spin up a gVisor-isolated sandbox from any container image |
| 02 | Execute | Run commands, read and write files via the Python SDK |
| 03 | Snapshot & Restore | Capture rootfs changes, restore instantly on any node |

### 6. Footer

Minimal, separated by `var(--line)` top border.

**Layout:** Flexbox, space-between.
- Left: "Isola" in `var(--muted-soft)`, small font (~0.82rem)
- Right: "GitHub" text link in `var(--muted-soft)`, same size, hover color `var(--cyan)`

**Spacing:** Modest padding (~24px 0), sits within the same `.page` max-width container.

## Section Spacing

Consistent vertical rhythm between sections:
- Sections separated by `~80px` vertical padding on desktop
- `~48px` on mobile
- Feature, code, and how-it-works sections each wrapped in a `<section>` element with appropriate semantic markup

## Ambient Background

Unchanged. The existing ambient gradients and grid overlay extend behind all new sections naturally since they use `position: fixed`.

## Accessibility

- Semantic HTML: `<header>`, `<main>`, `<section>`, `<footer>`
- Code block uses `<pre><code>` with appropriate structure
- Syntax highlighting spans are purely decorative (no semantic meaning lost without CSS)
- All text meets reasonable contrast against the dark background
- Links are keyboard-focusable
