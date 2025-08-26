package server

import (
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/config"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

type Server struct {
	store         db.Store
	router        *gin.Engine
	logger        *logging.Logger
	tenderService *services.TenderProcessingService
	httpClient    *http.Client
	config        *config.Config
}

func NewServer(store db.Store, logger *logging.Logger, tenderService *services.TenderProcessingService, cfg *config.Config) *Server {
	httpClient := &http.Client{
		Timeout: time.Minute * 5,
	}

	server := &Server{
		store:         store,
		logger:        logger,
		tenderService: tenderService,
		httpClient:    httpClient,
		config:        cfg,
	}
	router := gin.Default()

	// Настройка CORS
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://local-api.dev:5173"}, // Укажите адрес фронтенда
		AllowMethods:     []string{"GET", "POST", "OPTIONS", "PUT", "PATCH"},             // Методы, которые разрешены
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true, // Если вы используете cookies или авторизацию
	}))

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
	}
	// ------------------------------------------------

	server.router = router
	return server
}

func (s *Server) Start(address string) error {
	return s.router.Run(address)
}

func errorResponse(err error) gin.H {
	return gin.H{"error": err.Error()}
}
