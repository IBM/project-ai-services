import { BrowserRouter, Routes, Route, Navigate } from "react-router";
import { ThemeProvider } from "./contexts/ThemeProvider";
import MainLayout from "./layouts/MainLayout";
import JobMonitorPage from "./pages/JobMonitorPage";
import DocumentListPage from "./pages/DocumentListPage";
import AuthCallbackPage from "./pages/AuthCallback";

function App() {
  return (
    <ThemeProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Navigate to="/jobs" replace />} />

          <Route element={<MainLayout />}>
            <Route path="/jobs" element={<JobMonitorPage />} />
            <Route path="/documents" element={<DocumentListPage />} />
          </Route>

          <Route path="/auth/callback" element={<AuthCallbackPage />} />
        </Routes>
      </BrowserRouter>
    </ThemeProvider>
  );
}

export default App;

// Made with Bob
