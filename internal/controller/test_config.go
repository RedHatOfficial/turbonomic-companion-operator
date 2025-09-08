/*
Copyright 2025 Marek Paterczyk

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap/zapcore"
)

// TestConfig holds configuration for the test suite
type TestConfig struct {
	WebhookHost           string
	WebhookPort           int
	WebhookCertDir        string
	TestTimeout           time.Duration
	WebhookReadyTimeout   time.Duration
	CleanupTimeout        time.Duration
	LogLevel              zapcore.Level
	Development           bool
	IgnoreArgoCDResources bool
	EnableMetrics         bool
	EnableWebhookLogging  bool
	TestNamespacePrefix   string
	MaxTestRetries        int
	RetryInterval         time.Duration
}

// Environment variable names
const (
	EnvTestTimeout          = "TEST_TIMEOUT"
	EnvWebhookReadyTimeout  = "WEBHOOK_READY_TIMEOUT"
	EnvCleanupTimeout       = "CLEANUP_TIMEOUT"
	EnvLogLevel             = "LOG_LEVEL"
	EnvDevelopment          = "DEVELOPMENT"
	EnvIgnoreArgoCD         = "IGNORE_ARGOCD_RESOURCES"
	EnvEnableMetrics        = "ENABLE_METRICS"
	EnvEnableWebhookLogging = "ENABLE_WEBHOOK_LOGGING"
	EnvTestNamespacePrefix  = "TEST_NAMESPACE_PREFIX"
	EnvMaxTestRetries       = "MAX_TEST_RETRIES"
	EnvRetryInterval        = "RETRY_INTERVAL"
)

// DefaultTestConfig returns a default test configuration
func DefaultTestConfig() *TestConfig {
	return &TestConfig{
		WebhookHost:           "localhost",
		WebhookPort:           0,  // Will be set by envtest
		WebhookCertDir:        "", // Will be set by envtest
		TestTimeout:           time.Second * 30,
		WebhookReadyTimeout:   time.Second * 10,
		CleanupTimeout:        time.Second * 10,
		LogLevel:              zapcore.Level(-10),
		Development:           true,
		IgnoreArgoCDResources: true,
		EnableMetrics:         true,
		EnableWebhookLogging:  true,
		TestNamespacePrefix:   "turbo-test",
		MaxTestRetries:        3,
		RetryInterval:         time.Second * 2,
	}
}

// LoadTestConfigFromEnv loads test configuration from environment variables
func LoadTestConfigFromEnv() *TestConfig {
	config := DefaultTestConfig()

	// Load timeout configurations
	if val := os.Getenv(EnvTestTimeout); val != "" {
		if timeout, err := time.ParseDuration(val); err == nil {
			config.TestTimeout = timeout
		}
	}

	if val := os.Getenv(EnvWebhookReadyTimeout); val != "" {
		if timeout, err := time.ParseDuration(val); err == nil {
			config.WebhookReadyTimeout = timeout
		}
	}

	if val := os.Getenv(EnvCleanupTimeout); val != "" {
		if timeout, err := time.ParseDuration(val); err == nil {
			config.CleanupTimeout = timeout
		}
	}

	if val := os.Getenv(EnvRetryInterval); val != "" {
		if interval, err := time.ParseDuration(val); err == nil {
			config.RetryInterval = interval
		}
	}

	// Load log level
	if val := os.Getenv(EnvLogLevel); val != "" {
		if level, err := parseLogLevel(val); err == nil {
			config.LogLevel = level
		}
	}

	// Load boolean configurations
	if val := os.Getenv(EnvDevelopment); val != "" {
		config.Development = parseBool(val)
	}

	if val := os.Getenv(EnvIgnoreArgoCD); val != "" {
		config.IgnoreArgoCDResources = parseBool(val)
	}

	if val := os.Getenv(EnvEnableMetrics); val != "" {
		config.EnableMetrics = parseBool(val)
	}

	if val := os.Getenv(EnvEnableWebhookLogging); val != "" {
		config.EnableWebhookLogging = parseBool(val)
	}

	// Load string configurations
	if val := os.Getenv(EnvTestNamespacePrefix); val != "" {
		config.TestNamespacePrefix = val
	}

	// Load integer configurations
	if val := os.Getenv(EnvMaxTestRetries); val != "" {
		if retries, err := strconv.Atoi(val); err == nil && retries > 0 {
			config.MaxTestRetries = retries
		}
	}

	return config
}

// Validate validates the test configuration
func (tc *TestConfig) Validate() error {
	var errors []string

	if tc.TestTimeout <= 0 {
		errors = append(errors, "TestTimeout must be positive")
	}

	if tc.WebhookReadyTimeout <= 0 {
		errors = append(errors, "WebhookReadyTimeout must be positive")
	}

	if tc.CleanupTimeout <= 0 {
		errors = append(errors, "CleanupTimeout must be positive")
	}

	if tc.RetryInterval <= 0 {
		errors = append(errors, "RetryInterval must be positive")
	}

	if tc.MaxTestRetries <= 0 {
		errors = append(errors, "MaxTestRetries must be positive")
	}

	if tc.TestNamespacePrefix == "" {
		errors = append(errors, "TestNamespacePrefix cannot be empty")
	}

	if len(errors) > 0 {
		return fmt.Errorf("invalid test configuration: %s", strings.Join(errors, "; "))
	}

	return nil
}

// String returns a string representation of the test configuration
func (tc *TestConfig) String() string {
	return fmt.Sprintf(
		"TestConfig{TestTimeout: %v, WebhookReadyTimeout: %v, CleanupTimeout: %v, "+
			"LogLevel: %v, Development: %v, IgnoreArgoCDResources: %v, "+
			"EnableMetrics: %v, EnableWebhookLogging: %v, TestNamespacePrefix: %s, "+
			"MaxTestRetries: %d, RetryInterval: %v}",
		tc.TestTimeout, tc.WebhookReadyTimeout, tc.CleanupTimeout,
		tc.LogLevel, tc.Development, tc.IgnoreArgoCDResources,
		tc.EnableMetrics, tc.EnableWebhookLogging, tc.TestNamespacePrefix,
		tc.MaxTestRetries, tc.RetryInterval,
	)
}

// Clone creates a copy of the test configuration
func (tc *TestConfig) Clone() *TestConfig {
	return &TestConfig{
		WebhookHost:           tc.WebhookHost,
		WebhookPort:           tc.WebhookPort,
		WebhookCertDir:        tc.WebhookCertDir,
		TestTimeout:           tc.TestTimeout,
		WebhookReadyTimeout:   tc.WebhookReadyTimeout,
		CleanupTimeout:        tc.CleanupTimeout,
		LogLevel:              tc.LogLevel,
		Development:           tc.Development,
		IgnoreArgoCDResources: tc.IgnoreArgoCDResources,
		EnableMetrics:         tc.EnableMetrics,
		EnableWebhookLogging:  tc.EnableWebhookLogging,
		TestNamespacePrefix:   tc.TestNamespacePrefix,
		MaxTestRetries:        tc.MaxTestRetries,
		RetryInterval:         tc.RetryInterval,
	}
}

// WithTimeout returns a new config with the specified timeout
func (tc *TestConfig) WithTimeout(timeout time.Duration) *TestConfig {
	config := tc.Clone()
	config.TestTimeout = timeout
	return config
}

// WithLogLevel returns a new config with the specified log level
func (tc *TestConfig) WithLogLevel(level zapcore.Level) *TestConfig {
	config := tc.Clone()
	config.LogLevel = level
	return config
}

// WithNamespacePrefix returns a new config with the specified namespace prefix
func (tc *TestConfig) WithNamespacePrefix(prefix string) *TestConfig {
	config := tc.Clone()
	config.TestNamespacePrefix = prefix
	return config
}

// Helper functions

// parseLogLevel parses a log level string
func parseLogLevel(level string) (zapcore.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return zapcore.DebugLevel, nil
	case "info":
		return zapcore.InfoLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	case "fatal":
		return zapcore.FatalLevel, nil
	case "panic":
		return zapcore.PanicLevel, nil
	default:
		// Try to parse as numeric level
		if num, err := strconv.Atoi(level); err == nil {
			return zapcore.Level(num), nil
		}
		return zapcore.InfoLevel, fmt.Errorf("invalid log level: %s", level)
	}
}

// parseBool parses a boolean string
func parseBool(val string) bool {
	switch strings.ToLower(val) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return false
	}
}
