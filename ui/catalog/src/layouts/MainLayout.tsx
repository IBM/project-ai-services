import { Outlet } from "react-router";
import { useState } from "react";
import AppHeader from "@/components/AppHeader";
import Navbar from "@/components/Navbar";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";

const MainLayout = () => {
  const [isSideNavOpen, setIsSideNavOpen] = useState(false);
  useDocumentTitle();

  return (
    <div className="appLayout">
      <AppHeader
        isSideNavOpen={isSideNavOpen}
        setIsSideNavOpen={setIsSideNavOpen}
      />

      <Navbar
        isSideNavOpen={isSideNavOpen}
        setIsSideNavOpen={setIsSideNavOpen}
      />

      <main className={isSideNavOpen ? "mainContent shifted" : "mainContent"}>
        <Outlet />
      </main>
    </div>
  );
};

export default MainLayout;
