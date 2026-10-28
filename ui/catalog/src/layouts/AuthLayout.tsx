import { Outlet } from "react-router";
import AppHeader from "@/components/AppHeader";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";

const AuthLayout = () => {
  useDocumentTitle();
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
