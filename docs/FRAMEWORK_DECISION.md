# Documentation Framework Decision

## 1. Recommendation

**MkDocs with Material for MkDocs theme.** Single tool covers both the landing page and full documentation site with the strongest OpenAPI integration story of any Markdown-first SSG.

## 2. Why this wins (project-specific)

- **OpenAPI integration is plug-and-play.** The project already auto-generates `api/openapi/api-gateway.yaml` (1,200 lines). The `mkdocs-render-swagger` or `mkdocs-redoc` plugin renders it inline with one Markdown directive — no manual API reference maintenance. This is the decisive differentiator: VitePress and Docusaurus have no equivalent plugin; you'd embed Swagger UI manually.
- **Dual-audience navigation is native.** Material's tabs + sections cleanly separate "SDK Users" from "Operators" from "Contributors" without custom layouts or manual sidebar configuration.
- **Markdown-only authoring.** The existing `sdks/python/README.md` and `CLAUDE.md` content can be moved into the docs site with zero rewriting. No MDX, no JSX, no React components to maintain.
- **Documentation-specific extensions are built in.** Admonitions, tabbed code blocks (show Go and Python side-by-side), copy buttons, code annotations — all included without plugin assembly.
- **Landing page without a separate site.** Material supports a custom `home.html` override with hero sections, feature grids, and CTAs — sufficient for an infrastructure project. No need for a separate marketing site at this stage.
- **Search works out of the box.** Built-in lunr.js search indexes all content at build time. No external service needed.

> **Note on Node.js:** Adding Node.js to CI is trivial (`actions/setup-node`), and tools like VitePress have negligible operational burden. The choice of MkDocs is not driven by avoiding Node.js — it's driven by OpenAPI plugin maturity and documentation-specific feature density. If the OpenAPI spec were maintained manually or didn't exist, VitePress would be equally strong.

## 3. Alternatives considered

| Option | Why not chosen |
|--------|---------------|
| **VitePress** | Closest competitor. Better landing page defaults, faster builds, clean aesthetics. However, no mature OpenAPI rendering plugin — you'd manually embed Swagger UI or maintain a custom Vue component. For a project that auto-generates a 1,200-line OpenAPI spec, this gap matters. If OpenAPI inline rendering weren't a requirement, VitePress would be a strong pick. |
| **Docusaurus** | MDX/React flexibility is unnecessary — Isola's docs are technical reference + guides, not interactive demos. Heavier framework for what it provides. Versioning support is stronger, but Isola is pre-1.0 (v0.1.0) and doesn't need multi-version docs yet. |
| **Sphinx + Furo** | Better for auto-generating Python API docs from docstrings, but the SDK's public surface is small (8 classes). Manual Markdown reference pages are more readable and maintainable at this scale. RST-first ecosystem adds friction for contributors writing guides. |
| **Hugo** | Documentation-specific features (tabs, admonitions, copy buttons, search) require theme hunting and plugin assembly. Hugo's Go templates are powerful but arcane for doc contributors. |

## 4. Key tradeoffs

**Optimizes for:**
- Inline OpenAPI rendering from the already-generated spec (the killer feature)
- Documentation-specific Markdown extensions (admonitions, tabs, code annotations) without assembly
- Immediate productivity (content structure > framework configuration)

**Sacrifices vs closest alternative (VitePress):**
- Landing page is "good enough" not "polished" — Material's `home.html` override is functional but less elegant than VitePress's built-in hero/features layout
- Slower builds (Python vs Vite) — negligible at expected doc size, but VitePress is objectively faster
- Single-maintainer ecosystem risk (see Hidden risks) vs Vue team backing
- Versioning requires `mike` plugin rather than being built-in

## 5. When this choice would be wrong

- **Interactive API playground needed.** If users expect a "Try it" button that hits a live sandbox API from the docs, Docusaurus + a custom React component would be better.
- **Heavy versioning requirements.** If Isola ships multiple supported major versions simultaneously (e.g., v1 and v2 with different APIs), Docusaurus handles this more natively. Unlikely before v1.0.
- **Marketing-driven landing page.** If the landing page needs animations, complex layouts, or A/B testing, decouple it as a separate Next.js/Astro site and keep MkDocs for docs only.
- **Auto-generated Python API reference from docstrings.** If the SDK grows to 50+ public classes and you want sphinx-style autodoc, Sphinx would be more appropriate. At 8 classes, manual reference pages are better.

## 6. Minimal implementation plan

### Project structure

```
docs/
├── mkdocs.yml              # Site configuration
├── overrides/
│   └── home.html           # Landing page template override
├── docs/
│   ├── index.md            # Landing page content
│   ├── getting-started/
│   │   ├── quickstart.md
│   │   └── installation.md
│   ├── sdk/
│   │   ├── overview.md
│   │   ├── sandboxes.md
│   │   ├── commands.md
│   │   ├── filesystem.md
│   │   └── errors.md
│   ├── api/
│   │   └── reference.md    # Renders OpenAPI spec inline
│   ├── operator/
│   │   ├── installation.md # Helm deployment guide
│   │   ├── configuration.md
│   │   ├── crds.md
│   │   └── networking.md
│   └── contributing/
│       ├── development.md  # Adapted from CLAUDE.md
│       └── architecture.md
├── requirements.txt        # MkDocs + plugins pinned
└── Makefile                # (or targets in root Makefile)
```

### Authoring format

Pure Markdown with Material extensions (admonitions, tabs, code annotations). No MDX.

### Dependencies (`requirements.txt`)

```
mkdocs
mkdocs-material
mkdocs-render-swagger      # OpenAPI spec rendering
```

### Build commands (add to root Makefile)

```makefile
.PHONY: docs docs-serve docs-build

docs-serve:                 ## Serve docs locally with hot reload
	cd docs && uv run --with mkdocs --with mkdocs-material --with mkdocs-render-swagger mkdocs serve

docs-build:                 ## Build static docs site
	cd docs && uv run --with mkdocs --with mkdocs-material --with mkdocs-render-swagger mkdocs build
```

### Deployment

**GitHub Pages via GitHub Actions** (simplest, free, already on GitHub):

```yaml
# .github/workflows/docs.yml
name: docs
on:
  push:
    branches: [main]
    paths: [docs/**]
jobs:
  deploy:
    runs-on: ubuntu-latest
    permissions:
      pages: write
      id-token: write
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-python@v5
      - run: pip install mkdocs mkdocs-material mkdocs-render-swagger
      - run: cd docs && mkdocs build
      - uses: actions/upload-pages-artifact@v3
        with:
          path: docs/site
      - uses: actions/deploy-pages@v4
```

Alternative: Cloudflare Pages (faster CDN, preview deploys on PRs) — same build command, point build output to `docs/site`.

### Minimal `mkdocs.yml`

```yaml
site_name: Isola
site_description: Secure sandbox orchestration for Kubernetes
repo_url: https://github.com/isola-ai/isola

theme:
  name: material
  custom_dir: overrides
  features:
    - navigation.tabs
    - navigation.sections
    - content.code.copy
    - search.suggest

nav:
  - Home: index.md
  - Getting Started:
    - Quickstart: getting-started/quickstart.md
    - Installation: getting-started/installation.md
  - Python SDK:
    - Overview: sdk/overview.md
    - Sandboxes: sdk/sandboxes.md
    - Commands: sdk/commands.md
    - Filesystem: sdk/filesystem.md
    - Errors: sdk/errors.md
  - API Reference:
    - REST API: api/reference.md
  - Operator Guide:
    - Installation: operator/installation.md
    - Configuration: operator/configuration.md
    - CRDs: operator/crds.md
    - Networking: operator/networking.md
  - Contributing:
    - Development: contributing/development.md
    - Architecture: contributing/architecture.md

markdown_extensions:
  - admonition
  - pymdownx.details
  - pymdownx.superfences
  - pymdownx.tabbed:
      alternate_style: true
  - pymdownx.highlight:
      anchor_linenums: true

plugins:
  - search
  - render_swagger
```

## 7. Hidden risks

- **Material theme is maintained by one person (Martin Donath).** The "Insiders" sponsorware model means some features are paywalled. The free tier is fully sufficient for Isola's needs, but if you later want features like social cards or privacy plugin, you'd need a $15/mo sponsorship. Risk: if the maintainer steps back, the community fork path is unclear.
- **OpenAPI plugin ecosystem is fragile.** `mkdocs-render-swagger` and alternatives (`mkdocs-redoc`) are community-maintained with irregular updates. If the plugin breaks on a MkDocs upgrade, you may need to pin versions or switch plugins. Mitigation: the OpenAPI spec is only used on one page, so worst case you link to a standalone Swagger UI.
- **`mike` for versioning adds complexity.** Unlike Docusaurus where versioning is built-in, MkDocs versioning via `mike` requires a specific git-branch-based workflow. Don't add it until you actually ship v1.0 and need to maintain v0.x docs simultaneously.
- **No MDX means no embedded interactive components.** If you ever want a "create sandbox" button that live-demos the API from the docs, you'd need to bolt on custom JavaScript or switch frameworks.
- **Build speed is fine now, will stay fine.** MkDocs builds are fast (<5s for hundreds of pages). This is not a real risk — listing it to preempt the concern.
