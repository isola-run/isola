# Isola Frontend

A modern, professional web interface for managing Isola sandbox environments.

## Features

- **Dashboard**: Overview of sandbox statistics and quick actions
- **Sandbox Management**: Create, list, and manage sandbox environments
- **Terminal**: Execute commands in running sandboxes
- **File Upload**: Upload files to sandboxes with drag-and-drop support
- **Settings**: Configure API key and theme preferences
- **Dark/Light Mode**: Automatic theme detection with manual override

## Tech Stack

- **React 18** with TypeScript
- **Vite** for fast development and building
- **Tailwind CSS** for styling
- **TanStack Query** for data fetching and caching
- **React Router** for navigation
- **Lucide React** for icons

## Development

### Prerequisites

- Node.js 18+
- npm or yarn

### Setup

```bash
# Install dependencies
npm install

# Start development server
npm run dev
```

The development server runs at `http://localhost:3000` and proxies API requests to `http://localhost:8080` (isola-gw).

### Building

```bash
# Build for production
npm run build

# Preview production build
npm run preview
```

## Docker

Build and run the frontend container:

```bash
# Build image
docker build -t isola-frontend .

# Run container
docker run -p 80:80 isola-frontend
```

The container includes nginx configured to:
- Serve the SPA correctly (all routes fallback to index.html)
- Proxy `/api/*` requests to the gateway at `isola-gw:8080`
- Enable gzip compression
- Cache static assets

## Configuration

### API Key

Set your API key in Settings or use one of the demo keys:
- `iso_sk_demo`
- `iso_sk_a1b2c3d4e5f67890a1b2c3d4e5f67890`

### Environment Variables

The frontend is a static SPA and doesn't require environment variables at runtime.
API configuration is done via the Settings page.

## Project Structure

```
frontend/
├── src/
│   ├── components/
│   │   ├── dashboard/      # Dashboard view
│   │   ├── layout/         # Header, Sidebar, Layout
│   │   ├── sandbox/        # Sandbox components
│   │   └── ui/             # Reusable UI components
│   ├── hooks/              # React hooks
│   ├── lib/                # Utilities
│   ├── services/           # API client
│   ├── types/              # TypeScript types
│   ├── App.tsx             # Main app component
│   └── main.tsx            # Entry point
├── public/                 # Static assets
├── index.html              # HTML template
└── package.json
```

## API Integration

The frontend communicates with isola-gw via REST API:

- `GET /api/v1/sandboxes` - List sandboxes
- `POST /api/v1/sandboxes` - Create sandbox
- `GET /api/v1/sandboxes/:id` - Get sandbox details
- `DELETE /api/v1/sandboxes/:id` - Terminate sandbox
- `POST /api/v1/sandboxes/:id/execute` - Execute command
- `POST /api/v1/sandboxes/:id/files` - Upload file

All API requests require the `X-API-Key` header for authentication.
