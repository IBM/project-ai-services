# Quick Start Guide

## Prerequisites

1. **Node.js and npm**: Ensure you have Node.js 18+ installed
   ```bash
   node --version  # Should be 18.x or higher
   npm --version
   ```

2. **Backend Service**: The Digitize Service backend must be running
   ```bash
   # From the spyre-rag directory
   cd spyre-rag/src
   python -m digitize.app
   ```
   The backend should be running on `http://localhost:4000`

## Installation & Setup

1. **Navigate to the UI directory**:
   ```bash
   cd digitize-ui
   ```

2. **Install dependencies**:
   ```bash
   npm install
   ```

3. **Start the development server**:
   ```bash
   npm run dev
   ```

4. **Open your browser**:
   Navigate to `http://localhost:3000`

## First Steps

### 1. Upload a Document

1. Click on the **"Upload Documents"** tab
2. Select operation type:
   - **Ingestion**: For processing and storing in vector database
   - **Digitization**: For converting to text/markdown/JSON
3. Click "Select files" and choose a PDF document
4. Click "Upload"
5. You'll receive a Job ID upon successful upload

### 2. Monitor Processing Jobs

1. Click on the **"Job Monitor"** tab
2. View the status of your upload job
3. Click "View Details" to see more information
4. Use "Refresh" to update the job list

### 3. View Processed Documents

1. Click on the **"Documents"** tab
2. Browse through processed documents
3. Use the search bar to find specific documents
4. Click the eye icon to view document content
5. Click the trash icon to delete documents

## Configuration

### Backend URL

If your backend is running on a different host/port, update `vite.config.js`:

```javascript
export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    proxy: {
      '/v1': {
        target: 'http://your-backend-host:port',  // Change this
        changeOrigin: true,
      }
    }
  }
})
```

## Troubleshooting

### "Failed to fetch" errors

- Ensure the backend service is running on port 4000
- Check browser console for CORS errors
- Verify the proxy configuration in `vite.config.js`

### Upload fails with "Too many concurrent requests"

- The backend has concurrency limits (2 for digitization, 1 for ingestion)
- Wait for current jobs to complete before uploading more

### No documents showing up

- Check if the backend API is returning data
- Open browser DevTools > Network tab to inspect API responses
- Verify the backend database is properly configured

## Production Deployment

1. **Build the application**:
   ```bash
   npm run build
   ```

2. **Preview the build**:
   ```bash
   npm run preview
   ```

3. **Deploy the `dist` folder** to your web server or hosting platform

## Environment Variables

Create a `.env` file in the root directory for custom configuration:

```env
VITE_API_BASE_URL=http://localhost:4000
```

Then update `src/services/api.js`:

```javascript
const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || '/v1';
```

## Support

For issues or questions:
- Check the main README.md for detailed documentation
- Review the backend API documentation
- Check the browser console for error messages
- Verify all prerequisites are met

## Next Steps

- Explore the IBM Carbon Design System documentation
- Customize the UI theme in `src/App.scss`
- Add additional features or components as needed
- Integrate with authentication if required