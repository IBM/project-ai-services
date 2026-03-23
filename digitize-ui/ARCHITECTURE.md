# Architecture Overview

## System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Browser (Client)                         │
│  ┌───────────────────────────────────────────────────────┐  │
│  │              React Application (Port 3000)             │  │
│  │  ┌─────────────────────────────────────────────────┐  │  │
│  │  │  IBM Carbon Design System Components            │  │  │
│  │  │  - DocumentUpload                                │  │  │
│  │  │  - JobMonitor                                    │  │  │
│  │  │  - DocumentList                                  │  │  │
│  │  └─────────────────────────────────────────────────┘  │  │
│  │  ┌─────────────────────────────────────────────────┐  │  │
│  │  │  API Service Layer (axios)                      │  │  │
│  │  └─────────────────────────────────────────────────┘  │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                            │
                            │ HTTP/REST API
                            │ (Proxied via Vite)
                            ▼
┌─────────────────────────────────────────────────────────────┐
│              Digitize Service Backend (Port 4000)            │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  FastAPI Application (spyre-rag/src/digitize/app.py) │  │
│  │  ┌─────────────────────────────────────────────────┐ │  │
│  │  │  Endpoints:                                      │ │  │
│  │  │  - POST /v1/documents (upload)                   │ │  │
│  │  │  - GET /v1/documents/jobs (list jobs)            │ │  │
│  │  │  - GET /v1/documents/jobs/{id} (job details)     │ │  │
│  │  │  - GET /v1/documents (list documents)            │ │  │
│  │  │  - GET /v1/documents/{id} (document metadata)    │ │  │
│  │  │  - GET /v1/documents/{id}/content (content)      │ │  │
│  │  │  - DELETE /v1/documents/{id} (delete)            │ │  │
│  │  └─────────────────────────────────────────────────┘ │  │
│  └───────────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  Document Processing Pipeline                         │  │
│  │  - Docling (PDF conversion)                           │  │
│  │  - LLM (table summarization)                          │  │
│  │  - Embedding (vectorization)                          │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                    Storage Layer                             │
│  ┌──────────────────┐  ┌──────────────────┐                 │
│  │  OpenSearch      │  │  File System     │                 │
│  │  (Vector DB)     │  │  (Cache)         │                 │
│  └──────────────────┘  └──────────────────┘                 │
└─────────────────────────────────────────────────────────────┘
```

## Component Architecture

### Frontend Components

#### 1. **App.jsx**
- Root component
- Manages routing with React Router
- Applies IBM Carbon theme (g100)
- Renders header and content area

#### 2. **HomePage.jsx**
- Main page component
- Contains tabbed interface
- Manages state for refresh triggers
- Coordinates between child components

#### 3. **DocumentUpload.jsx**
- File upload interface
- Operation type selection (ingestion/digitization)
- Output format selection (for digitization)
- File validation and upload handling
- Success/error notifications

#### 4. **JobMonitor.jsx**
- Displays job list in a data table
- Pagination support
- Job status visualization with tags
- Refresh functionality
- Job details modal

#### 5. **DocumentList.jsx**
- Document listing with search
- Pagination support
- Document content viewer (modal)
- Delete confirmation
- Status indicators

### API Service Layer

**src/services/api.js**
- Centralized API client using axios
- Base URL configuration
- Request/response interceptors
- Error handling
- Typed API methods for all endpoints

## Data Flow

### Document Upload Flow

```
User selects file(s)
    ↓
DocumentUpload validates input
    ↓
API call: POST /v1/documents
    ↓
Backend creates job
    ↓
Returns job_id
    ↓
UI shows success notification
    ↓
Triggers refresh of JobMonitor
```

### Job Monitoring Flow

```
Component mounts / Refresh clicked
    ↓
API call: GET /v1/documents/jobs
    ↓
Backend returns job list
    ↓
UI renders jobs in table
    ↓
User clicks "View Details"
    ↓
API call: GET /v1/documents/jobs/{id}
    ↓
Display job details in modal
```

### Document Management Flow

```
Component mounts / Search / Pagination
    ↓
API call: GET /v1/documents
    ↓
Backend returns document list
    ↓
UI renders documents in table
    ↓
User clicks view/delete
    ↓
API call: GET /v1/documents/{id}/content
         or DELETE /v1/documents/{id}
    ↓
Display content or refresh list
```

## State Management

### Local Component State
- Each component manages its own state using React hooks
- `useState` for local data (files, loading, errors)
- `useEffect` for side effects (API calls, subscriptions)

### Shared State
- Refresh triggers passed from HomePage to child components
- Upload success callback propagates state changes

### Future Considerations
- Consider Redux/Context API for complex state
- Add WebSocket for real-time job updates
- Implement optimistic UI updates

## Styling Architecture

### IBM Carbon Design System
- Uses Carbon's theming system
- G100 (dark) theme applied globally
- Carbon components provide consistent styling

### Custom Styles (App.scss)
- SCSS for preprocessing
- Imports Carbon's SCSS modules
- Custom utility classes
- Responsive design breakpoints

### Component Styles
- Inline styles for dynamic values
- Carbon utility classes for spacing/layout
- Scoped styles when needed

## API Integration

### Proxy Configuration
- Vite dev server proxies `/v1` to backend
- Avoids CORS issues in development
- Production requires proper CORS setup

### Error Handling
- Try-catch blocks in all API calls
- User-friendly error messages
- HTTP status code handling
- Network error detection

### Request/Response Format

**Upload Request:**
```javascript
FormData with:
- files: File[]
- operation: 'ingestion' | 'digitization'
- output_format: 'json' | 'md' | 'text'
```

**Standard Response:**
```javascript
{
  data: [...],
  pagination: {
    total: number,
    limit: number,
    offset: number
  }
}
```

## Security Considerations

### Current Implementation
- No authentication (development only)
- Client-side validation
- HTTPS recommended for production

### Production Recommendations
- Add JWT/OAuth authentication
- Implement RBAC (Role-Based Access Control)
- Add CSRF protection
- Sanitize file uploads
- Rate limiting
- Input validation on backend

## Performance Optimization

### Current Optimizations
- Pagination for large lists
- Lazy loading of document content
- Debounced search
- Efficient re-renders with React keys

### Future Optimizations
- Virtual scrolling for large tables
- Image lazy loading
- Code splitting
- Service worker for offline support
- Caching strategies

## Deployment Architecture

### Development
```
npm run dev → Vite Dev Server (Port 3000)
              ↓ Proxy
              Backend (Port 4000)
```

### Production
```
npm run build → Static files in dist/
                ↓
                Web Server (Nginx/Apache)
                ↓ Reverse Proxy
                Backend API
```

## Technology Stack

### Frontend
- **React 18**: UI library
- **IBM Carbon Design System**: Component library
- **React Router**: Client-side routing
- **Axios**: HTTP client
- **Vite**: Build tool
- **Sass**: CSS preprocessing

### Backend (Existing)
- **FastAPI**: Python web framework
- **Docling**: Document processing
- **OpenSearch**: Vector database
- **vLLM**: LLM serving

## Extension Points

### Adding New Features
1. Create component in `src/components/`
2. Add API method in `src/services/api.js`
3. Add route in `src/App.jsx` if needed
4. Update navigation/tabs

### Customization
- Theme: Modify `src/App.scss`
- Components: Extend Carbon components
- API: Add interceptors in `api.js`
- Layout: Modify `App.jsx` structure

## Testing Strategy

### Unit Tests (Recommended)
- Component rendering
- User interactions
- API service methods
- Utility functions

### Integration Tests (Recommended)
- Component interactions
- API integration
- Routing
- Form submissions

### E2E Tests (Recommended)
- Complete user flows
- Upload → Monitor → View workflow
- Error scenarios
- Cross-browser testing

## Monitoring & Logging

### Current Implementation
- Console logging for errors
- Browser DevTools for debugging

### Production Recommendations
- Error tracking (Sentry, LogRocket)
- Analytics (Google Analytics, Mixpanel)
- Performance monitoring (Web Vitals)
- User session recording