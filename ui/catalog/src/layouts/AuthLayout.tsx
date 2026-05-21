import { Outlet } from "react-router-dom";
import AppHeader from "@/components/AppHeader";

const AuthLayout = () => {
  return (
    <>
      <AppHeader minimal />
      <main>
        <Outlet />
      </main>
    </>
  );
};

export default AuthLayout;
