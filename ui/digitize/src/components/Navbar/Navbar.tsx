import { Theme, SideNav, SideNavItems, SideNavLink } from '@carbon/react';
import { Activity, Document } from '@carbon/icons-react';
import { NavLink } from 'react-router-dom';
import { useRef, useEffect } from 'react';
import { useTheme } from '@contexts/useTheme';
import styles from './Navbar.module.scss';

interface NavbarProps {
  isSideNavOpen: boolean;
  setIsSideNavOpen: React.Dispatch<React.SetStateAction<boolean>>;
}

const Navbar = ({ isSideNavOpen, setIsSideNavOpen }: NavbarProps) => {
  const navRef = useRef<HTMLElement>(null);
  const { effectiveTheme } = useTheme();

  useEffect(() => {
    function handleOutsideClick(e: MouseEvent) {
      if (!isSideNavOpen || !setIsSideNavOpen) return;
      const target = e.target as Node;
      if (navRef.current && navRef.current.contains(target)) return;
      setIsSideNavOpen(false);
    }

    document.addEventListener('mousedown', handleOutsideClick);
    return () => document.removeEventListener('mousedown', handleOutsideClick);
  }, [isSideNavOpen, setIsSideNavOpen]);

  return (
    <Theme theme={effectiveTheme}>
      <SideNav
        aria-label="Side navigation"
        expanded={isSideNavOpen}
        isFixedNav
        isChildOfHeader={false}
        ref={navRef}
      >
        <SideNavItems>
          <SideNavLink
            renderIcon={Activity}
            as={NavLink}
            to="/jobs"
            className={styles.sideNavItem}
          >
            Job Monitor
          </SideNavLink>

          <SideNavLink
            renderIcon={Document}
            as={NavLink}
            to="/documents"
            className={styles.sideNavItem}
          >
            Documents
          </SideNavLink>
        </SideNavItems>
      </SideNav>
    </Theme>
  );
};

export default Navbar;

// Made with Bob