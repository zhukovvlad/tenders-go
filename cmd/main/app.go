package main

import (
	"database/sql"
	"fmt"

	"github.com/joho/godotenv"
	"github.com/zhukovvlad/tenders-go/cmd/internal/config"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/server"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"

	_ "github.com/lib/pq"
)

const (
	dbDriver = "postgres"
	dbSource = "postgres://root:secret@localhost:5435/tendersdb?sslmode=disable"
)

func main() {
	logger := logging.GetLogger()
	logger.Info("Starting Tenders API...")

	err := godotenv.Load()
	if err != nil {
		logger.Fatalf("error loading .env file: %v", err)
	}

	cfg := config.GetConfig()

	conn, err := sql.Open(dbDriver, dbSource)
	if err != nil {
		logger.Fatalf("error connecting to database: %v", err)
	}
	defer conn.Close()

	if err = conn.Ping(); err != nil {
		logger.Fatalf("error pinging database: %v", err)
	}

	logger.Info("Database connection established")


	store := db.NewStore(conn)
	tenderService := services.NewTenderProcessingService(store, logger)
	server := server.NewServer(store, logger, tenderService, cfg)

	serverAddress := fmt.Sprintf("%s:%s", cfg.Listen.BindIP, cfg.Listen.Port)
	logger.Infof("Starting server on %s", serverAddress)

	err = server.Start(serverAddress)
	if err != nil {
		logger.Fatalf("error starting server: %v", err)
	}
}