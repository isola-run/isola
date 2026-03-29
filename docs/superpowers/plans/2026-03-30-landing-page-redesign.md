# Landing Page Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign isola.run from a hero-only splash into a full single-page site with feature highlights, a Python SDK code example, and a how-it-works flow.

**Architecture:** Static HTML/CSS, no JS. Two files: `docs/index.html` and `docs/styles.css`. Each task adds one section (HTML markup + CSS styles) and commits. Sections are added top-to-bottom.

**Tech Stack:** HTML, CSS (custom properties, grid, flexbox, glassmorphism), Google Fonts (Comfortaa)

**Spec:** `docs/superpowers/specs/2026-03-30-landing-page-redesign-design.md`

---

### Task 1: Refine Hero Section

**Files:**
- Modify: `docs/index.html`
- Modify: `docs/styles.css`

This task modifies the existing hero to make room for content below it and adds the missing description paragraph.

- [ ] **Step 1: Update hero HTML — add `.hero-text` paragraph**

In `docs/index.html`, inside the `<section class="hero-copy">` block, add a `<p>` after the `.hero-meta` paragraph:

```html
<section class="hero-copy">
  <h1>Secure Sandboxing for Kubernetes</h1>
  <p class="hero-meta">gVisor · Snapshotting · Open source · Developer-first SDKs</p>
  <p class="hero-text">Run untrusted code in gVisor-isolated containers with instant snapshot and restore. Kubernetes-native, open source, Python SDK.</p>
</section>
```

- [ ] **Step 2: Update hero CSS — remove full-viewport height**

In `docs/styles.css`, in the `.hero` rule, remove the `min-height: calc(100vh - 108px);` line. The hero should take only the space it needs.

Change the `.hero` rule from:

```css
.hero {
  align-items: center;
  display: grid;
  gap: 56px;
  grid-template-columns: minmax(0, 0.82fr) minmax(360px, 1fr);
  min-height: calc(100vh - 108px);
  padding-top: 30px;
}
```

to:

```css
.hero {
  align-items: center;
  display: grid;
  gap: 56px;
  grid-template-columns: minmax(0, 0.82fr) minmax(360px, 1fr);
  padding-top: 30px;
}
```

- [ ] **Step 3: Update `.page` grid — remove `1fr` row stretch**

The `.page` currently uses `grid-template-rows: auto 1fr` which stretches `<main>` to fill the viewport. Change to `auto` only, and remove `min-height: 100vh`:

```css
.page {
  display: grid;
  grid-template-rows: auto auto;
  margin: 0 auto;
  max-width: 1200px;
  padding: 28px 24px 44px;
  position: relative;
  z-index: 1;
}
```

- [ ] **Step 4: Verify in browser**

Open `docs/index.html` in a browser. The hero should no longer fill the full viewport. The description paragraph should appear below the tagline in muted text. On narrow viewports (<980px), it should stack to a single column.

- [ ] **Step 5: Commit**

```bash
git add docs/index.html docs/styles.css
git commit -m "refine hero: add description, remove full-viewport height"
```

---

### Task 2: Add Features Section

**Files:**
- Modify: `docs/index.html`
- Modify: `docs/styles.css`

- [ ] **Step 1: Add features HTML**

In `docs/index.html`, after the closing `</main>` tag for the hero, add the features section. This goes inside `.page`, after `<main class="hero">`:

```html
<section class="features">
  <div class="card">
    <h3 class="card-title">gVisor Isolation</h3>
    <p class="card-desc">Hardware-virtualized sandboxing via gVisor with Kubernetes-native pod scheduling</p>
  </div>
  <div class="card">
    <h3 class="card-title">Rootfs Snapshots</h3>
    <p class="card-desc">Snapshot filesystem changes and restore instantly. Snapshots are stored in a shared bucket, accessible on demand from any node.</p>
  </div>
  <div class="card">
    <h3 class="card-title">Python SDK</h3>
    <p class="card-desc">Create sandboxes, run commands, and manage snapshots in a few lines of code</p>
  </div>
  <div class="card">
    <h3 class="card-title">Open Source</h3>
    <p class="card-desc">Apache 2.0 licensed, built on Kubernetes primitives, no vendor lock-in</p>
  </div>
</section>
```

Note: The `<main class="hero">` closing tag needs to move. Restructure so that `<main>` wraps the hero AND all content sections (features, code, how-it-works), since they are all the main content. Move the `</main>` to after the last content section (will be moved further down in later tasks). For now, place `</main>` after the features section.

Updated structure:

```html
<main>
  <section class="hero">
    <!-- existing hero content -->
  </section>

  <section class="features">
    <!-- feature cards -->
  </section>
</main>
```

This means changing `<main class="hero">` to `<main>` and wrapping the hero content in `<section class="hero">` instead. The `.hero` CSS class moves from `<main>` to a `<section>`.

- [ ] **Step 2: Add features CSS**

Append to `docs/styles.css`, before the `@media` rules:

```css
.features {
  border-top: 1px solid var(--line);
  display: grid;
  gap: 20px;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  padding-top: 80px;
  margin-top: 80px;
}

.card {
  backdrop-filter: blur(16px);
  background: rgba(14, 20, 37, 0.74);
  border: 1px solid var(--line);
  border-radius: 16px;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.08);
  padding: 24px;
}

.card-title {
  font-size: 1rem;
  font-weight: 700;
  letter-spacing: -0.04em;
  margin: 0;
}

.card-desc {
  color: var(--muted);
  font-size: 0.88rem;
  font-weight: 500;
  line-height: 1.7;
  margin: 10px 0 0;
}
```

- [ ] **Step 3: Add responsive styles for features**

In the `@media (max-width: 640px)` block, add:

```css
.features {
  margin-top: 48px;
  padding-top: 48px;
}
```

- [ ] **Step 4: Verify in browser**

Cards should display in a 4-column grid on wide screens, 2 columns around 980px, 1 column below 640px. Each card should have the glassmorphism background, subtle border, and inset glow.

- [ ] **Step 5: Commit**

```bash
git add docs/index.html docs/styles.css
git commit -m "add features section with glassmorphism cards"
```

---

### Task 3: Add Code Example Section

**Files:**
- Modify: `docs/index.html`
- Modify: `docs/styles.css`

- [ ] **Step 1: Add code example HTML**

In `docs/index.html`, after the `</section>` closing the `.features` section, add:

```html
<section class="code-example">
  <span class="code-label">Python</span>
  <pre class="code-block"><code><span class="kw">from</span> isola <span class="kw">import</span> Isola

client = Isola()

<span class="kw">with</span> client.sandboxes.create(image=<span class="str">"alpine:3.21"</span>) <span class="kw">as</span> sandbox:
    result = sandbox.commands.run(<span class="str">"echo"</span>, <span class="str">"hello world"</span>)
    <span class="kw">print</span>(result.stdout)  <span class="comment"># "hello world\n"</span>

    <span class="comment"># Snapshot the filesystem changes</span>
    client.rootfs_snapshots.create(
        sandbox_id=sandbox.id,
        snapshot_name=<span class="str">"my-snapshot"</span>,
    )

<span class="comment"># Restore from snapshot on any node</span>
<span class="kw">with</span> client.sandboxes.create(
    image=<span class="str">"alpine:3.21"</span>,
    rootfs_snapshot_source=<span class="str">"my-snapshot"</span>,
) <span class="kw">as</span> restored:
    restored.commands.run(<span class="str">"cat"</span>, <span class="str">"/data/results.json"</span>)</code></pre>
</section>
```

Note: The `<pre><code>` content must NOT be indented in the HTML source — whitespace inside `<pre>` is literal. The code starts immediately after `<code>` with no leading newline.

- [ ] **Step 2: Add code example CSS**

Append to `docs/styles.css`, before the `@media` rules:

```css
.code-example {
  margin-top: 80px;
}

.code-label {
  color: var(--muted-soft);
  display: block;
  font-size: 0.82rem;
  font-weight: 600;
  letter-spacing: -0.02em;
  margin-bottom: 12px;
}

.code-block {
  backdrop-filter: blur(16px);
  background: rgba(14, 20, 37, 0.74);
  border: 1px solid var(--line);
  border-radius: 16px;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.08);
  color: var(--text);
  font-family: ui-monospace, "SF Mono", "Cascadia Code", Consolas, monospace;
  font-size: 0.88rem;
  line-height: 1.7;
  margin: 0;
  overflow-x: auto;
  padding: 24px 28px;
}

.code-block .kw {
  color: #a87ee6;
}

.code-block .str {
  color: var(--cyan);
}

.code-block .comment {
  color: var(--muted-soft);
}
```

The keyword color `#a87ee6` is a lighter purple (derived from `--purple` but brighter) for better readability on the dark background.

- [ ] **Step 3: Add responsive styles for code example**

In the `@media (max-width: 640px)` block, add:

```css
.code-example {
  margin-top: 48px;
}

.code-block {
  border-radius: 12px;
  font-size: 0.82rem;
  padding: 18px 20px;
}
```

- [ ] **Step 4: Verify in browser**

The code block should show syntax-highlighted Python with purple keywords, cyan strings, and muted comments. It should scroll horizontally on narrow screens rather than wrapping.

- [ ] **Step 5: Commit**

```bash
git add docs/index.html docs/styles.css
git commit -m "add code example section with CSS syntax highlighting"
```

---

### Task 4: Add How It Works Section

**Files:**
- Modify: `docs/index.html`
- Modify: `docs/styles.css`

- [ ] **Step 1: Add how-it-works HTML**

In `docs/index.html`, after the `</section>` closing `.code-example`, add:

```html
<section class="steps">
  <div class="step">
    <span class="step-num" style="color: var(--purple)">01</span>
    <h3 class="step-title">Create</h3>
    <p class="step-desc">Spin up a gVisor-isolated sandbox from any container image</p>
  </div>
  <div class="step">
    <span class="step-num" style="color: var(--indigo)">02</span>
    <h3 class="step-title">Execute</h3>
    <p class="step-desc">Run commands, read and write files via the Python SDK</p>
  </div>
  <div class="step">
    <span class="step-num" style="color: var(--cyan)">03</span>
    <h3 class="step-title">Snapshot &amp; Restore</h3>
    <p class="step-desc">Capture rootfs changes, restore instantly on any node</p>
  </div>
</section>
```

Inline `style` attributes are used for the step numbers because each has a unique color from the gradient. This is cleaner than three one-off CSS classes.

- [ ] **Step 2: Add how-it-works CSS**

Append to `docs/styles.css`, before the `@media` rules:

```css
.steps {
  display: grid;
  gap: 0;
  grid-template-columns: repeat(3, 1fr);
  margin-top: 80px;
}

.step {
  border-right: 1px solid var(--line);
  padding: 0 28px;
}

.step:first-child {
  padding-left: 0;
}

.step:last-child {
  border-right: none;
  padding-right: 0;
}

.step-num {
  font-size: 1.6rem;
  font-weight: 700;
  letter-spacing: -0.04em;
}

.step-title {
  font-size: 1rem;
  font-weight: 700;
  letter-spacing: -0.04em;
  margin: 8px 0 0;
}

.step-desc {
  color: var(--muted);
  font-size: 0.88rem;
  font-weight: 500;
  line-height: 1.7;
  margin: 8px 0 0;
}
```

- [ ] **Step 3: Add responsive styles for steps**

In the `@media (max-width: 640px)` block, add:

```css
.steps {
  grid-template-columns: 1fr;
  margin-top: 48px;
}

.step {
  border-bottom: 1px solid var(--line);
  border-right: none;
  padding: 20px 0;
}

.step:first-child {
  padding-top: 0;
}

.step:last-child {
  border-bottom: none;
  padding-bottom: 0;
}
```

- [ ] **Step 4: Verify in browser**

Three steps in a row on desktop with vertical dividers between them. Numbers should be purple, indigo, and cyan respectively. On mobile, steps should stack vertically with horizontal dividers.

- [ ] **Step 5: Commit**

```bash
git add docs/index.html docs/styles.css
git commit -m "add how-it-works 3-step flow section"
```

---

### Task 5: Add Footer

**Files:**
- Modify: `docs/index.html`
- Modify: `docs/styles.css`

- [ ] **Step 1: Add footer HTML**

In `docs/index.html`, close `</main>` after the `.steps` section, then add a footer before the closing `</div>` of `.page`:

```html
    </main>

    <footer class="site-footer">
      <span class="footer-brand">Isola</span>
      <a class="footer-link" href="https://github.com/isola-run/isola">GitHub</a>
    </footer>
  </div>
```

- [ ] **Step 2: Add footer CSS**

Append to `docs/styles.css`, before the `@media` rules:

```css
.site-footer {
  align-items: center;
  border-top: 1px solid var(--line);
  color: var(--muted-soft);
  display: flex;
  font-size: 0.82rem;
  font-weight: 600;
  justify-content: space-between;
  margin-top: 80px;
  padding: 24px 0;
}

.footer-link {
  transition: color 160ms ease;
}

.footer-link:hover {
  color: var(--cyan);
}
```

- [ ] **Step 3: Add responsive styles for footer**

In the `@media (max-width: 640px)` block, add:

```css
.site-footer {
  margin-top: 48px;
}
```

- [ ] **Step 4: Verify in browser**

Footer should show "Isola" on the left and "GitHub" on the right, separated by a subtle top border. GitHub link should highlight cyan on hover.

- [ ] **Step 5: Commit**

```bash
git add docs/index.html docs/styles.css
git commit -m "add minimal footer"
```

---

### Task 6: Final Polish and Verify

**Files:**
- Modify: `docs/index.html` (if needed)
- Modify: `docs/styles.css` (if needed)

- [ ] **Step 1: Verify complete HTML structure**

The final `docs/index.html` structure should be:

```
<body>
  <div class="ambient ambient-one">
  <div class="ambient ambient-two">
  <div class="ambient-grid">
  <div class="page">
    <header class="site-header">
    <main>
      <section class="hero">
      <section class="features">
      <section class="code-example">
      <section class="steps">
    </main>
    <footer class="site-footer">
  </div>
</body>
```

Verify this structure is correct.

- [ ] **Step 2: Verify responsive behavior at all breakpoints**

Check the page at:
- Wide desktop (1400px+): Hero 2-column, 4 feature cards, 3 steps in a row
- Standard desktop (1200px): Same layout, within max-width
- Tablet (~800px): Hero single-column, 2 feature cards per row, 3 steps still in a row
- Mobile (~375px): Everything stacked, smaller text, tighter spacing

- [ ] **Step 3: Fix any issues found**

Address any visual or structural issues discovered during verification.

- [ ] **Step 4: Final commit (if changes were made)**

```bash
git add docs/index.html docs/styles.css
git commit -m "polish landing page layout and responsive behavior"
```
