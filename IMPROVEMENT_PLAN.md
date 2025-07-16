# План улучшения кода (Action Plan)

## Фаза 1: Критические исправления (1-2 недели)

### 1.1 Исправление panic в логгере
**Файл:** `cmd/pkg/logging/logging.go`
**Проблема:** panic() в init() может привести к падению приложения
**Решение:**
```go
// Вместо panic(err)
func initLogger() (*logrus.Logger, error) {
    l := logrus.New()
    // ... настройка логгера ...
    
    err := os.MkdirAll("logs", 0644)
    if err != nil {
        return nil, fmt.Errorf("failed to create logs directory: %w", err)
    }
    
    allFile, err := os.OpenFile("logs/all.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
    if err != nil {
        return nil, fmt.Errorf("failed to open log file: %w", err)
    }
    
    // ... остальная настройка ...
    return l, nil
}
```

### 1.2 Вынос конфигурации из хардкода
**Файл:** `cmd/main/app.go`
**Создать:** `cmd/config/database.go`
```go
type DatabaseConfig struct {
    Host     string `env:"DB_HOST" env-default:"localhost"`
    Port     int    `env:"DB_PORT" env-default:"5435"`
    User     string `env:"DB_USER" env-default:"root"`
    Password string `env:"DB_PASSWORD" env-default:"secret"`
    Name     string `env:"DB_NAME" env-default:"tendersdb"`
    SSLMode  string `env:"DB_SSLMODE" env-default:"disable"`
}
```

### 1.3 Добавление graceful shutdown
**Файл:** `cmd/main/app.go`
```go
import (
    "context"
    "os"
    "os/signal"
    "syscall"
    "time"
)

func main() {
    // ... existing code ...
    
    // Setup graceful shutdown
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()
    
    go func() {
        if err := server.Start(serverAddress); err != nil && err != http.ErrServerClosed {
            logger.Fatalf("Server startup failed: %v", err)
        }
    }()
    
    <-ctx.Done()
    logger.Info("Shutting down gracefully...")
    
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    
    if err := server.Shutdown(shutdownCtx); err != nil {
        logger.Errorf("Server shutdown error: %v", err)
    }
}
```

## Фаза 2: Базовые тесты (2-3 недели)

### 2.1 Unit тесты для service layer
**Создать:** `cmd/internal/services/tender_services_test.go`
```go
func TestTenderProcessingService_GetOrCreateObject(t *testing.T) {
    tests := []struct {
        name        string
        objectTitle string
        address     string
        want        db.Object
        wantErr     bool
    }{
        {
            name:        "create new object",
            objectTitle: "Test Object",
            address:     "Test Address",
            want:        db.Object{ID: 1, Title: "Test Object", Address: "Test Address"},
            wantErr:     false,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ... test implementation
        })
    }
}
```

### 2.2 Тесты для handlers
**Создать:** `cmd/internal/server/handlers_test.go`
```go
func TestServer_listTendersHandler(t *testing.T) {
    gin.SetMode(gin.TestMode)
    
    mockStore := &MockStore{}
    server := NewServer(mockStore, logger, tenderService, cfg)
    
    router := gin.New()
    router.GET("/api/v1/tenders", server.listTendersHandler)
    
    req := httptest.NewRequest("GET", "/api/v1/tenders?page=1&page_size=10", nil)
    w := httptest.NewRecorder()
    
    router.ServeHTTP(w, req)
    
    assert.Equal(t, http.StatusOK, w.Code)
    // ... additional assertions
}
```

### 2.3 Integration тесты
**Создать:** `tests/integration/`
- Тесты с реальной БД (testcontainers)
- Тесты API endpoints
- Тесты импорта данных

## Фаза 3: Middleware и безопасность (1-2 недели)

### 3.1 Recovery middleware
**Создать:** `cmd/internal/middleware/recovery.go`
```go
func RecoveryMiddleware(logger *logging.Logger) gin.HandlerFunc {
    return gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
        logger.Errorf("Panic recovered: %v", recovered)
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Internal server error",
        })
    })
}
```

### 3.2 Request logging middleware
**Создать:** `cmd/internal/middleware/logging.go`
```go
func RequestLoggingMiddleware(logger *logging.Logger) gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        
        c.Next()
        
        logger.WithFields(logrus.Fields{
            "method":     c.Request.Method,
            "path":       c.Request.URL.Path,
            "status":     c.Writer.Status(),
            "duration":   time.Since(start),
            "ip":         c.ClientIP(),
            "user_agent": c.Request.UserAgent(),
        }).Info("Request processed")
    }
}
```

### 3.3 Input validation middleware
**Создать:** `cmd/internal/middleware/validation.go`
```go
func ValidateJSONMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        if c.Request.Method == "POST" || c.Request.Method == "PUT" || c.Request.Method == "PATCH" {
            if c.GetHeader("Content-Type") != "application/json" {
                c.JSON(http.StatusBadRequest, gin.H{
                    "error": "Content-Type must be application/json",
                })
                c.Abort()
                return
            }
        }
        c.Next()
    }
}
```

### 3.4 Rate limiting
**Добавить dependency:** `golang.org/x/time/rate`
```go
func RateLimitMiddleware(rps rate.Limit, burst int) gin.HandlerFunc {
    limiter := rate.NewLimiter(rps, burst)
    
    return func(c *gin.Context) {
        if !limiter.Allow() {
            c.JSON(http.StatusTooManyRequests, gin.H{
                "error": "Rate limit exceeded",
            })
            c.Abort()
            return
        }
        c.Next()
    }
}
```

## Фаза 4: Документация и мониторинг (1-2 недели)

### 4.1 API документация (OpenAPI/Swagger)
**Добавить dependency:** `github.com/swaggo/gin-swagger`
```go
// @title Tenders API
// @version 1.0
// @description API для управления тендерами и заявками
// @host localhost:8080
// @BasePath /api/v1

// @Summary Получить список тендеров
// @Description Возвращает пагинированный список тендеров
// @Tags tenders
// @Accept json
// @Produce json
// @Param page query int false "Номер страницы" default(1)
// @Param page_size query int false "Размер страницы" default(10)
// @Success 200 {array} listTendersResponse
// @Router /tenders [get]
func (s *Server) listTendersHandler(c *gin.Context) {
    // ... existing implementation
}
```

### 4.2 Health checks
**Создать:** `cmd/internal/server/handlers_health.go`
```go
func (s *Server) healthHandler(c *gin.Context) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    // Check database connectivity
    if err := s.store.Ping(ctx); err != nil {
        c.JSON(http.StatusServiceUnavailable, gin.H{
            "status":   "unhealthy",
            "database": "down",
            "error":    err.Error(),
        })
        return
    }
    
    c.JSON(http.StatusOK, gin.H{
        "status":   "healthy",
        "database": "up",
        "version":  "1.0.0",
    })
}
```

### 4.3 Metrics (Prometheus)
**Добавить dependency:** `github.com/prometheus/client_golang`
```go
var (
    httpRequestsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "http_requests_total",
            Help: "Total number of HTTP requests",
        },
        []string{"method", "endpoint", "status"},
    )
    
    httpRequestDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "http_request_duration_seconds",
            Help: "HTTP request duration",
        },
        []string{"method", "endpoint"},
    )
)
```

## Фаза 5: Оптимизация и advanced features (2-3 недели)

### 5.1 Кэширование
**Добавить:** Redis для кэширования часто запрашиваемых данных
```go
type CacheService struct {
    redis  *redis.Client
    logger *logging.Logger
}

func (c *CacheService) GetTenders(key string) ([]db.Tender, error) {
    // Implementation
}
```

### 5.2 Background jobs
**Добавить:** Background processing для тяжелых операций
```go
type JobQueue struct {
    jobs   chan Job
    worker int
}

func (jq *JobQueue) ProcessImport(data api_models.FullTenderData) {
    // Async processing
}
```

### 5.3 Distributed tracing
**Добавить:** OpenTelemetry для tracing
```go
func TracingMiddleware() gin.HandlerFunc {
    return otelgin.Middleware("tenders-api")
}
```

## Чек-лист выполнения

### Критические (Must-have):
- [ ] Убрать panic из логгера
- [ ] Вынести конфигурацию в переменные окружения
- [ ] Добавить graceful shutdown
- [ ] Написать unit тесты для service layer (покрытие >70%)
- [ ] Добавить integration тесты для ключевых endpoints

### Важные (Should-have):
- [ ] Recovery middleware
- [ ] Request logging middleware
- [ ] Input validation middleware
- [ ] Rate limiting
- [ ] Health checks
- [ ] API документация (Swagger)

### Желательные (Nice-to-have):
- [ ] Metrics (Prometheus)
- [ ] Кэширование (Redis)
- [ ] Distributed tracing
- [ ] Background jobs
- [ ] Performance benchmarks

## Ожидаемые результаты

После выполнения всех фаз:
- **Надежность:** 95% uptime с graceful shutdown
- **Безопасность:** Basic authentication, rate limiting, input validation
- **Мониторинг:** Полная observability с метриками и трacing
- **Производительность:** Кэширование, оптимизированные запросы
- **Качество кода:** 80%+ test coverage, документированный API

**Общий timeline:** 8-12 недель
**Команда:** 2-3 разработчика
**Приоритет:** Критические исправления должны быть выполнены в первую очередь