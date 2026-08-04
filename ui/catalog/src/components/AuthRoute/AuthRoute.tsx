import { Navigate, Outlet } from "react-router-dom";
import { ROUTES } from "@/constants";
import { useAuthStore } from "@/store/auth.store";

interface AuthRouteProps {
  requireAuth?: boolean;
}

const AuthRoute = ({ requireAuth = true }: AuthRouteProps) => {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated());

  // Protected routes - require authentication
  if (requireAuth && !isAuthenticated) {
    return <Navigate to={ROUTES.LOGIN} replace />;
  }

  // Public routes - redirect if already authenticated
  if (!requireAuth && isAuthenticated) {
    return <Navigate to={ROUTES.DIGITAL_ASSISTANTS} replace />;
  }

  return <Outlet />;
};

export default AuthRoute;
