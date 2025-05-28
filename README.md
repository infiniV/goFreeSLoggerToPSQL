# GoFreeSWITCH ESL Integration

A Go application for integrating with FreeSWITCH via the Event Socket Library (ESL), persisting call events to PostgreSQL, and exposing a RESTful API for querying call data.

## Table of Contents

- [Project Structure](#project-structure)
- [Features](#features)
- [Requirements](#requirements)
- [Setup](#setup)
- [Configuration](#configuration)
- [Running the Application](#running-the-application)
- [API Endpoints](#api-endpoints)
- [Technical Overview](#technical-overview)
- [Logging](#logging)
- [Database Schema](#database-schema)
- [License](#license)

---

## Project Structure

```
.
├── main.go               # Application entry point
├── go.mod, go.sum        # Go modules and dependencies
├── .env                  # Environment variables (not for production)
├── api/
│   └── server.go         # REST API server (Gin)
├── config/
│   └── config.go         # Configuration loader
├── esl/
│   └── esl_client.go     # FreeSWITCH ESL client logic
├── store/
│   └── store.go          # PostgreSQL data access layer
└── utils/
    └── logger.go         # Logrus logger setup
```

## Features

- Connects to FreeSWITCH via ESL (Event Socket Library)
- Listens for call events (CHANNEL_CREATE, CHANNEL_HANGUP)
- Persists call data to PostgreSQL
- Exposes RESTful API to query call records
- Graceful shutdown and robust reconnection logic
- Structured JSON logging (Logrus)

## Requirements

- Go 1.24+
- PostgreSQL database
- FreeSWITCH server with ESL enabled

## Setup

1. **Clone the repository:**
   ```sh
   git clone <your-repo-url>
   cd gofreeswitchesl
   ```
2. **Install dependencies:**
   ```sh
   go mod tidy
   ```
3. **Configure environment:**
   - Copy `.env` and adjust as needed:
     ```env
     ESL_ADDR=127.0.0.1:8021
     ESL_PASS=ClueCon
     DATABASE_URL=postgresql://user:password@host:port/dbname
     API_PORT=8080
     ```

## Configuration

- Configuration is loaded from environment variables (see `.env`).
- Sensitive data (passwords, DSNs) should not be committed to version control.

## Running the Application

```sh
go run main.go
```

- The application will:
  - Connect to PostgreSQL and initialize the schema (creates `calls` table if missing)
  - Connect to FreeSWITCH ESL and subscribe to events
  - Start the REST API server (default: `http://localhost:8080`)

## API Endpoints

- **Health Check:**
  - `GET /health` → `{ "status": "UP" }`
  - **Sample:**
    ```sh
    curl http://localhost:8080/health
    ```

- **List Calls:**
  - `GET /api/v1/calls?limit=10&offset=0`
  - Returns a paginated list of call records
  - **Sample:**
    ```sh
    curl "http://localhost:8080/api/v1/calls?limit=10&offset=0"
    ```

- **Get Call by UUID:**
  - `GET /api/v1/calls/{uuid}`
  - Returns a single call record by its unique ID
  - **Sample:**
    ```sh
    curl http://localhost:8080/api/v1/calls/<uuid>
    ```

### Example Call Record

```json
{
  "id": 1,
  "uuid": "...",
  "direction": "inbound",
  "caller": "+1234567890",
  "callee": "+0987654321",
  "start_time": "2024-06-01T12:00:00Z",
  "end_time": "2024-06-01T12:05:00Z",
  "status": "NORMAL_CLEARING",
  "created_at": "2024-06-01T12:00:00Z"
}
```

## Technical Overview (Detailed Per File)

### main.go
The entry point of the application. It:
- Initializes the logger for structured output.
- Loads configuration from environment variables or `.env`.
- Connects to PostgreSQL using the provided DSN and initializes the schema (auto-creates the `calls` table if missing).
- Instantiates the data store, ESL client, and API server.
- Starts the API server in a goroutine, listening for HTTP requests.
- Starts the ESL client, which connects to FreeSWITCH and listens for call events.
- Handles graceful shutdown on SIGINT/SIGTERM, ensuring all resources (HTTP server, ESL client, DB pool) are properly closed.

### api/server.go
Defines the REST API using the Gin framework. Key features:
- Sets up middleware for structured logging and panic recovery.
- Exposes endpoints:
  - `GET /health`: Health check.
  - `GET /api/v1/calls`: List calls with pagination (`limit`, `offset`).
  - `GET /api/v1/calls/:uuid`: Retrieve a call by its UUID.
- Validates and parses query parameters, returning appropriate HTTP status codes and error messages.
- Uses the store to fetch call data from the database.

### config/config.go
Handles application configuration. Responsibilities:
- Loads environment variables, optionally from a `.env` file (using `godotenv`).
- Provides default values for all config keys if not set in the environment.
- Exposes a `Config` struct with fields for ESL address, ESL password, database URL, and API port.
- Includes helper methods for type conversion (e.g., port as integer).

### esl/esl_client.go
Implements the FreeSWITCH ESL (Event Socket Library) client. Features:
- Manages a persistent connection to FreeSWITCH ESL, with automatic reconnection logic.
- Subscribes to all ESL events (in JSON format).
- Listens for and processes events in a background goroutine.
- Handles `CHANNEL_CREATE` and `CHANNEL_HANGUP` events:
  - On `CHANNEL_CREATE`, parses event data and creates a new call record in the database.
  - On `CHANNEL_HANGUP`, updates the corresponding call record with hangup time and status.
- Uses structured logging for all connection, event, and error states.
- Provides a `Close()` method for graceful shutdown.

### store/store.go
Implements the data access layer for PostgreSQL. Responsibilities:
- Defines the `Call` struct, representing a call record (with fields for UUID, direction, caller, callee, start/end time, status, etc.).
- Provides methods:
  - `CreateCall`: Inserts a new call record.
  - `UpdateCallHangup`: Updates a call record with hangup info.
  - `GetCalls`: Retrieves a paginated list of calls.
  - `GetCallByUUID`: Retrieves a call by its UUID.
  - `InitSchema`: Creates the `calls` table if it does not exist (idempotent, for development/testing; use migrations for production).
- Uses context timeouts for all DB operations to avoid hanging.
- Logs all DB actions and errors with context.

### utils/logger.go
Sets up the Logrus logger for the application. Features:
- Configures Logrus to output JSON-formatted logs with ISO8601 timestamps.
- Outputs logs to stdout.
- Sets the log level to `Info` by default (can be changed for debugging).
- Returns a configured logger instance for use throughout the app.

## Logging

- Uses Logrus for structured, JSON-formatted logs.
- Logs include context (event, UUID, errors, etc.) for traceability.

## Database Schema

The application will auto-create the following table if it does not exist:

```sql
CREATE TABLE IF NOT EXISTS calls (
    id         SERIAL PRIMARY KEY,
    uuid       TEXT UNIQUE NOT NULL,
    direction  TEXT NOT NULL,
    caller     TEXT NOT NULL,
    callee     TEXT NOT NULL,
    start_time TIMESTAMP NOT NULL,
    end_time   TIMESTAMP,
    status     TEXT,
    created_at TIMESTAMP DEFAULT now()
);
```

## License

MIT License. See `LICENSE` file for details.
