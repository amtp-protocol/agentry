# AMTP Gateway Testing Guide

This document provides comprehensive information about testing the AMTP Gateway implementation.

## Test Structure

The testing strategy follows Go best practices with a mix of unit tests, integration tests, and benchmarks.

### Test Organization

```
agentry/
├── internal/
│   ├── types/
│   │   └── message_test.go          # Message type validation tests
│   ├── validation/
│   │   └── validator_test.go        # Request validation tests
│   ├── processing/
│   │   ├── processor_test.go        # Message processing unit tests
│   │   ├── delivery_test.go         # Delivery engine unit tests
│   │   └── test_utils.go           # Shared test utilities
│   ├── server/
│   │   └── handlers_test.go         # HTTP handler unit tests
│   └── errors/
│       └── errors_test.go           # Error handling tests
├── pkg/
│   └── uuid/
│       └── uuidv7_test.go          # UUID generation tests
├── tests/
│   ├── integration_test.go          # End-to-end integration tests
│   └── helpers.go                   # Test helpers and builders
└── scripts/
    └── run-tests.sh                 # Test runner script
```

## Running Tests

### Quick Test Run

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests with coverage
go test -cover ./...
```

### Using the Test Script

```bash
# Run comprehensive test suite
./scripts/run-tests.sh
```

The test script provides:
- ✅ Colored output for better readability
- 📊 Test coverage reporting
- 🏃 Benchmark execution
- 📈 Summary statistics

### Package-Specific Tests

```bash
# Test specific packages
go test ./internal/processing -v
go test ./internal/server -v
go test ./tests -v

# Run benchmarks
go test ./internal/processing -bench=.
go test ./tests -bench=.
```

## Test Categories

### 1. Unit Tests

**Location**: Alongside source code in `*_test.go` files

**Purpose**: Test individual components in isolation

**Examples**:
- Message validation logic
- UUID generation
- Error handling
- Processing algorithms
- Delivery mechanisms

**Key Features**:
- Mock dependencies for isolation
- Fast execution
- High code coverage
- Edge case testing

### 2. Integration Tests

**Location**: `tests/integration_test.go`

**Purpose**: Test complete request/response flows

**Coverage**:
- HTTP API endpoints
- Message lifecycle (send → process → deliver → status)
- Error handling across components
- Coordination types (parallel, sequential, conditional)
- Idempotency verification

**Key Features**:
- Real HTTP server testing
- End-to-end message flows
- Error scenario validation
- Performance benchmarking

### 3. Test Utilities

**Location**: `tests/helpers.go` and `internal/processing/test_utils.go`

**Purpose**: Provide reusable test infrastructure

**Components**:
- **TestMessageBuilder**: Fluent interface for creating test messages
- **TestSendRequestBuilder**: Builder for API requests
- **TestDataGenerator**: Random test data generation
- **MockDiscovery**: Mock DNS discovery service
- **MockDeliveryEngine**: Mock message delivery
- **TestAssertions**: Common validation helpers

## Test Patterns and Best Practices

### 1. Test Message Creation

```go
// Using the builder pattern
message := NewTestMessage().
    WithSender("test@example.com").
    WithRecipients("recipient1@test.com", "recipient2@test.com").
    WithSubject("Test Message").
    WithParallelCoordination(30).
    Build()
```

### 2. Mock Usage

```go
// Setup mocks
discovery := NewMockDiscovery()
discovery.SetCapabilities("test.com", &dns.AMTPCapabilities{
    Version: "1.0",
    Gateway: "https://test.com",
    MaxSize: 10485760,
})

deliveryEngine := NewMockDeliveryEngine()
deliveryEngine.SetDeliveryResult("recipient@test.com", &DeliveryResult{
    Status: types.StatusDelivered,
})
```

### 3. HTTP Testing

```go
// Create test server
server := createTestServer(t)
defer server.Close()

// Make request
resp, err := http.Post(server.URL+"/v1/messages", "application/json", body)
```

### 4. Error Testing

```go
// Test error scenarios
tests := []struct {
    name           string
    request        interface{}
    expectedStatus int
    expectedCode   string
}{
    {"Invalid JSON", "invalid json", 400, "INVALID_REQUEST_FORMAT"},
    {"Missing Sender", request, 400, "VALIDATION_FAILED"},
}
```

## Test Coverage

### Current Coverage Areas

✅ **Message Types and Validation**
- Message structure validation
- Coordination configuration validation
- Schema format validation
- Size calculations

✅ **Processing Engine**
- Immediate path processing
- Coordination types (parallel, sequential, conditional)
- Idempotency checking
- Status tracking
- Error handling

✅ **Delivery Engine**
- HTTPS delivery
- Retry logic with exponential backoff
- Connection pooling
- Error classification
- Batch delivery

✅ **HTTP Handlers**
- Message sending endpoint
- Message retrieval
- Status queries
- Health checks
- Error responses

✅ **Error Handling**
- Error code standardization
- HTTP status mapping
- Error response formatting
- Retryable error classification

✅ **Integration Flows**
- Complete message lifecycle
- Multi-recipient scenarios
- Coordination patterns
- Error propagation
- Idempotency verification

### Coverage Goals

- **Unit Tests**: >90% code coverage
- **Integration Tests**: All major user flows
- **Error Scenarios**: All error codes and edge cases
- **Performance**: Benchmark critical paths

## Test Data and Fixtures

### Valid Test Data

```go
const (
    ValidMessageID      = "01234567-89ab-7def-8123-456789abcdef"
    ValidIdempotencyKey = "01234567-89ab-4def-8123-456789abcdef"
    ValidEmail          = "test@example.com"
    ValidSchema         = "agntcy:commerce.order.v1"
)

var (
    SimplePayload = json.RawMessage(`{"message": "Hello, World!"}`)
    ComplexPayload = json.RawMessage(`{
        "order_id": "order-123",
        "customer": {"id": "cust-456", "name": "John Doe"},
        "items": [{"id": "item-1", "quantity": 2, "price": 19.99}],
        "total": 69.97
    }`)
)
```

### Random Data Generation

```go
generator := NewTestDataGenerator()

email := generator.RandomEmail()           // user123@example.com
emails := generator.RandomEmails(5)        // []string{...}
subject := generator.RandomSubject()       // "Important Update"
payload := generator.RandomPayload()       // Random JSON
schema := generator.RandomSchema()         // "agntcy:commerce.order.v1"
headers := generator.RandomHeaders()       // map[string]interface{}
```

## Performance Testing

### Benchmarks

```bash
# Run all benchmarks
go test -bench=. ./...

# Run specific benchmarks
go test -bench=BenchmarkProcessMessage ./internal/processing
go test -bench=BenchmarkDeliverMessage ./internal/processing
go test -bench=BenchmarkIntegration ./tests
```

### Benchmark Results

Typical performance characteristics:
- **Message Processing**: ~1ms per message (immediate path)
- **HTTP Request Handling**: ~2ms per request
- **UUID Generation**: ~100ns per UUID
- **Message Validation**: ~10μs per message

## Continuous Integration

### Test Pipeline

1. **Lint Check**: `golangci-lint run`
2. **Unit Tests**: `go test ./... -short`
3. **Integration Tests**: `go test ./tests`
4. **Coverage Report**: `go test -coverprofile=coverage.out ./...`
5. **Benchmark Regression**: `go test -bench=. -benchmem`

### Quality Gates

- ✅ All tests must pass
- ✅ Code coverage >85%
- ✅ No linting errors
- ✅ Benchmark performance within 10% of baseline

## Debugging Tests

### Verbose Output

```bash
go test -v ./internal/processing
```

### Specific Test

```bash
go test -run TestProcessMessage_ImmediatePath ./internal/processing
```

### Debug with Delve

```bash
dlv test ./internal/processing -- -test.run TestProcessMessage_ImmediatePath
```

### Test Logging

Tests use structured logging that can be enabled:

```go
// In test setup
logger := logging.NewLogger(config.LoggingConfig{Level: "debug"})
```

## Common Test Scenarios

### 1. Message Processing Flow

```go
func TestMessageLifecycle(t *testing.T) {
    // 1. Send message
    // 2. Verify processing
    // 3. Check delivery status
    // 4. Validate final state
}
```

### 2. Error Handling

```go
func TestErrorScenarios(t *testing.T) {
    // 1. Invalid input
    // 2. Processing failures
    // 3. Delivery failures
    // 4. Network errors
}
```

### 3. Coordination Patterns

```go
func TestCoordinationTypes(t *testing.T) {
    // 1. Parallel coordination
    // 2. Sequential coordination
    // 3. Conditional coordination
    // 4. Mixed scenarios
}
```

### 4. Performance Testing

```go
func BenchmarkHighLoad(b *testing.B) {
    // 1. Setup test environment
    // 2. Generate load
    // 3. Measure performance
    // 4. Validate results
}
```

## Test Maintenance

### Adding New Tests

1. **Identify test category** (unit/integration)
2. **Choose appropriate location** (alongside code or tests/)
3. **Use existing patterns** (builders, mocks, helpers)
4. **Follow naming conventions** (TestFunctionName_Scenario)
5. **Add to test script** if needed

### Updating Tests

1. **Keep tests in sync** with code changes
2. **Update mocks** when interfaces change
3. **Maintain test data** validity
4. **Review coverage** after changes

### Test Documentation

- **Document complex test scenarios**
- **Explain mock configurations**
- **Provide examples** for new patterns
- **Update this guide** when adding new test types

## Troubleshooting

### Common Issues

1. **Import cycles**: Use interfaces and dependency injection
2. **Flaky tests**: Avoid time dependencies, use mocks
3. **Slow tests**: Optimize setup, use parallel execution
4. **Coverage gaps**: Add tests for uncovered branches

### Test Failures

1. **Check error messages** for specific failures
2. **Verify test data** is still valid
3. **Review recent changes** that might affect tests
4. **Run tests in isolation** to identify conflicts

## Future Enhancements

### Planned Additions

- [ ] **Load Testing**: High-concurrency scenarios
- [ ] **Chaos Testing**: Network failures, service disruptions
- [ ] **Contract Testing**: API compatibility verification
- [ ] **Security Testing**: Authentication, authorization, input validation
- [ ] **Performance Regression**: Automated benchmark comparison

### Test Infrastructure

- [ ] **Test Containers**: Database and external service testing
- [ ] **Test Fixtures**: Standardized test data sets
- [ ] **Test Reporting**: Enhanced metrics and dashboards
- [ ] **Parallel Execution**: Faster test runs

This testing guide ensures comprehensive coverage of the AMTP Gateway functionality while maintaining high code quality and performance standards.
