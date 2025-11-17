import eslint from '@eslint/js';
import reactPlugin from 'eslint-plugin-react';
import reactHooksPlugin from 'eslint-plugin-react-hooks';
import globals from 'globals';

export default [
  // Global ignores
  {
    ignores: ['dist/', 'build/', 'node_modules/', 'coverage/'],
  },

  // Apply to JS, JSX, MJS files
  {
    files: ['**/*.js', '**/*.jsx', '**/*.mjs'],
    ...eslint.configs.recommended, // Basic ESLint recommended rules
    ...reactPlugin.configs.flat.recommended, // Recommended React rules
    ...reactPlugin.configs.flat['jsx-runtime'], // Rules for new JSX transform (React 17+)
    plugins: {
      react: reactPlugin,
      'react-hooks': reactHooksPlugin,
    },
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: {
        ...globals.browser,
        ...globals.node,
      },
      parserOptions: {
        ecmaFeatures: {
          jsx: true,
        },
      },
    },
    settings: {
      react: {
        version: 'detect', // Automatically detect the React version
      },
    },
    rules: {
      // Add or override specific rules
      ...reactPlugin.configs.recommended.rules, // Recommended rules for React
      ...reactHooksPlugin.configs.recommended.rules, // Recommended rules for React Hooks

      // TODO: Enable below later
      // 'semi': ['error', 'always'],
      // 'quotes': ['error', 'single'],
      'no-unused-vars': 'warn',

      // disable
      'react/prop-types': 'off', // Required if we want to check prop type (Typecript)
      'react/react-in-jsx-scope': 'off', // Disabling as this expects React import in all jsx files
    },
  },
];
