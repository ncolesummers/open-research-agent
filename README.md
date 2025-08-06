# Open Research Agent

A Go-based research assistant application that provides intelligent research capabilities.

## Quick Start

### Run the simple version
```bash
go run main.go
```
Visit http://localhost:8080

### Run the structured version
```bash
go run cmd/agent/main.go
```
Visit http://localhost:8080 for the web interface

## Project Structure

```
.
├── cmd/agent/      # Application entry points
├── internal/       # Private application code
│   └── research/   # Research logic
├── pkg/           # Public libraries
│   └── utils/     # Utility functions
└── main.go        # Simple entry point
```

## Development

Build the application:
```bash
make build
```

Run tests:
```bash
make test
```

## API Endpoints

- `/` - Hello World homepage
- `/health` - Health check endpoint
- `/api/research` - Research API (to be implemented)