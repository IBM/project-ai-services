import { createContext } from 'react';

export type Theme = 'system' | 'light' | 'dark';
export type EffectiveTheme = 'white' | 'g100';

export interface ThemeContextType {
  theme: Theme;
  setTheme: (theme: Theme) => void;
  effectiveTheme: EffectiveTheme;
}

export const ThemeContext = createContext<ThemeContextType | undefined>(undefined);

// Made with Bob
