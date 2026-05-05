import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { ROUTES } from "@/constants";
import MainLayout from "./layouts/MainLayout";
import AuthLayout from "./layouts/AuthLayout";

import Login from "./pages/Login";
import Logout from "./pages/Logout";
import ApplicationsListPage from "./pages/AiDeployments";
import Architectures from "./pages/Architectures";
import Services from "./pages/Services";
import SolutionsAndUseCases from "./pages/SolutionsAndUseCases";
import { ProtectedRoute } from "@/components";

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Navigate to={ROUTES.LOGIN} replace />} />

        {/* Protected routes */}
        <Route element={<ProtectedRoute />}>
          <Route element={<MainLayout />}>
            <Route
              path={ROUTES.AI_DEPLOYMENTS}
              element={<ApplicationsListPage />}
            />
            <Route path={ROUTES.ARCHITECTURES} element={<Architectures />} />
            <Route path={ROUTES.SERVICES} element={<Services />} />
            <Route
              path={ROUTES.SOLUTIONS_AND_USE_CASES}
              element={<SolutionsAndUseCases />}
            />
          </Route>
        </Route>

        {/* Public routes */}
        <Route element={<AuthLayout />}>
          <Route path={ROUTES.LOGIN} element={<Login />} />
        </Route>

        <Route path={ROUTES.LOGOUT} element={<Logout />} />
      </Routes>
    </BrowserRouter>
  );
}

export default App;
