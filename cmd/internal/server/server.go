package server

import (
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/config"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
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

	server := &Server{
		store:           store,
		logger:          logger,
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
		// В режиме отладки разрешаем все origins
		corsConfig.AllowAllOrigins = true
		corsConfig.AllowMethods = []string{"GET", "POST", "OPTIONS", "PUT", "PATCH", "DELETE"}
		corsConfig.AllowHeaders = []string{"Origin", "Content-Type", "Authorization", "Accept", "X-Requested-With"}
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

	// --- ДОБАВЛЯЕМ НОВЫЙ РОУТ ДЛЯ СПИСКА ТЕНДЕРОВ ---
	v1 := router.Group("/api/v1")
	{
		v1.POST("/upload-tender", server.ProxyUploadHandler)
		v1.GET("/tasks/:task_id/status", server.GetTaskStatusHandler)

		v1.GET("/tenders", server.listTendersHandler)
		v1.GET("/tenders/:id", server.getTenderDetailsHandler)
		v1.GET("/tenders/:id/proposals", server.listProposalsHandler)

		// AI Results endpoint для Python сервиса (упрощенный, только lot_id)
		v1.POST("/lots/:lot_id/ai-results", server.SimpleLotAIResultsHandler)

		// Используем PATCH для частичного обновления всего ресурса 'tenders'
		v1.PATCH("/tenders/:id", server.patchTenderHandler)

		v1.GET("/lots/:id/proposals", server.listProposalsForLotHandler)
		v1.PATCH("/lots/:id/key-parameters", server.patchLotKeyParametersHandler)

		v1.GET("/tender-types", server.listTenderTypesHandler)
		v1.POST("/tender-types", server.createTenderTypeHandler)

		v1.PUT("/tender-types/:id", server.updateTenderTypeHandler)
		v1.DELETE("/tender-types/:id", server.deleteTenderTypeHandler)
		v1.GET("/tender-types/:type_id/chapters", server.listChaptersByTypeHandler)

		v1.GET("/tender-chapters", server.listTenderChaptersHandler)
		v1.POST("/tender-chapters", server.createTenderChapterHandler)
		v1.GET("/tender-chapters/:chapter_id/categories", server.listCategoriesByChapterHandler)

		// --- ДОБАВЛЯЕМ НОВЫЕ РОУТЫ ДЛЯ UPDATE И DELETE ---
		v1.PUT("/tender-chapters/:id", server.updateTenderChapterHandler)
		v1.DELETE("/tender-chapters/:id", server.deleteTenderChapterHandler)

		// --- НОВЫЕ РОУТЫ ДЛЯ КАТЕГОРИЙ ---
		v1.GET("/tender-categories", server.listTenderCategoriesHandler)
		v1.POST("/tender-categories", server.createTenderCategoryHandler)
		v1.PUT("/tender-categories/:id", server.updateTenderCategoryHandler)
		v1.DELETE("/tender-categories/:id", server.deleteTenderCategoryHandler)

		// RAG-воркфлоу
		// 1. "Дай мне работу" (для Процесса 2)
		v1.GET("/positions/unmatched", server.UnmatchedPositionsHandler)

		// 2. "Прими результат" (для Процесса 2)
		v1.POST("/positions/match", server.MatchPositionHandler)

		// 3. "Дай каталог на индексацию" (для Процесса 3)
		v1.GET("/catalog/unindexed", server.UnindexedCatalogItemsHandler)

		// 4. "Я проиндексировал" (для Процесса 3)
		v1.POST("/catalog/indexed", server.CatalogIndexedHandler)

		// 5. "Предложи слияние" (для Процесса 3)
		v1.POST("/merges/suggest", server.SuggestMergeHandler)

		// 6. "Дай весь активный каталог" (для Процесса 3, Часть Б)
		v1.GET("/catalog/active", server.ActiveCatalogItemsHandler)
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
