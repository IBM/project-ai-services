import {
  Theme,
  SideNav,
  SideNavItems,
  SideNavMenuItem,
  SideNavDivider,
  Tag,
} from "@carbon/react";
import { NavLink } from "react-router";
import { useRef, useEffect, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { ROUTES } from "@/constants";
import { fetchDataSourceConnectors } from "@/api/connectors.api";
import styles from "./Navbar.module.scss";

type NavbarProps = {
  isSideNavOpen: boolean;
  setIsSideNavOpen?: Dispatch<SetStateAction<boolean>>;
};

const Navbar = (props: NavbarProps) => {
  const { isSideNavOpen, setIsSideNavOpen } = props;
  const navRef = useRef<HTMLElement | null>(null);
  const [offlineCount, setOfflineCount] = useState<number | null>(null);

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

  useEffect(() => {
    if (!isSideNavOpen) {
      return;
    }

    let isMounted = true;
    const loadOfflineCount = async () => {
      try {
        const response = await fetchDataSourceConnectors(1, 1, "offline");
        if (isMounted) {
          setOfflineCount(response.pagination?.total_items ?? 0);
        }
      } catch (error) {
        console.error("Failed to fetch offline connectors count", error);
        if (isMounted) {
          setOfflineCount(null);
        }
      }
    };

    loadOfflineCount();

    const intervalId = setInterval(loadOfflineCount, 120000);

    return () => {
      isMounted = false;
      clearInterval(intervalId);
    };
  }, [isSideNavOpen]);

  return (
    <Theme theme="g90">
      <SideNav
        aria-label="Side navigation"
        expanded={isSideNavOpen}
        isPersistent={false}
        ref={navRef}
      >
        <SideNavItems>
          <SideNavMenuItem as={NavLink} to={ROUTES.DIGITAL_ASSISTANTS}>
            Digital Assistants
          </SideNavMenuItem>

          <SideNavMenuItem as={NavLink} to={ROUTES.SERVICES}>
            Services
          </SideNavMenuItem>

          <SideNavMenuItem as={NavLink} to={ROUTES.CONNECTORS}>
            <div className={styles.navItemContent}>
              <span>Connectors</span>
              {offlineCount !== null && offlineCount > 0 && (
                <Tag type="red" size="sm" className={styles.badge}>
                  {offlineCount}
                </Tag>
              )}
            </div>
          </SideNavMenuItem>

          <SideNavMenuItem as={NavLink} to={ROUTES.WORKER_RESOURCES}>
            Worker Resources
          </SideNavMenuItem>

          <SideNavDivider />

          <SideNavMenuItem as={NavLink} to={ROUTES.USE_CASE_REFERENCES}>
            Use case references
          </SideNavMenuItem>
        </SideNavItems>
      </SideNav>
    </Theme>
  );
};

export default Navbar;
