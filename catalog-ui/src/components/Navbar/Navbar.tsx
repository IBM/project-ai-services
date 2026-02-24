import { Theme, SideNav, SideNavItems, SideNavMenuItem } from "@carbon/react";
import { NavLink, useLocation } from "react-router-dom";

interface Props {
  isSideNavOpen: boolean;
}

const Navbar = ({ isSideNavOpen }: Props) => {
  const location = useLocation();

  return (
    <Theme theme="g100">
      <SideNav
        aria-label="Side navigation"
        expanded={isSideNavOpen}
        isPersistent={false}
      >
        <SideNavItems>
          <SideNavMenuItem
            element={NavLink}
            to="/applications"
            isActive={location.pathname === "/applications"}
          >
            Applications
          </SideNavMenuItem>

          <SideNavMenuItem
            element={NavLink}
            to="/technical"
            isActive={location.pathname === "/technical"}
          >
            Technical templates
          </SideNavMenuItem>

          <SideNavMenuItem
            element={NavLink}
            to="/business"
            isActive={location.pathname === "/business"}
          >
            Business demo templates
          </SideNavMenuItem>

          <SideNavMenuItem
            element={NavLink}
            to="/services"
            isActive={location.pathname === "/services"}
          >
            Services catalog
          </SideNavMenuItem>
        </SideNavItems>
      </SideNav>
    </Theme>
  );
};

export default Navbar;
