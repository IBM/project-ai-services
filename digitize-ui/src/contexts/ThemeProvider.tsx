import { useState, useEffect, ReactNode } from 'react';
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

  const [effectiveTheme, setEffectiveTheme] = useState<EffectiveTheme>('white');

  useEffect(() => {
    // Save theme preference to localStorage
    localStorage.setItem('app-theme', theme);

    // Determine the effective theme
    let newEffectiveTheme: EffectiveTheme = theme as EffectiveTheme;
    
    if (theme === 'system') {
      // Check system preference
      const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
      newEffectiveTheme = prefersDark ? 'g100' : 'white';
    } else if (theme === 'dark') {
      newEffectiveTheme = 'g100';
    } else if (theme === 'light') {
      newEffectiveTheme = 'white';
    }

    setEffectiveTheme(newEffectiveTheme);

    // Apply theme to document root
    document.documentElement.setAttribute('data-carbon-theme', newEffectiveTheme);
  }, [theme]);

  // Listen for system theme changes when in system mode
  useEffect(() => {
    if (theme !== 'system') return;

    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    const handleChange = (e: MediaQueryListEvent) => {
      const newEffectiveTheme: EffectiveTheme = e.matches ? 'g100' : 'white';
      setEffectiveTheme(newEffectiveTheme);
      document.documentElement.setAttribute('data-carbon-theme', newEffectiveTheme);
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