package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	internalLog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
)

// InstrumentationConfig holds configuration for instrumentation
type InstrumentationConfig struct {
	// OTEL Configuration
	BaseURL      *string
	APIKey       *string
	PublicAPIKey *string
	Headers      *map[string]string

	// Attributes Configuration
	ServiceName *string
	InstanceID  *string
	Env         *string
	GitCommit   *string
	GitHubURL   *string
}

// getDefaultConfig generates the default configuration values
func getDefaultConfig() InstrumentationConfig {
	defaultBaseURL := getEnv("BASE_URL", nil)
	if defaultBaseURL == nil {
		defaultBaseURL = stringPtr("api.iudex.ai")
	}
	defaultAPIKey := getEnv("API_KEY", nil)
	defaultPublicAPIKey := getEnv("PUBLIC_API_KEY", nil)
	defaultServiceName := getEnv("SERVICE_NAME", nil)
	if defaultServiceName == nil {
		defaultServiceName = stringPtr("default-service")
	}
	defaultInstanceID := getEnv("INSTANCE_ID", nil)
	defaultEnv := getEnv("ENVIRONMENT", nil)
	if defaultEnv == nil {
		defaultEnv = stringPtr("development")
	}
	defaultGitCommit := getEnv("GIT_COMMIT", nil)

	return InstrumentationConfig{
		BaseURL:      defaultBaseURL,
		APIKey:       defaultAPIKey,
		PublicAPIKey: defaultPublicAPIKey,
		ServiceName:  defaultServiceName,
		InstanceID:   defaultInstanceID,
		Env:          defaultEnv,
		GitCommit:    defaultGitCommit,
	}
}

// getEnv retrieves the value of the environment variable named by the key or returns nil if not found
func getEnv(key string, defaultValue *string) *string {
	if value, exists := os.LookupEnv(key); exists {
		return &value
	}
	return defaultValue
}

// stringPtr returns a pointer to the given string
func stringPtr(s string) *string {
	return &s
}

// setupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func setupOTelSDK(ctx context.Context, config InstrumentationConfig) (shutdown func(context.Context) error, err error) {
	var shutdownFuncs []func(context.Context) error

	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	// handleErr calls shutdown for cleanup and makes sure that all errors are returned.
	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	// Set default values if not provided
	defaults := getDefaultConfig()
	if config.ServiceName == nil {
		config.ServiceName = defaults.ServiceName
	}
	if config.InstanceID == nil {
		config.InstanceID = defaults.InstanceID
	}
	if config.Env == nil {
		config.Env = defaults.Env
	}
	if config.GitCommit == nil {
		config.GitCommit = defaults.GitCommit
	}
	if config.BaseURL == nil {
		config.BaseURL = defaults.BaseURL
	}

	// Set up propagator.
	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	// Set up resource.
	res, err := newResource(ctx, config)
	if err != nil {
		handleErr(err)
		return
	}

	// Set up headers.
	headers, err := newHeaders(config)
	if err != nil {
		handleErr(err)
		return
	}

	// Set up trace provider.
	tracerProvider, err := newTraceProvider(ctx, config, res, headers)
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	// Set up logger provider.
	loggerProvider, err := newLoggerProvider(ctx, config, res, headers)
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
	global.SetLoggerProvider(loggerProvider)

	return
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func newResource(ctx context.Context, config InstrumentationConfig) (*resource.Resource, error) {
	// Create resource with service information
	attributes := []attribute.KeyValue{}
	if config.ServiceName != nil {
		attributes = append(attributes, attribute.String("service.name", *config.ServiceName))
	}
	if config.InstanceID != nil {
		attributes = append(attributes, attribute.String("service.instance.id", *config.InstanceID))
	}
	if config.Env != nil {
		attributes = append(attributes, attribute.String("env", *config.Env))
	}
	if config.GitCommit != nil {
		attributes = append(attributes, attribute.String("git.commit", *config.GitCommit))
	}
	if config.GitHubURL != nil {
		attributes = append(attributes, attribute.String("github.url", *config.GitHubURL))
	}

	res, err := resource.New(ctx, resource.WithAttributes(attributes...))
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	return res, nil
}

func newHeaders(config InstrumentationConfig) (*map[string]string, error) {
	if config.APIKey == nil && config.PublicAPIKey == nil {
		return nil, fmt.Errorf("PUBLIC_WRITE_ONLY_IUDEX_API_KEY environment variable is missing or empty")
	}

	headers := map[string]string{}

	if config.PublicAPIKey != nil {
		headers["x-write-only-api-key"] = *config.PublicAPIKey
	} else if config.APIKey != nil {
		headers["x-api-key"] = *config.APIKey
	}

	return &headers, nil
}

func newTraceProvider(ctx context.Context, config InstrumentationConfig, res *resource.Resource, headers *map[string]string) (*trace.TracerProvider, error) {
	baseURL := "api.iudex.ai"
	if config.BaseURL != nil {
		baseURL = *config.BaseURL
	}

	traceExporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(baseURL),
		otlptracehttp.WithHeaders(*headers),
	)
	if err != nil {
		return nil, err
	}

	traceProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter,
			trace.WithBatchTimeout(time.Second)),
		trace.WithResource(res),
	)
	return traceProvider, nil
}

func newLoggerProvider(ctx context.Context, config InstrumentationConfig, res *resource.Resource, headers *map[string]string) (*log.LoggerProvider, error) {
	baseURL := "api.iudex.ai"
	if config.BaseURL != nil {
		baseURL = *config.BaseURL
	}

	logExporter, err := otlploghttp.New(ctx,
		otlploghttp.WithEndpoint(baseURL),
		otlploghttp.WithHeaders(*headers),
	)
	if err != nil {
		return nil, err
	}

	processor := log.NewBatchProcessor(logExporter)
	loggerProvider := log.NewLoggerProvider(
		log.WithResource(res),
		log.WithProcessor(processor),
	)
	return loggerProvider, nil
}

func getLoggerProvider() internalLog.LoggerProvider {
	return global.GetLoggerProvider()
}

func newSlogLogger(name string) *slog.Logger {
	provider := getLoggerProvider()
	return otelslog.NewLogger(name, otelslog.WithLoggerProvider(provider))
}

func newZapLogger(name string) *zap.Logger {
	provider := getLoggerProvider()
	return zap.New(otelzap.NewCore(name, otelzap.WithLoggerProvider(provider)))
}
