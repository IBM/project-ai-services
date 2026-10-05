import { BrowserRouter, Routes, Route, Navigate } from 'react-router';
import { ThemeProvider } from '@contexts/ThemeProvider';
import MainLayout from './layouts/MainLayout';
import SubmitPage from './pages/SubmitPage';
import JobsPage from './pages/JobsPage';
import ReviewPage from './pages/ReviewPage';
import JobDetailPage from './pages/JobDetailPage';

function App() {
  return (
    <ThemeProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Navigate to="/jobs" replace />} />
          <Route element={<MainLayout />}>
            <Route path="/jobs" element={<JobsPage />} />
            <Route path="/jobs/:jobId" element={<JobDetailPage />} />
            <Route path="/submit" element={<SubmitPage />} />
            <Route path="/review/:jobId" element={<ReviewPage />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </ThemeProvider>
  );
}

export default App;
