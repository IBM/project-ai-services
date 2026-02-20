import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { Theme } from "@carbon/react";
import { ROUTES } from "@/constants";

import Login from "./pages/Login";
import Logout from "./pages/Logout";

function App() {
  return (
    <Theme theme="white">
      <BrowserRouter>
        <Routes>
          <Route path={ROUTES.LOGIN} element={<Login />} />
          <Route path={ROUTES.LOGOUT} element={<Logout />} />
          <Route path="*" element={<Navigate to={ROUTES.LOGIN} replace />} />
        </Routes>
      </BrowserRouter>
    </Theme>
  );
}

export default App;
