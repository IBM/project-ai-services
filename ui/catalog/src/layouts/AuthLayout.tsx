import { Outlet } from "react-router";
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
