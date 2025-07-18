package server

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
)

// ImportTenderHandler — обработчик импорта полного тендера через POST /api/v1/import-tender.
//
// Этапы работы:
// 1. Парсит входящий JSON в структуру api_models.FullTenderData.
// 2. Валидирует полученные данные методом Validate().
// 3. Передаёт данные в сервисный слой (ImportFullTender) для сохранения тендера и связанных сущностей.
//
// Пример тела запроса:
//
//	{
//	  "tender_id": "строка",
//	  "tender_title": "строка",
//	  "executor": {
//	    "executor_name": "строка",
//	    "executor_phone": "строка",
//	    "executor_date": "строка"
//	  },
//	  "lots": {
//	    "lot_1": {
//	      "lot_title": "строка",
//	      "proposals": {
//	        "contractor_1": {
//	          "title": "строка",
//	          "inn": "строка",
//	          "contractor_items": {
//	            "positions": {
//	              "1": {
//	                "job_title": "строка",
//	                "unit": "строка | null",
//	                "quantity": "число | null",
//	                "total_cost": { "total": "число | null" },
//	                "is_chapter": "boolean",
//	                "chapter_ref": "строка | null"
//	              }
//	            },
//	            "summary": { /* объект summary */ }
//	          },
//	          "additional_info": { /* map[string]string */ }
//	        }
//	      }
//	    }
//	  }
//	}
//
// Пример успешного ответа (201 Created):
//
//	{
//	  "message":   "Тендер успешно импортирован",
//	  "tender_id": "<оригинальный ID тендера>",
//	  "db_id":     "<ID тендера в базе данных>",
//	  "lots_id":   [<список ID лотов>]
//	}
//
// Возможные ошибки:
// - 400 Bad Request: Некорректный JSON или ошибка валидации данных.
// - 500 Internal Server Error: Внутренняя ошибка при обработке
func (s *Server) ImportTenderHandler(c *gin.Context) {
	// Создаем экземпляр логгера с контекстным полем, чтобы легко отслеживать логи именно этого хендлера.
	handlerLogger := s.logger.WithField("handler", "ImportTenderHandler")
	// Информационное сообщение о начале работы.
	handlerLogger.Info("Начало обработки запроса на импорт тендера")

	// Объявляем переменную, в которую будут загружены данные из тела запроса.
	var payload api_models.FullTenderData
	// Пытаемся распарсить (привязать) JSON из тела HTTP-запроса к нашей структуре `payload`.
	if err := c.ShouldBindJSON(&payload); err != nil {
		// Если произошла ошибка (например, невалидный JSON), логируем её...
		handlerLogger.Errorf("Ошибка парсинга JSON: %v", err)
		// ...и возвращаем клиенту ошибку 400 Bad Request с подробностями.
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("некорректный JSON: %w", err)))
		return // Прерываем выполнение функции.
	}

	// Шаг 1: После успешного парсинга, вызываем метод валидации для проверки бизнес-правил.
	if err := payload.Validate(); err != nil {
		// Если данные не прошли валидацию, логируем это как предупреждение (ошибка на стороне клиента).
		s.logger.Warnf("Невалидные данные для импорта тендера: %v", err)
		// Возвращаем клиенту ошибку 400 Bad Request, сообщая о проблеме с данными.
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}

	// Шаг 2: Вызываем метод сервисного слоя для выполнения основной бизнес-логики (сохранение в БД и т.д.).
	// Передаем контекст запроса для контроля времени выполнения и возможной отмены операции.
	newDatabaseID, lotsData, err := s.tenderService.ImportFullTender(c.Request.Context(), &payload)
	// Проверяем, не вернул ли сервисный слой ошибку.
	if err != nil {
		// Ошибка уже залогирована в сервисе, поэтому здесь её повторно не логируем.
		// Возвращаем клиенту ошибку 500 Internal Server Error, т.к. проблема на нашей стороне.
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	s.logger.Infof("Полученные лоты: %v", lotsData) // Логируем ID лотов для отладки.
	// Если все прошло успешно, отправляем клиенту ответ со статусом 201 Created.
	c.JSON(http.StatusCreated, gin.H{
		"message":   "Тендер успешно импортирован", // Сообщение об успехе
		"tender_id": payload.TenderID,              // Возвращаем ID созданного тендера для удобства
		"db_id":     newDatabaseID,                 // Возвращаем ID в базе данных
		"lots_id":   lotsData,                      // Возвращаем ID лотов
	})
}
