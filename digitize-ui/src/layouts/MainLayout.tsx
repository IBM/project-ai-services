import { Outlet } from 'react-router-dom';
import { useState } from 'react';
import AppHeader from '../components/AppHeader';
import Navbar from '../components/Navbar';
import '../index.scss';

const MainLayout = () => {
  const [isSideNavOpen, setIsSideNavOpen] = useState(false);

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

      <main>
        <Outlet />
      </main>
    </div>
  );
};

export default MainLayout;

// Made with Bob