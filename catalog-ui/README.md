# catalog-ui

Frontend application built with **React 19**, **Vite 7**, **TypeScript**, and **IBM Carbon Design System**.

---

## Tech Stack

- React 19
- Vite 7
- TypeScript (strict mode)
- IBM Carbon (`@carbon/react` + `@carbon/styles`)
- React Router v7
- ESLint (flat config)
- Prettier
- Sass (`sass`)
- Yarn (v1.22.22)

---

## Getting Started

### Install dependencies

```bash
yarn install
```

### Start development server

```bash
yarn dev
```

Application runs at:

```
http://localhost:5173
```

---

## Available Scripts

### Development

```bash
yarn dev
```

Starts Vite dev server with HMR.

---

### Build

```bash
yarn build
```

Runs TypeScript type-check and builds the production bundle.

---

### Preview Production Build

```bash
yarn preview
```

Serves the built production files locally.

---

## Code Quality

### Lint

```bash
yarn lint
```

Runs ESLint.

### Auto-fix Lint Issues

```bash
yarn lint:fix
```

---

### Format Code

```bash
yarn format
```

Formats files using Prettier.

---

### Type Check

```bash
yarn typecheck
```

Runs TypeScript validation without emitting files.

---

### Full Validation (Recommended Before Push)

```bash
yarn check
```

Runs:

- ESLint
- Prettier format check
- TypeScript type-check

---

### Auto Fix (Lint + Format)

```bash
yarn fix
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