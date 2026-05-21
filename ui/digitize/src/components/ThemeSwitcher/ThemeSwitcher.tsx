import { useState, useRef, useEffect } from 'react';
import { HeaderGlobalAction } from '@carbon/react';
import { Asleep, Light, Laptop } from '@carbon/icons-react';
import type { CarbonIconType } from '@carbon/icons-react';
import { useTheme } from '@contexts/useTheme';
import styles from './ThemeSwitcher.module.scss';

type ThemeValue = 'system' | 'light' | 'dark';

interface ThemeOption {
  value: ThemeValue;
  label: string;
  icon: CarbonIconType;
}

const ThemeSwitcher = () => {
  const { theme, setTheme } = useTheme();
  const [isOpen, setIsOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  const themeOptions: ThemeOption[] = [
    { value: 'system', label: 'System', icon: Laptop },
    { value: 'light', label: 'Light', icon: Light },
    { value: 'dark', label: 'Dark', icon: Asleep },
  ];

  const currentThemeOption = themeOptions.find(opt => opt.value === theme);
  const CurrentIcon = currentThemeOption?.icon || Laptop;

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };

    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside);
    }

    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, [isOpen]);

  const handleThemeChange = (newTheme: ThemeValue) => {
    setTheme(newTheme);
    setIsOpen(false);
  };

  return (
    <div className={styles.themeSwitcher} ref={dropdownRef}>
      <HeaderGlobalAction
        aria-label="Theme settings"
        aria-expanded={isOpen}
        onClick={() => setIsOpen(!isOpen)}
        className={styles.themeButton}
      >
        <CurrentIcon size={20} />
      </HeaderGlobalAction>

      {isOpen && (
        <div className={styles.dropdown}>
          <div className={styles.dropdownHeader}>Theme</div>
          <ul className={styles.dropdownList}>
            {themeOptions.map((option) => {
              const OptionIcon = option.icon;
              return (
                <li key={option.value}>
                  <button
                    className={`${styles.dropdownItem} ${
                      theme === option.value ? styles.active : ''
                    }`}
                    onClick={() => handleThemeChange(option.value)}
                  >
                    <OptionIcon size={16} className={styles.optionIcon} />
                    <span>{option.label}</span>
                    {theme === option.value && (
                      <span className={styles.checkmark}>✓</span>
                    )}
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </div>
  );
};

export default ThemeSwitcher;

// Made with Bob