# Turbonomic Companion Operator Test Suite

This document describes the improved test suite architecture for the Turbonomic Companion Operator.

## Overview

The test suite has been significantly improved with better organization, configuration management, and utility functions. The improvements include:

- **Modular Architecture**: Separated concerns into different files
- **Configuration Management**: Environment-based configuration with validation
- **Test Utilities**: Reusable test helpers and assertions
- **Metrics Testing**: Dedicated utilities for Prometheus metrics testing
- **Better Error Handling**: Improved error handling and cleanup procedures

## File Structure

```
internal/controller/
├── suite_test.go              # Main test suite setup and teardown
├── test_config.go             # Configuration management
├── test_utils.go              # Test utilities and helpers
├── metrics_test_utils.go      # Metrics testing utilities
├── workload_resources_mutator_test.go  # Existing tests
└── README_TEST_SUITE.md       # This documentation
```

## Configuration

The test suite supports configuration through environment variables:

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `TEST_TIMEOUT` | `30s` | Timeout for test operations |
| `WEBHOOK_READY_TIMEOUT` | `10s` | Timeout for webhook server readiness |
| `CLEANUP_TIMEOUT` | `10s` | Timeout for cleanup operations |
| `LOG_LEVEL` | `-10` | Log level (debug, info, warn, error) |
| `DEVELOPMENT` | `true` | Enable development mode |
| `IGNORE_ARGOCD_RESOURCES` | `true` | Ignore ArgoCD managed resources |
| `ENABLE_METRICS` | `true` | Enable metrics collection |
| `ENABLE_WEBHOOK_LOGGING` | `true` | Enable webhook logging |
| `TEST_NAMESPACE_PREFIX` | `turbo-test` | Prefix for test namespaces |
| `MAX_TEST_RETRIES` | `3` | Maximum test retries |
| `RETRY_INTERVAL` | `2s` | Interval between retries |

### Example Configuration

```bash
export TEST_TIMEOUT=60s
export LOG_LEVEL=debug
export TEST_NAMESPACE_PREFIX=my-test
export MAX_TEST_RETRIES=5
```

## Test Utilities

### TestSuite

The `TestSuite` provides a high-level interface for managing test resources:

```go
// Create a new test suite
suite := NewTestSuite("my-namespace", k8sClient, k8sClientTurbo, ctx, testConfig)

// Setup test environment
suite.Setup()
defer suite.Teardown()

// Create deployments
deployment := suite.CreateDeployment("my-deployment")
turboDeployment := suite.CreateDeploymentWithTurbo("turbo-deployment")
```

### TestDeployment

The `TestDeployment` provides methods for managing deployment resources:

```go
// Create a deployment
deployment := NewTestDeployment("my-deployment", "my-namespace", k8sClient, ctx)

// Set resources
deployment.SetResources("500m", "1", "1Gi", "2Gi")

// Set annotations
deployment.SetAnnotation("turbo.ibm.com/override", "true")

// Create and wait
deployment.Create()
deployment.WaitForExists(30 * time.Second)

// Update
deployment.Update()

// Cleanup
deployment.Delete()
deployment.WaitForDeletion(30 * time.Second)
```

### Metrics Testing

The `MetricsTestHelper` provides utilities for testing Prometheus metrics:

```go
// Create metrics helper
metrics := NewMetricsTestHelper("my-namespace", "Deployment", "my-deployment", "main")

// Assert specific metrics
metrics.AssertTurboOverridesTotal(1.0)
metrics.AssertTurboRecommendedTotal(1.0)
metrics.AssertTurboRecommendedRequestCpuGauge(500.0)

// Assert all metrics at once
metrics.AssertAllMetrics(1.0, 1.0, 500.0, 1000.0, 1073741824.0, 2147483648.0)

// Wait for metrics to change
metrics.WaitForMetricsChange(2.0, 2.0, 400.0, 800.0, 1073741824.0, 2147483648.0, 30*time.Second)

// Take snapshots
snapshot := metrics.TakeSnapshot()
// ... perform actions ...
metrics.AssertSnapshotChange(snapshot, 30*time.Second)
```

## Writing Tests

### Basic Test Structure

```go
var _ = Describe("WorkloadResourcesMutator webhook", func() {
    var suite *TestSuite
    var metrics *MetricsTestHelper

    BeforeEach(func() {
        suite = NewTestSuite("test-namespace", k8sClient, k8sClientTurbo, ctx, testConfig)
        suite.Setup()
        
        metrics = NewMetricsTestHelper("test-namespace", "Deployment", "test-deployment", "main")
    })

    AfterEach(func() {
        suite.Teardown()
    })

    It("should handle resource overrides correctly", func() {
        // Create deployment
        deployment := suite.CreateDeployment("test-deployment")
        
        // Verify initial state
        AssertResourceValues(deployment, "1", "2", "1Gi", "2Gi")
        
        // Simulate Turbonomic changes
        deployment.SetResources("500m", "1", "2Gi", "4Gi")
        deployment.Update()
        
        // Verify changes
        WaitForResourceChange(deployment, "500m", "1", "2Gi", "4Gi", 30*time.Second)
        
        // Verify metrics
        metrics.AssertAllMetrics(0.0, 1.0, 500.0, 1000.0, 2147483648.0, 4294967296.0)
    })
})
```

### Advanced Test Patterns

#### Testing with Retries

```go
It("should eventually succeed with retries", func() {
    Eventually(func() error {
        // Perform operation that might fail
        return someOperation()
    }, testConfig.TestTimeout, testConfig.RetryInterval).Should(Succeed())
})
```

#### Testing Error Conditions

```go
It("should handle invalid resources gracefully", func() {
    deployment := suite.CreateDeployment("invalid-deployment")
    
    // Set invalid resources
    deployment.SetResources("invalid", "invalid", "invalid", "invalid")
    
    // Expect update to fail
    Expect(deployment.Update()).ShouldNot(Succeed())
})
```

#### Testing Metrics Changes

```go
It("should track metrics correctly", func() {
    // Take initial snapshot
    snapshot := metrics.TakeSnapshot()
    
    // Perform action
    deployment := suite.CreateDeployment("metrics-test")
    deployment.SetResources("300m", "600m", "512Mi", "1Gi")
    deployment.Update()
    
    // Verify metrics changed
    metrics.AssertSnapshotChange(snapshot, 30*time.Second)
    
    // Verify specific values
    metrics.AssertTurboRecommendedRequestCpuGauge(300.0)
    metrics.AssertTurboRecommendedLimitCpuGauge(600.0)
})
```

## Best Practices

### 1. Use TestSuite for Resource Management

Always use `TestSuite` for managing test namespaces and deployments:

```go
// Good
suite := NewTestSuite("test-ns", k8sClient, k8sClientTurbo, ctx, testConfig)
suite.Setup()
defer suite.Teardown()

// Avoid manual namespace creation
```

### 2. Use MetricsTestHelper for Metrics Testing

Use the dedicated metrics helper for all metrics assertions:

```go
// Good
metrics := NewMetricsTestHelper(namespace, kind, name, container)
metrics.AssertAllMetrics(overrides, recommended, cpuReq, cpuLim, memReq, memLim)

// Avoid direct metric access
```

### 3. Use Configuration for Flexibility

Use environment variables to configure test behavior:

```go
// Good - configurable
timeout := testConfig.TestTimeout

// Avoid - hardcoded
timeout := 30 * time.Second
```

### 4. Clean Up Resources

Always clean up test resources:

```go
// Good
defer suite.Teardown()

// Or manually
defer func() {
    deployment.Delete()
    deployment.WaitForDeletion(testConfig.TestTimeout)
}()
```

### 5. Use Descriptive Test Names

Use descriptive test names that explain the scenario:

```go
// Good
It("should override resources when owner changes turbo.ibm.com/override annotation to false", func() {

// Avoid
It("should work", func() {
```

## Running Tests

### Basic Test Execution

```bash
# Run all tests
go test ./internal/controller/...

# Run specific test file
go test ./internal/controller/ -run TestControllers

# Run with verbose output
go test ./internal/controller/ -v
```

### Running with Custom Configuration

```bash
# Set custom timeouts
export TEST_TIMEOUT=60s
export WEBHOOK_READY_TIMEOUT=20s

# Set log level
export LOG_LEVEL=debug

# Run tests
go test ./internal/controller/...
```

### Running in CI/CD

For CI/CD environments, consider these settings:

```bash
export TEST_TIMEOUT=120s
export WEBHOOK_READY_TIMEOUT=30s
export CLEANUP_TIMEOUT=30s
export LOG_LEVEL=info
export MAX_TEST_RETRIES=5
export RETRY_INTERVAL=5s
```

## Troubleshooting

### Common Issues

1. **Webhook Server Not Ready**
   - Increase `WEBHOOK_READY_TIMEOUT`
   - Check webhook certificate generation

2. **Test Timeouts**
   - Increase `TEST_TIMEOUT`
   - Check for resource contention

3. **Metrics Not Updating**
   - Verify metrics are enabled (`ENABLE_METRICS=true`)
   - Check metric label values

4. **Cleanup Failures**
   - Increase `CLEANUP_TIMEOUT`
   - Check for finalizers blocking deletion

### Debug Mode

Enable debug mode for detailed logging:

```bash
export LOG_LEVEL=debug
export ENABLE_WEBHOOK_LOGGING=true
```

### Performance Optimization

For faster test execution:

```bash
export TEST_TIMEOUT=30s
export WEBHOOK_READY_TIMEOUT=5s
export CLEANUP_TIMEOUT=5s
export MAX_TEST_RETRIES=1
export RETRY_INTERVAL=1s
```

## Contributing

When adding new tests:

1. Use the provided utilities (`TestSuite`, `TestDeployment`, `MetricsTestHelper`)
2. Follow the established patterns
3. Add appropriate cleanup
4. Use descriptive test names
5. Consider configuration flexibility
6. Add documentation for complex scenarios

## Migration Guide

If you have existing tests, here's how to migrate them:

### Before (Old Style)

```go
var _ = Describe("Old test", func() {
    It("should work", func() {
        // Manual namespace creation
        ns := &corev1.Namespace{...}
        Expect(k8sClient.Create(ctx, ns)).Should(Succeed())
        
        // Manual deployment creation
        deployment := &appsv1.Deployment{...}
        Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())
        
        // Manual cleanup
        Expect(k8sClient.Delete(ctx, deployment)).Should(Succeed())
        Expect(k8sClient.Delete(ctx, ns)).Should(Succeed())
    })
})
```

### After (New Style)

```go
var _ = Describe("New test", func() {
    var suite *TestSuite
    
    BeforeEach(func() {
        suite = NewTestSuite("test-ns", k8sClient, k8sClientTurbo, ctx, testConfig)
        suite.Setup()
    })
    
    AfterEach(func() {
        suite.Teardown()
    })
    
    It("should work", func() {
        // Use utilities
        deployment := suite.CreateDeployment("test-deployment")
        
        // Use assertions
        AssertResourceValues(deployment, "1", "2", "1Gi", "2Gi")
        
        // Automatic cleanup via TestSuite
    })
})
``` 