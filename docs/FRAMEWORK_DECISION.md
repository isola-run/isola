# Documentation Framework Decision

## 1. Recommendation

**VitePress.** Markdown-first static site generator with built-in landing page support, versioning, and search — backed by the Vue team. Covers docs + landing page in a single build with minimal configuration.

## 2. Why this wins (project-specific)

- **Landing page is first-class.** VitePress's default theme includes hero sections, feature grids, and CTAs as declarative frontmatter — no template hacks. Sufficient for an infrastructure product landing page without a separate site.
- **Documentation breadth is the primary need.** Isola's docs span SDK guides, architecture, CRD reference, operator guides, and API reference. The OpenAPI spec is one section among many, not the primary consumption path. VitePress handles broad Markdown documentation well without requiring plugins for the core use case.
- **Versioning is built-in.** When Isola reaches v1.0 and needs to maintain multiple version docs, VitePress handles this natively — no `mike` plugin with its git-branch workflow.
- **Vue team maintenance.** Actively maintained by the Vue.js core team. Lower bus-factor risk than single-maintainer projects.
- **Fast builds.** Vite-powered — sub-second hot reload during authoring, fast production builds.
- **Markdown-only authoring.** Existing `sdks/python/README.md` and `CLAUDE.md` content ports directly. Vue components are available if needed but not required.
- **Search works out of the box.** Built-in local search via MiniSearch. No external service needed.

## 3. Alternatives considered

| Option | Why not chosen |
|--------|---------------|
| **MkDocs Material** | Stronger OpenAPI inline rendering via plugins, but OpenAPI is not Isola's primary documentation surface. Landing page requires Jinja2 template overrides (a hack). Single-maintainer ecosystem risk. Versioning requires `mike` plugin. Would win if inline OpenAPI rendering were the dominant use case. |
| **Docusaurus** | MDX/React flexibility is unnecessary — Isola's docs are technical reference + guides, not interactive demos. Heavier framework (React SSR) for what it provides. Better versioning than MkDocs but comparable to VitePress. |
| **Sphinx + Furo** | Better for auto-generating Python API docs from docstrings, but the SDK's public surface is small (8 classes). Manual Markdown reference pages are more readable at this scale. RST-first ecosystem adds friction. |

## 4. Key tradeoffs

**Optimizes for:**
- Landing page + docs in one cohesive site without hacks
- Low maintenance burden with team-backed ecosystem
- Built-in versioning for future multi-version docs
- Fast authoring feedback loop (Vite hot reload)

**Sacrifices vs MkDocs Material:**
- No drop-in OpenAPI rendering plugin — the REST API reference page requires either linking to a standalone Swagger UI or embedding it via iframe/script tag
- Fewer built-in documentation extensions — admonitions and code groups are supported natively, but Material has more (tabs, annotations, copy buttons with highlighting). VitePress covers the common cases; Material covers edge cases too.

**Sacrifices vs Docusaurus:**
- No MDX — cannot embed React components inline. Vue components are possible but the ecosystem of ready-made doc components is smaller.

## 5. When this choice would be wrong

- **OpenAPI is the primary docs surface.** If most users consume Isola via raw REST API (not the SDK) and need an interactive API explorer in the docs, MkDocs Material's plugin ecosystem would be better.
- **Auto-generated Python API reference.** If the SDK grows to 50+ public classes and you need sphinx-style autodoc from docstrings, Sphinx would be more appropriate. At 8 classes, manual pages are better.
- **Interactive playground.** If users expect a "Try it" button that hits a live sandbox API, Docusaurus with custom React components would be more natural.

## 6. Minimal implementation plan

### Project structure

```
docs/
├── .vitepress/
│   └── config.ts           # Site configuration
├── index.md                # Landing page (hero + features via frontmatter)
├── getting-started/
│   ├── quickstart.md
│   └── installation.md
├── sdk/
│   ├── overview.md
│   ├── sandboxes.md
│   ├── commands.md
│   ├── filesystem.md
│   └── errors.md
├── api/
│   └── reference.md        # REST API docs (link to or embed Swagger UI)
├── operator/
│   ├── installation.md     # Helm deployment guide
│   ├── configuration.md
│   ├── crds.md
│   └── networking.md
├── contributing/
│   ├── development.md      # Adapted from CLAUDE.md
│   └── architecture.md
└── package.json
```

### Authoring format

Markdown with VitePress extensions (custom containers for tips/warnings/danger, code groups). Vue components available if needed but not required for standard docs.

### Dependencies (`package.json`)

```json
{
  "devDependencies": {
    "vitepress": "^1"
  },
  "scripts": {
    "docs:dev": "vitepress dev",
    "docs:build": "vitepress build",
    "docs:preview": "vitepress preview"
  }
}
```

### Build commands (add to root Makefile)

```makefile
.PHONY: docs-serve docs-build

docs-serve:                 ## Serve docs locally with hot reload
	cd docs && npx vitepress dev

docs-build:                 ## Build static docs site
	cd docs && npx vitepress build
```

### Minimal `.vitepress/config.ts`

```ts
import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Isola',
  description: 'Secure sandbox orchestration for Kubernetes',

  themeConfig: {
    nav: [
      { text: 'Guide', link: '/getting-started/quickstart' },
      { text: 'SDK', link: '/sdk/overview' },
      { text: 'API', link: '/api/reference' },
      { text: 'Operator', link: '/operator/installation' },
    ],

    sidebar: {
      '/getting-started/': [
        { text: 'Quickstart', link: '/getting-started/quickstart' },
        { text: 'Installation', link: '/getting-started/installation' },
      ],
      '/sdk/': [
        { text: 'Overview', link: '/sdk/overview' },
        { text: 'Sandboxes', link: '/sdk/sandboxes' },
        { text: 'Commands', link: '/sdk/commands' },
        { text: 'Filesystem', link: '/sdk/filesystem' },
        { text: 'Errors', link: '/sdk/errors' },
      ],
      '/api/': [
        { text: 'REST API', link: '/api/reference' },
      ],
      '/operator/': [
        { text: 'Installation', link: '/operator/installation' },
        { text: 'Configuration', link: '/operator/configuration' },
        { text: 'CRDs', link: '/operator/crds' },
        { text: 'Networking', link: '/operator/networking' },
      ],
      '/contributing/': [
        { text: 'Development', link: '/contributing/development' },
        { text: 'Architecture', link: '/contributing/architecture' },
      ],
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/isola-ai/isola' },
    ],

    search: {
      provider: 'local',
    },
  },
})
```

### Landing page (`index.md`)

```markdown
---
layout: home
hero:
  name: Isola
  text: Secure sandbox orchestration for Kubernetes
  tagline: Create, manage, and snapshot isolated environments with a Python SDK and REST API
  actions:
    - theme: brand
      text: Get Started
      link: /getting-started/quickstart
    - theme: alt
      text: View on GitHub
      link: https://github.com/isola-ai/isola
features:
  - title: Python SDK
    details: Sync and async clients with streaming command output, file I/O, and automatic retry logic.
  - title: Secure by Default
    details: gVisor runtime isolation with deny-all networking. Opt-in egress rules per sandbox.
  - title: Kubernetes Native
    details: CRD-based lifecycle management with Helm deployment. Fits into existing K8s infrastructure.
---
```

### Deployment

**GitHub Pages via GitHub Actions:**

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
      - uses: actions/setup-node@v4
      - run: cd docs && npm install && npm run docs:build
      - uses: actions/upload-pages-artifact@v3
        with:
          path: docs/.vitepress/dist
      - uses: actions/deploy-pages@v4
```

Alternative: Cloudflare Pages (faster CDN, preview deploys on PRs) — point build output to `docs/.vitepress/dist`.

### OpenAPI / Swagger UI

For the REST API reference page, embed Swagger UI via a script tag or link to a standalone hosted instance. Example in `api/reference.md`:

```html
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist/swagger-ui-bundle.js"></script>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist/swagger-ui.css" />
<script>
SwaggerUIBundle({ url: '/api-gateway.yaml', dom_id: '#swagger-ui' })
</script>
```

Copy the generated `api/openapi/api-gateway.yaml` into `docs/public/` during build so it's served as a static asset.

## 7. Hidden risks

- **Vue component lock-in.** If you start using Vue components in Markdown for custom layouts, migration to a non-Vue framework later requires rewriting those components. Stick to standard Markdown for portability.
- **Sidebar configuration is manual.** Unlike MkDocs which can auto-generate nav from directory structure, VitePress requires explicit sidebar config in `config.ts`. This is fine for ~20 pages but becomes tedious at 100+. Community plugins exist but aren't official.
- **Swagger UI embed is a workaround.** Loading Swagger UI via CDN script tag works but looks different from the rest of the site. If polished API docs matter, consider hosting a standalone Swagger UI or Redoc page separately.
- **Node.js version management.** The project currently has no `package.json` or `.nvmrc`. Adding one for docs is trivial but it's a new dependency to pin and maintain. Use the LTS version and pin in CI.
- **VitePress major versions.** VitePress reached 1.0 in early 2024 and is stable, but Vue ecosystem tools do occasionally have breaking major releases. Pin the major version in `package.json`.
