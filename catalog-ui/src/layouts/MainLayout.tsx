import { Outlet } from "react-router-dom";
import { useState } from "react";
import AppHeader from "@/components/AppHeader";
import Navbar from "@/components/Navbar";
import "../index.scss";

const MainLayout = () => {
  const [isSideNavOpen, setIsSideNavOpen] = useState(false);

  return (
    <>
      <AppHeader
        isSideNavOpen={isSideNavOpen}
        setIsSideNavOpen={setIsSideNavOpen}
      />

      <Navbar
        isSideNavOpen={isSideNavOpen}
        setIsSideNavOpen={setIsSideNavOpen}
      />

      <main className="appContent">
        <Outlet />
      </main>
    </>
  );
};

export default MainLayout;
