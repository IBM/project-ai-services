# catalog-ui

Frontend application built with **React 19**, **Vite 7**, **TypeScript**, and **IBM Carbon Design System**.

---

## Tech Stack

- React 19
- Vite 7
- TypeScript (strict mode)
- IBM Carbon (`@carbon/react` + `@carbon/icons-react`)
- React Router v7
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

---
