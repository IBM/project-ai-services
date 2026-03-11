# Digitize Service UI

A modern React application built with IBM Carbon Design System to provide a user interface for the Digitize Service from spyre-rag.

## Features

- **Document Upload**: Upload PDF documents for ingestion or digitization
- **Job Monitoring**: Track the status of document processing jobs
- **Document Management**: View, search, and manage processed documents
- **IBM Carbon Design**: Professional UI with IBM's design system

## Prerequisites

- Node.js 18+ and npm
- Running instance of the Digitize Service backend (spyre-rag/src/digitize/app.py) on port 4000

## Installation

1. Navigate to the digitize-ui directory:
```bash
cd digitize-ui
```

2. Install dependencies:
```bash
npm install
```

## Configuration

The application uses environment variables for configuration.

### Quick Setup

1. Copy the example environment file:
```bash
cp .env.example .env.local
```

2. Edit `.env.local` to configure your API target:
```env
VITE_API_TARGET=http://localhost:4000
VITE_PORT=3000
```

### Available Configuration Options

- **VITE_API_TARGET**: Backend API URL (default: `http://localhost:4000`)
- **VITE_PORT**: Development server port (default: `3000`)

For detailed configuration instructions, see [.env.README.md](.env.README.md).

### Examples

**Local backend (default):**
```env
VITE_API_TARGET=http://localhost:4000
```

**Remote development server:**
```env
VITE_API_TARGET=http://dev-server.example.com:4000
```

**Note**: Changes to environment variables require restarting the dev server.

## Running the Application

### Development Mode

Start the development server:
```bash
npm run dev
```

The application will be available at `http://localhost:3000`

### Production Build

Build the application for production:
```bash
npm run build
```

Preview the production build:
```bash
npm run preview
```

## Usage

### Upload Documents

1. Navigate to the "Upload Documents" tab
2. Select operation type:
   - **Ingestion**: Process and store documents in vector database
   - **Digitization**: Convert documents to text/markdown/JSON format
3. For digitization, select output format (JSON, Markdown, or Text)
4. Select PDF file(s) to upload
5. Click "Upload" to start processing

### Monitor Jobs

1. Navigate to the "Job Monitor" tab
2. View all processing jobs with their status
3. Click "View Details" to see job information
4. Use "Refresh" to update the job list

### Manage Documents

1. Navigate to the "Documents" tab
2. Search for documents by name
3. View document content by clicking the view icon
4. Delete documents by clicking the trash icon
5. Use pagination to browse through documents

## API Endpoints

The UI communicates with the following backend endpoints:

- `POST /v1/documents` - Upload documents
- `GET /v1/documents/jobs` - List all jobs
- `GET /v1/documents/jobs/{job_id}` - Get job details
- `GET /v1/documents` - List documents
- `GET /v1/documents/{doc_id}` - Get document metadata
- `GET /v1/documents/{doc_id}/content` - Get document content
- `DELETE /v1/documents/{doc_id}` - Delete document
- `DELETE /v1/documents?confirm=true` - Bulk delete documents

## Technology Stack

- **React 18**: Modern React with hooks
- **IBM Carbon Design System**: Professional UI components
- **Vite**: Fast build tool and dev server
- **React Router**: Client-side routing
- **Axios**: HTTP client for API calls
- **Sass**: CSS preprocessing

## Project Structure

```
digitize-ui/
├── src/
│   ├── components/          # Reusable UI components
│   │   ├── DocumentUpload.jsx
│   │   ├── JobMonitor.jsx
│   │   └── DocumentList.jsx
│   ├── pages/              # Page components
│   │   └── HomePage.jsx
│   ├── services/           # API service layer
│   │   └── api.js
│   ├── App.jsx            # Main application component
│   ├── App.scss           # Global styles
│   └── main.jsx           # Application entry point
├── index.html             # HTML template
├── vite.config.js         # Vite configuration
└── package.json           # Project dependencies
```

## Development

### Adding New Features

1. Create new components in `src/components/`
2. Add API methods in `src/services/api.js`
3. Update routes in `src/App.jsx` if needed
4. Follow IBM Carbon Design System guidelines

### Styling

The application uses IBM Carbon Design System's theming. Global styles are in `src/App.scss`. Component-specific styles should use Carbon's utility classes or be scoped to the component.

## Troubleshooting

### Backend Connection Issues

If you see connection errors:
1. Ensure the backend service is running on port 4000
2. Check the proxy configuration in `vite.config.js`
3. Verify CORS settings on the backend

### Build Issues

If you encounter build errors:
1. Delete `node_modules` and `package-lock.json`
2. Run `npm install` again
3. Clear Vite cache: `rm -rf node_modules/.vite`

## License

This project is part of the IBM Project AI Services.