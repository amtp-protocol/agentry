# Development

## Environment Setup

```bash
# Setup development environment
make setup

# Run in development mode
make dev
```

## Building

```bash
# Build gateway for current platform
make build

# Build admin tool
make build-admin

# Build both gateway and admin tool
make build-all

# Build for specific platform
make build-linux
make build-darwin
make build-windows

# Build Docker image
make docker-build
```

## Testing

```bash
# Run tests
make test

# Run tests with coverage
make test-coverage

# Run benchmarks
make benchmark
```

See [TESTING.md](TESTING.md) for the test structure and patterns, [LOCAL_TESTING.md](LOCAL_TESTING.md) for mock DNS testing, and [DOCKER_TESTING.md](DOCKER_TESTING.md) for the multi-domain Docker simulation.

## Code Quality

```bash
# Format code
make fmt

# Run linter
make lint

# Run security scan
make security-scan

# Run all checks
make ci
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

Guidelines:

- Follow Go best practices and idioms
- Write comprehensive tests for new features
- Update documentation for API changes
- Run `make ci` before submitting PRs
- Follow the existing code style and patterns
