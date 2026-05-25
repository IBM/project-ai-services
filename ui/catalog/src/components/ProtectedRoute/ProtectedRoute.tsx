import { Navigate, Outlet } from "react-router-dom";
import { ROUTES } from "@/constants";
import { useAuthStore } from "@/store/auth.store";

const ProtectedRoute = () => {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated());

  if (!isAuthenticated) {
    return <Navigate to={ROUTES.LOGIN} replace />;
  }

  return <Outlet />;
};

export default ProtectedRoute;
