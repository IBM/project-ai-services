import {
  Theme,
  SideNav,
  SideNavItems,
  SideNavMenuItem,
  SideNavDivider,
} from "@carbon/react";
import { NavLink } from "react-router-dom";
import { useRef, useEffect } from "react";
import type { Dispatch, SetStateAction } from "react";

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
    <Theme theme="g90">
      <SideNav
        aria-label="Side navigation"
        expanded={isSideNavOpen}
        isPersistent={false}
        ref={navRef}
      >
        <SideNavItems>
          <SideNavMenuItem as={NavLink} to="/digital-assistants">
            Digital assistants
          </SideNavMenuItem>

          <SideNavMenuItem as={NavLink} to="/architectures">
            Architectures
          </SideNavMenuItem>

          <SideNavMenuItem as={NavLink} to="/services">
            Services
          </SideNavMenuItem>

          <SideNavDivider />

          <SideNavMenuItem as={NavLink} to="/use-case-references">
            Use case references
          </SideNavMenuItem>
        </SideNavItems>
      </SideNav>
    </Theme>
  );
};

export default Navbar;
