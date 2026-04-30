# RediScan

[![CI](https://github.com/its-the-vibe/RediScan/actions/workflows/ci.yaml/badge.svg)](https://github.com/its-the-vibe/RediScan/actions/workflows/ci.yaml)

A lightweight web UI to inspect and page through Redis lists with automatic JSON pretty-printing and keyboard navigation.

## Features

- 🔍 **Inspect Redis Lists**: Browse through Redis list elements with a user-friendly web interface
- 📋 **List Discovery**: Automatically displays available Redis lists on the index page with clickable links
- 🎨 **JSON Pretty-Printing**: Automatically formats JSON data for easy reading
- ⌨️ **Keyboard Navigation**: Use arrow keys to navigate through list elements
- 🔒 **Secure**: Supports Redis password authentication
- 🐳 **Docker Ready**: Includes Dockerfile and docker-compose.yml for easy deployment
- 📦 **Minimal Size**: Uses scratch Docker image for minimal footprint

## Prerequisites

- Go 1.25+ (for local development)
- Docker and Docker Compose (for containerized deployment)
- An external Redis server

## Quick Start

### Using Docker Compose

1. Clone the repository:
```bash
git clone https://github.com/its-the-vibe/RediScan.git
cd RediScan
```

2. Configure your Redis connection by creating a `.env` file:
```bash
cp .env.example .env
# Edit .env with your Redis server details
```

3. Start the service:
```bash
docker-compose up -d
```

4. Access the UI at http://localhost:8080

### Local Development

1. Install dependencies:
```bash
go mod download
```

2. Set environment variables:
```bash
export REDIS_ADDR=localhost:6379
export REDIS_PASSWORD=your_password  # if required
export PORT=8080
```

3. Run the application:
```bash
go run main.go
```

4. Access the UI at http://localhost:8080

## Configuration

Configure the application using environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `REDIS_ADDR` | Redis server address (host:port) | `localhost:6379` |
| `REDIS_PASSWORD` | Redis password (if required) | (empty) |
| `REDIS_DB` | Redis database number | `0` |
| `PORT` | HTTP server port | `8080` |
| `MAX_LISTS` | Maximum number of lists to display on index page | `10` |

## Usage

### Web Interface

1. Navigate to the home page (http://localhost:8080)
2. View the list of available Redis lists with clickable links
3. Click on a list name to inspect it, or manually enter a Redis list key and starting index
4. Click "Inspect" to view the element
5. Use the navigation buttons or arrow keys (← →) to browse through the list

### API Endpoint

The service provides a REST endpoint:

```
GET /lindex?key=<redis_list_key>&index=<index>
```

**Parameters:**
- `key`: The name of the Redis list
- `index`: The index of the element to retrieve (0-based)

**Example:**
```bash
curl "http://localhost:8080/lindex?key=mylist&index=0"
```

**Response:**
- Returns an HTML page with the element value (pretty-printed if JSON)
- Returns a 404 page if:
  - The key doesn't exist
  - The key is not a list
  - The list is empty
  - The index is out of bounds

## Building

### Build Binary

```bash
go build -o rediscan main.go
```

### Build Docker Image

```bash
docker build -t rediscan:latest .
```

## Development

### Makefile

A `Makefile` is provided to standardise common development tasks:

| Target | Description |
|--------|-------------|
| `make build` | Compile the Go binary (`rediscan`) |
| `make test` | Run all unit tests |
| `make lint` | Run `go vet` static analysis |
| `make ci` | Run lint, build, and test (used in CI) |
| `make clean` | Remove the compiled binary |

### CI

A GitHub Actions workflow (`.github/workflows/ci.yaml`) runs `make ci` automatically on every push and pull request to `main`. The current build status is shown by the badge at the top of this README.

## Testing with Redis

To test the application, you'll need a Redis server with some sample data:

```bash
# Start a local Redis server (for testing only)
docker run -d --name redis-test -p 6379:6379 redis:alpine

# Add some test data
docker exec -it redis-test redis-cli LPUSH mylist '{"name": "Alice", "age": 30}'
docker exec -it redis-test redis-cli LPUSH mylist '{"name": "Bob", "age": 25}'
docker exec -it redis-test redis-cli LPUSH mylist '{"name": "Charlie", "age": 35}'

# Now access http://localhost:8080 and inspect the "mylist" key
```

## Security Considerations

- Store Redis credentials in environment variables or secrets management systems
- Use `.env` file for local development (already in `.gitignore`)
- Never commit credentials to version control
- Consider using TLS/SSL for Redis connections in production

## License

MIT License - feel free to use this project as you wish.
