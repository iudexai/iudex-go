# IUDEX Go

Next generation observability for Go applications. IUDEX Go provides tracing and logging capabilities to help you monitor and debug your infrastructure easily.


### Table of Contents
- [IUDEX Go](#iudex-go)
    - [Table of Contents](#table-of-contents)
- [Getting Started](#getting-started)
- [Usage](#usage)
    - [Setup with OTel SDK](#setup-with-otel-sdk)
    - [Tracing Functions](#tracing-functions)
    - [Chi Instrumentation](#chi-instrumentation)
- [Appendix](#appendix)


# Getting Started
Instrumenting your Go application with IUDEX takes a few steps:

1. **Install the necessary dependencies**
  Install IUDEX Go and other dependencies with the following command:
  ```bash
  go get github.com/iudexai/iudex-go
  ```

2. **Set up the IUDEX SDK**
  Use `SetupOTelSDK` with a configuration to initialize IUDEX:
  ```go
  package main

  import (
      "context"
      "log"
      "github.com/iudexai/iudex-go"
  )

  func main() {
      // Set up IUDEX with custom configuration
      config := iudex.InstrumentationConfig{
          ServiceName:    "my-service",
      }
      if err := iudex.SetupOTelSDK(context.Background(), config); err != nil {
          log.Fatalf("failed to set up IUDEX with OTel SDK: %v", err)
      }
      defer iudex.Shutdown()

      // Your application code here
  }
  ```

3. **Add tracing to your functions**
   Use `StartSpan` to trace specific sections of your code:
   ```go
   func doSomething(ctx context.Context) {
       ctx, span := iudex.StartSpan(ctx, "doSomething")
       defer span.End()

       // Function logic here
   }
   ```

4. **Send over logs**
  After instrumenting, add `NewSlogLogger` or `NewZapLogger` to create a logger that send OTel logs:
  ```go
  func main() {
      // Set up IUDEX with custom configuration
      config := iudex.InstrumentationConfig{
          ServiceName:    "my-service",
      }
      if err := iudex.SetupOTelSDK(context.Background(), config); err != nil {
          log.Fatalf("failed to set up IUDEX with OTel SDK: %v", err)
      }
      defer iudex.Shutdown()

      logger := iudex.NewSlogLogger("main")

      // Your application code here
  }
  ```

# Usage
### Setup with OTel SDK
To further customize the setup, IUDEX provides `SetupOTelSDK` and `InstrumentationConfig` for configuring OpenTelemetry instrumentation.

```go
package main

import (
    "context"
    "log"
    "github.com/iudexai/iudex-go"
)

func main() {
    // Set up IUDEX with custom configuration
    config := iudex.InstrumentationConfig{
        ServiceName:    "my-service",
        ServiceVersion: "1.0.0",
    }
    if err := iudex.SetupOTelSDK(context.Background(), config); err != nil {
        log.Fatalf("failed to set up IUDEX with OTel SDK: %v", err)
    }
    defer iudex.Shutdown()

    // Your application code here
}
```

### Tracing Functions
You can add tracing to specific functions in your Go application to monitor performance and gather detailed telemetry.

```go
func doSomething(ctx context.Context) {
    ctx, span := iudex.StartSpan(ctx, "doSomething")
    defer span.End()

    // Function logic here
}
```

### Chi Instrumentation
To instrument your Go application that uses the Chi router, you can use IUDEX to add observability with minimal changes. Below is a more detailed example that includes multiple endpoints and middleware usage:

1. Install OTel Chi middleware `go get github.com/riandyrn/otelchi`.

2. Set up a config `var iudexConfig = iudex.InstrumentationConfig{`, instrument using `iudex.SetupOTelSDK`, add instrumented logger `logger := iudex.NewSlogLogger("main")`, and add OTel middleware `r.Use(otelchi.Middleware("instrumented-chi", otelchi.WithChiRoutes(r)))`
```go
package main

import (
    "context"
    "log"
    "net/http"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
    "github.com/iudexai/iudex-go"
    "github.com/riandyrn/otelchi"
)

type RequestBody struct {
    Field1 string `json:"field1"`
    Field2 int    `json:"field2"`
}

func stringPtr(s string) *string {
  return &s
}

// Set up IUDEX with configuration
var iudexConfig = iudex.InstrumentationConfig{
  PublicAPIKey: stringPtr("YOUR_WRITE_ONLY_KEY_HERE"), // Its okay to commit this
  ServiceName:  stringPtr("MY_SERVICE_NAME"),
  Env:          stringPtr("YOUR_ENV"),
}

func main() {
    // Set up OpenTelemetry.
    otelShutdown, err := iudex.SetupOTelSDK(ctx, iudexConfig)
    if err != nil {
      return
    }

    // Handle shutdown properly so nothing leaks.
    defer func() {
      err = errors.Join(err, otelShutdown(context.Background()))
    }()

    // Create instrumented logger
    logger := iudex.NewSlogLogger("main")

    r := chi.NewRouter()
    r.Use(middleware.RequestID)
    r.Use(middleware.RealIP)
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)

    // Add otel middleware
    r.Use(otelchi.Middleware("instrumented-chi", otelchi.WithChiRoutes(r)))

    // Define routes
    r.Get("/", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Hello, World!"))
    })

    r.Get("/time", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte(time.Now().String()))
    })

    r.Post("/data", func(w http.ResponseWriter, r *http.Request) {
      var reqBody RequestBody

      // Decode the JSON request body
      if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
          http.Error(w, "Invalid request body", http.StatusBadRequest)
          return
      }

      // Log the Field1 value using slog
      logger.Info("Request received", "Field1", reqBody.Field1)

      w.Write([]byte("Data received"))
    })

    r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("OK"))
    })

    http.ListenAndServe(":8080", r)
}
```

# Appendix
The `main.go` file demonstrates several key exported functions of IUDEX Go in detail:

1. **SetupOTelSDK**
   - **Description**: Configures the OpenTelemetry SDK for advanced settings.
   - **Parameters**:
     - `ctx (context.Context)`: The context for managing the lifecycle of the setup process.
     - `config (InstrumentationConfig)`: Configuration parameters such as `ServiceName` and `ServiceVersion` for trace identification.
   - **Usage**: Use this function to customize the OpenTelemetry setup for your application, specifying additional options.

2. **StartSpan**
   - **Description**: Begins a new trace span, allowing you to monitor execution time and gather telemetry data for a specific code block or function.
   - **Parameters**:
     - `ctx (context.Context)`: The context to link the trace span to.
     - `name (string)`: The name of the span, used to identify the trace.
   - **Returns**:
     - `context.Context`: A new context linked to the started span.
     - `trace.Span`: The span that was started.
   - **Usage**: Use this function to trace specific sections of your code, helping to identify bottlenecks or performance issues.
