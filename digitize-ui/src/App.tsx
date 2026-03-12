import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { ThemeProvider } from './contexts/ThemeProvider';
import MainLayout from './layouts/MainLayout';
import JobMonitorPage from './pages/JobMonitorPage';
import DocumentListPage from './pages/DocumentListPage';

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
        </Routes>
      </BrowserRouter>
    </ThemeProvider>
  );
}

export default App;

// Made with Bob