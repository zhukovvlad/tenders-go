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
		if len(cfg.CORS.AllowedOrigins) > 0 {
			corsConfig.AllowOrigins = cfg.CORS.AllowedOrigins
		} else {
			// В production CORS origins должны быть явно настроены
			logger := logging.GetLogger()
			logger.Warn("CORS allowed_origins not configured in production - using restrictive default")
			corsConfig.AllowOrigins = []string{} // No origins allowed
		}
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

		// Приватные роуты (требуют аутентификацию)
		protected := v1.Group("/")
		protected.Use(AuthMiddleware(server.config, server.store))
		{
			// Информация о текущем пользователе
			protected.GET("/auth/me", server.meHandler)

			protected.POST("/upload-tender", server.ProxyUploadHandler)
			protected.GET("/tasks/:task_id/status", server.GetTaskStatusHandler)

			protected.GET("/tenders", server.listTendersHandler)
			protected.GET("/tenders/:id", server.getTenderDetailsHandler)
			protected.GET("/tenders/:id/proposals", server.listProposalsHandler)

			// AI Results endpoint для Python сервиса (упрощенный, только lot_id)
			protected.POST("/lots/:lot_id/ai-results", server.SimpleLotAIResultsHandler)

			// Используем PATCH для частичного обновления всего ресурса 'tenders'
			protected.PATCH("/tenders/:id", server.patchTenderHandler)

			protected.GET("/lots/:id/proposals", server.listProposalsForLotHandler)
			protected.PATCH("/lots/:id/key-parameters", server.patchLotKeyParametersHandler)

			protected.GET("/tender-types", server.listTenderTypesHandler)
			protected.POST("/tender-types", server.createTenderTypeHandler)

			protected.PUT("/tender-types/:id", server.updateTenderTypeHandler)
			protected.DELETE("/tender-types/:id", server.deleteTenderTypeHandler)
			protected.GET("/tender-types/:type_id/chapters", server.listChaptersByTypeHandler)

			protected.GET("/tender-chapters", server.listTenderChaptersHandler)
			protected.POST("/tender-chapters", server.createTenderChapterHandler)
			protected.GET("/tender-chapters/:chapter_id/categories", server.listCategoriesByChapterHandler)

			// --- ДОБАВЛЯЕМ НОВЫЕ РОУТЫ ДЛЯ UPDATE И DELETE ---
			protected.PUT("/tender-chapters/:id", server.updateTenderChapterHandler)
			protected.DELETE("/tender-chapters/:id", server.deleteTenderChapterHandler)

			// --- НОВЫЕ РОУТЫ ДЛЯ КАТЕГОРИЙ ---
			protected.GET("/tender-categories", server.listTenderCategoriesHandler)
			protected.POST("/tender-categories", server.createTenderCategoryHandler)
			protected.PUT("/tender-categories/:id", server.updateTenderCategoryHandler)
			protected.DELETE("/tender-categories/:id", server.deleteTenderCategoryHandler)

			// RAG-воркфлоу
			// 1. "Дай мне работу" (для Процесса 2)
			protected.GET("/positions/unmatched", server.UnmatchedPositionsHandler)

			// 2. "Прими результат" (для Процесса 2)
			protected.POST("/positions/match", server.MatchPositionHandler)

			// 3. "Дай каталог на индексацию" (для Процесса 3)
			protected.GET("/catalog/unindexed", server.UnindexedCatalogItemsHandler)

			// 4. "Я проиндексировал" (для Процесса 3)
			protected.POST("/catalog/indexed", server.CatalogIndexedHandler)

			// 5. "Предложи слияние" (для Процесса 3)
			protected.POST("/merges/suggest", server.SuggestMergeHandler)

			// 6. "Дай весь активный каталог" (для Процесса 3, Часть Б)
			protected.GET("/catalog/active", server.ActiveCatalogItemsHandler)
		}

		// Админские роуты (требуют роль admin)
		admin := protected.Group("/admin")
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
