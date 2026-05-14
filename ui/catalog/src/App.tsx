import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { ROUTES } from "@/constants";
import MainLayout from "./layouts/MainLayout";
import AuthLayout from "./layouts/AuthLayout";

import Login from "./pages/Login";
import Logout from "./pages/Logout";
import ApplicationsListPage from "./pages/AiDeployments";
import Services from "./pages/Services";
import UseCaseReferences from "./pages/UseCaseReferences";
import { ProtectedRoute } from "@/components";

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route
          path="/"
          element={<Navigate to={ROUTES.AI_DEPLOYMENTS} replace />}
        />

        {/* Protected routes */}
        <Route element={<ProtectedRoute />}>
          <Route element={<MainLayout />}>
            <Route
              path={ROUTES.AI_DEPLOYMENTS}
              element={<ApplicationsListPage />}
            />
            <Route path={ROUTES.SERVICES} element={<Services />} />
            <Route
              path={ROUTES.USE_CASE_REFERENCES}
              element={<UseCaseReferences />}
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
