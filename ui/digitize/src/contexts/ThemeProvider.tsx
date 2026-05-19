import { useState, useEffect, useMemo, ReactNode } from 'react';
import { ThemeContext } from './ThemeContext';
import type { Theme, EffectiveTheme, ThemeContextType } from './ThemeContext';

interface ThemeProviderProps {
  children: ReactNode;
}

export const ThemeProvider = ({ children }: ThemeProviderProps) => {
  const [theme, setTheme] = useState<Theme>(() => {
    // Check localStorage first, default to 'system'
    const savedTheme = localStorage.getItem('app-theme') as Theme | null;
    return savedTheme || 'system';
  });

  const [systemTheme, setSystemTheme] = useState<EffectiveTheme>(() => {
    // Initialize with current system preference
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    return prefersDark ? 'g100' : 'white';
  });

  // Derive effective theme from theme and systemTheme
  const effectiveTheme = useMemo<EffectiveTheme>(() => {
    if (theme === 'system') {
      return systemTheme;
    } else if (theme === 'dark') {
      return 'g100';
    } else if (theme === 'light') {
      return 'white';
    }
    return theme as EffectiveTheme;
  }, [theme, systemTheme]);

  // Save theme preference and apply to DOM
  useEffect(() => {
    localStorage.setItem('app-theme', theme);
    document.documentElement.setAttribute('data-carbon-theme', effectiveTheme);
  }, [theme, effectiveTheme]);

  // Listen for system theme changes when in system mode
  useEffect(() => {
    if (theme !== 'system') return;

    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    const handleChange = (e: MediaQueryListEvent) => {
      setSystemTheme(e.matches ? 'g100' : 'white');
    };

    mediaQuery.addEventListener('change', handleChange);
    return () => mediaQuery.removeEventListener('change', handleChange);
  }, [theme]);

  const value: ThemeContextType = {
    theme,
    setTheme,
    effectiveTheme,
  };

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
};

// Made with Bob
