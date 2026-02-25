import { Theme, SideNav, SideNavItems, SideNavMenuItem } from "@carbon/react";
import { NavLink, useLocation } from "react-router-dom";
import type { RefObject } from "react";
import styles from "./Navbar.module.scss";

type NavbarProps = {
  isSideNavOpen: boolean;
  sideNavRef?: RefObject<HTMLElement>;
};

const Navbar = (props: NavbarProps) => {
  const { isSideNavOpen, sideNavRef } = props;
  const location = useLocation();

  return (
    <Theme theme="g100">
      <SideNav
        aria-label="Side navigation"
        expanded={isSideNavOpen}
        isPersistent={false}
        ref={sideNavRef}
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
