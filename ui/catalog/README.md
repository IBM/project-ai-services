# catalog-ui

Frontend application built with **React 19**, **Vite 8**, **TypeScript**, and **IBM Carbon Design System**.

---

## Tech Stack

- React 19
- Vite 8
- TypeScript (strict mode)
- IBM Carbon (`@carbon/react` + `@carbon/icons-react` + `@carbon/ibm-products`)
- React Router v8
- ESLint (flat config)
- Prettier
- Sass (`sass`)

---

## Getting Started

### Install dependencies

```bash
npm install
```

### Start development server

```bash
npm run dev
```

Application runs at:

```
http://localhost:5173
```

---

## Available Scripts

### Development

```bash
npm run dev
```

Starts Vite dev server with HMR.

---

### Build

```bash
npm run build
```

Runs TypeScript type-check and builds the production bundle.

---

### Preview Production Build

```bash
npm run preview
```

Serves the built production files locally.

---

## Code Quality

### Lint

```bash
npm run lint
```

Runs ESLint.

### Auto-fix Lint Issues

```bash
npm run lint:fix
```

---

### Format Code

```bash
npm run format
```

Formats files using Prettier.

---

### Type Check

```bash
npm run typecheck
```

Runs TypeScript validation without emitting files.

---

### Full Validation (Recommended Before Push)

```bash
npm run check
```

Runs:

- ESLint
- Prettier format check
- TypeScript type-check

---

### Auto Fix (Lint + Format)

```bash
npm run fix
```

---

## Project Structure

```
src/
├── components/        # Reusable UI components
│   └── ComponentName/
│       ├── ComponentName.tsx
│       ├── ComponentName.module.scss
│       └── index.ts
├── pages/             # Route-level pages
│   └── PageName/
│       ├── PageName.tsx
│       ├── PageName.module.scss
│       └── index.ts
├── constants/         # Application constants (routes, API, env, etc.)
├── App.tsx            # Application routes
├── main.tsx           # Entry point
└── index.scss         # Global styles
```

## Environment Variables

Environment variables must be prefixed with:

```
VITE_
```

## Known Dependency Notes

### `react-table` override in `package.json`

`@carbon/ibm-products` depends internally on `react-table@7.8.0`, which is the last release of the v7 line and does not declare React 19 peer support. The `overrides` block forces React 19 for `react-table` to silence the `ERESOLVE` warning during `npm install`.

`react-table` v7 is unmaintained — in v8 it was moved to `@tanstack/react-table`, which Carbon has not yet adopted. **Remove this override once `@carbon/ibm-products` migrates to `@tanstack/react-table`.**

### `minimatch` override in `package.json`

`eslint` and `typescript-eslint` (via `@typescript-eslint/typescript-estree`) depend on `minimatch@10`, which is affected by a denial-of-service advisory ([GHSA-g3hv-75wg-m249](https://github.com/advisories/GHSA-g3hv-75wg-m249)) fixed in `11.0.1`. The `overrides` block forces `minimatch@^11.0.4` so `npm audit` stays clean. `minimatch@11` requires Node `20 || >=22`.

ESLint 10 does not fix this on its own, because it still depends on `minimatch@10`, and `eslint-plugin-react` does not yet support ESLint 10. **Remove this override once `eslint` and `typescript-eslint` depend on `minimatch@>=11.0.1`.**

---
