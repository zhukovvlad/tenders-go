package server

import (
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/config"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/auth"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/catalog"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/importer"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/lot"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/matching"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

type Server struct {
	store           db.Store
	router          *gin.Engine
	logger          *logging.Logger
	authService     *auth.Service
	tenderService   *importer.TenderImportService
	catalogService  *catalog.CatalogService
	lotService      *lot.LotService
	matchingService *matching.MatchingService
	httpClient      *http.Client
	config          *config.Config
}

func NewServer(
	store db.Store,
	logger *logging.Logger,
	tenderService *importer.TenderImportService,
	catalogService *catalog.CatalogService,
	lotService *lot.LotService,
	matchingService *matching.MatchingService,
	cfg *config.Config,
) *Server {
	httpClient := &http.Client{
		Timeout: time.Minute * 5,
	}

	authService := auth.NewService(store, cfg)

	server := &Server{
		store:           store,
		logger:          logger,
		authService:     authService,
		tenderService:   tenderService,
		catalogService:  catalogService,
		lotService:      lotService,
		matchingService: matchingService,
		httpClient:      httpClient,
		config:          cfg,
	}
	router := gin.Default()

	// Настройка CORS
	corsConfig := cors.DefaultConfig()
	if cfg.IsDebug != nil && *cfg.IsDebug {
		// В режиме отладки - локальные origins + credentials для cookie-based auth
		corsConfig.AllowOrigins = []string{
			"http://localhost:5173",
			"http://127.0.0.1:5173",
			"http://local-api.dev:5173",
		}
		corsConfig.AllowMethods = []string{"GET", "POST", "OPTIONS", "PUT", "PATCH", "DELETE"}
		corsConfig.AllowHeaders = []string{"Origin", "Content-Type", "Authorization", "Accept", "X-Requested-With"}
		corsConfig.AllowCredentials = true
	} else {
		// В production режиме - строгие настройки
		corsConfig.AllowOrigins = []string{"http://localhost:5173", "http://local-api.dev:5173"}
		corsConfig.AllowMethods = []string{"GET", "POST", "OPTIONS", "PUT", "PATCH", "DELETE"}
		corsConfig.AllowHeaders = []string{"Origin", "Content-Type", "Authorization"}
		corsConfig.AllowCredentials = true
	}
	corsConfig.ExposeHeaders = []string{"Content-Length"}
	router.Use(cors.New(corsConfig))

	router.GET("/home", server.HomeHandler)
	router.GET("/api/stats", server.getStatsHandler)
	router.POST("/api/v1/import-tender", server.ImportTenderHandler)

	// --- API V1 ---
	v1 := router.Group("/api/v1")
	{
		// Публичные auth-роуты
		v1.POST("/auth/login", server.loginHandler)
		v1.POST("/auth/refresh", server.refreshHandler)
		v1.POST("/auth/logout", server.logoutHandler)
		v1.GET("/auth/me", server.meHandler)

		// Приватные роуты (требуют аутентификацию)
		auth := v1.Group("/")
		auth.Use(AuthMiddleware(server.config, server.store))
		{
			auth.POST("/upload-tender", server.ProxyUploadHandler)
			auth.GET("/tasks/:task_id/status", server.GetTaskStatusHandler)

			auth.GET("/tenders", server.listTendersHandler)
			auth.GET("/tenders/:id", server.getTenderDetailsHandler)
			auth.GET("/tenders/:id/proposals", server.listProposalsHandler)

			// AI Results endpoint для Python сервиса (упрощенный, только lot_id)
			auth.POST("/lots/:lot_id/ai-results", server.SimpleLotAIResultsHandler)

			// Используем PATCH для частичного обновления всего ресурса 'tenders'
			auth.PATCH("/tenders/:id", server.patchTenderHandler)

			auth.GET("/lots/:id/proposals", server.listProposalsForLotHandler)
			auth.PATCH("/lots/:id/key-parameters", server.patchLotKeyParametersHandler)

			auth.GET("/tender-types", server.listTenderTypesHandler)
			auth.POST("/tender-types", server.createTenderTypeHandler)

			auth.PUT("/tender-types/:id", server.updateTenderTypeHandler)
			auth.DELETE("/tender-types/:id", server.deleteTenderTypeHandler)
			auth.GET("/tender-types/:type_id/chapters", server.listChaptersByTypeHandler)

			auth.GET("/tender-chapters", server.listTenderChaptersHandler)
			auth.POST("/tender-chapters", server.createTenderChapterHandler)
			auth.GET("/tender-chapters/:chapter_id/categories", server.listCategoriesByChapterHandler)

			// --- ДОБАВЛЯЕМ НОВЫЕ РОУТЫ ДЛЯ UPDATE И DELETE ---
			auth.PUT("/tender-chapters/:id", server.updateTenderChapterHandler)
			auth.DELETE("/tender-chapters/:id", server.deleteTenderChapterHandler)

			// --- НОВЫЕ РОУТЫ ДЛЯ КАТЕГОРИЙ ---
			auth.GET("/tender-categories", server.listTenderCategoriesHandler)
			auth.POST("/tender-categories", server.createTenderCategoryHandler)
			auth.PUT("/tender-categories/:id", server.updateTenderCategoryHandler)
			auth.DELETE("/tender-categories/:id", server.deleteTenderCategoryHandler)

			// RAG-воркфлоу
			// 1. "Дай мне работу" (для Процесса 2)
			auth.GET("/positions/unmatched", server.UnmatchedPositionsHandler)

			// 2. "Прими результат" (для Процесса 2)
			auth.POST("/positions/match", server.MatchPositionHandler)

			// 3. "Дай каталог на индексацию" (для Процесса 3)
			auth.GET("/catalog/unindexed", server.UnindexedCatalogItemsHandler)

			// 4. "Я проиндексировал" (для Процесса 3)
			auth.POST("/catalog/indexed", server.CatalogIndexedHandler)

			// 5. "Предложи слияние" (для Процесса 3)
			auth.POST("/merges/suggest", server.SuggestMergeHandler)

			// 6. "Дай весь активный каталог" (для Процесса 3, Часть Б)
			auth.GET("/catalog/active", server.ActiveCatalogItemsHandler)
		}

		// Админские роуты (требуют роль admin)
		admin := auth.Group("/admin")
		admin.Use(RequireRole("admin"))
		{
			admin.GET("/users", server.listUsersHandler)
			admin.PATCH("/users/:id/role", server.updateUserRoleHandler)
		}
	}

	server.router = router
	return server
}

func (s *Server) Start(address string) error {
	return s.router.Run(address)
}

func errorResponse(err error) gin.H {
	return gin.H{"error": err.Error()}
}
