package server

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services"
)

type Server struct {
	store  db.Store
	router *gin.Engine
	logger *logging.Logger
	tenderService *services.TenderProcessingService
}

func NewServer(store db.Store, logger *logging.Logger, tenderService *services.TenderProcessingService) *Server {
	server := &Server{store: store, logger: logger, tenderService: tenderService}
	router := gin.Default()

	// Настройка CORS
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"}, // Укажите адрес фронтенда
		AllowMethods:     []string{"GET", "POST", "OPTIONS", "PUT"}, // Методы, которые разрешены
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
        v1.GET("/tenders", server.listTendersHandler)
		v1.GET("/tenders/:id", server.getTenderDetailsHandler)
		v1.GET("/tenders/:id/proposals", server.listProposalsHandler)

		v1.GET("/lots/:id/proposals", server.listProposalsForLotHandler)
		
		v1.GET("/tender-types", server.listTenderTypesHandler)
		v1.POST("/tender-types", server.createTenderTypeHandler)

		v1.PUT("/tender-types/:id", server.updateTenderTypeHandler)
        v1.DELETE("/tender-types/:id", server.deleteTenderTypeHandler)
		v1.GET("/tender-types/:type_id/chapters", server.listChaptersByTypeHandler)

		v1.GET("/tender-chapters", server.listTenderChaptersHandler)
		v1.POST("/tender-chapters", server.createTenderChapterHandler)

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