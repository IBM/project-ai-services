import { Theme, SideNav, SideNavItems, SideNavMenuItem } from "@carbon/react";
import { NavLink } from "react-router-dom";
import { useRef, useEffect } from "react";
import type { Dispatch, SetStateAction } from "react";
import styles from "./Navbar.module.scss";

type NavbarProps = {
  isSideNavOpen: boolean;
  setIsSideNavOpen?: Dispatch<SetStateAction<boolean>>;
};

const Navbar = (props: NavbarProps) => {
  const { isSideNavOpen, setIsSideNavOpen } = props;
  const navRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    function handleOutsideClick(e: MouseEvent) {
      if (!isSideNavOpen || !setIsSideNavOpen) return;
      const target = e.target as Node;
      if (navRef.current && navRef.current.contains(target)) return;
      setIsSideNavOpen(false);
    }

    document.addEventListener("mousedown", handleOutsideClick);
    return () => document.removeEventListener("mousedown", handleOutsideClick);
  }, [isSideNavOpen, setIsSideNavOpen]);

  return (
    <Theme theme="g100">
      <SideNav
        aria-label="Side navigation"
        expanded={isSideNavOpen}
        isPersistent={false}
        ref={navRef}
      >
        <SideNavItems>
          <NavLink to="/applications" className={styles.navLink}>
            <SideNavMenuItem className={styles.sideNavItem}>
              Applications
            </SideNavMenuItem>
          </NavLink>
          <NavLink to="/technical-templates" className={styles.navLink}>
            <SideNavMenuItem className={styles.sideNavItem}>
              Technical templates
            </SideNavMenuItem>
          </NavLink>
          <NavLink to="/business-demo-templates" className={styles.navLink}>
            <SideNavMenuItem className={styles.sideNavItem}>
              Business demo templates
            </SideNavMenuItem>
          </NavLink>
          <NavLink to="/services-catalog" className={styles.navLink}>
            <SideNavMenuItem className={styles.sideNavItem}>
              Services catalog
            </SideNavMenuItem>
          </NavLink>
        </SideNavItems>
      </SideNav>
    </Theme>
  );
};

export default Navbar;
