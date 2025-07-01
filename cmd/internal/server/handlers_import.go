package server

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
)

// ImportTenderHandler обрабатывает HTTP-запрос на импорт полного набора данных о тендере.
//
// Он выполняет следующие шаги:
// 1. Декодирует JSON-тело запроса в структуру api_models.FullTenderData.
// 2. Валидирует полученные данные с помощью метода Validate() самой структуры.
// 3. Вызывает метод сервисного слоя ImportFullTender для выполнения основной бизнес-логики.
//
// Эндпоинт: POST /api/v1/import-tender
//
// Тело запроса (Body):
//
//	{
//	    "tender_id": "string",
//	    "tender_title": "string",
//	    "executor": {
//	        "executor_name": "string",
//	        "executor_phone": "string",
//	        "executor_date": "string"
//	    },
//	    "lots": {
//	        "lot_1": { // Ключи для лотов могут быть динамическими (например, lot_1, lot_2)
//	            "lot_title": "string",
//	            "proposals": {
//	                "contractor_1": { // Ключи для подрядчиков также динамические
//	                    "title": "string",
//	                    "inn": "string",
//	                    "contractor_items": {
//	                        "positions": {
//	                            "1": { // Ключ - порядковый номер позиции
//	                                "job_title": "string",
//	                                "unit": "string | null",
//	                                "quantity": "number | null",
//	                                "total_cost": { "total": "number | null" },
//	                                "is_chapter": "boolean",
//	                                "chapter_ref": "string | null"
//	                            }
//	                            // ... другие позиции ...
//	                        },
//	                        "summary": { /* Объект с итоговыми суммами */ }
//	                    },
//	                    "additional_info": { /* Карта [string]string с доп. информацией */ }
//	                }
//	                // ... другие подрядчики ...
//	            }
//	        }
//	    }
//	}
//
// Успешный ответ (Success Response):
// - Код: 201 Created
// - Тело:
//   {
//     "message": "Тендер успешно импортирован",
//     "tender_id": "<ID импортированного тендера>"
//   }
//
// Ошибки (Error Responses):
// - Код: 400 Bad Request - в случае некорректного формата JSON или провала валидации данных.
// - Код: 500 Internal Server Error - в случае внутренней ошибки при обработке и сохранении данных.
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
	err := s.tenderService.ImportFullTender(c.Request.Context(), &payload)
	// Проверяем, не вернул ли сервисный слой ошибку.
	if err != nil {
		// Ошибка уже залогирована в сервисе, поэтому здесь её повторно не логируем.
		// Возвращаем клиенту ошибку 500 Internal Server Error, т.к. проблема на нашей стороне.
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// Если все прошло успешно, отправляем клиенту ответ со статусом 201 Created.
	c.JSON(http.StatusCreated, gin.H{
		"message":   "Тендер успешно импортирован", // Сообщение об успехе.
		"tender_id": payload.TenderID,             // Возвращаем ID созданного тендера для удобства.
	})
}