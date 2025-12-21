# Создание первого администратора

После настройки базы данных и перед первым запуском сервера необходимо создать пользователя с правами администратора.

## Быстрый старт

```bash
make createadmin
```

## Что происходит

Команда интерактивно запросит:
1. Email администратора
2. Пароль (минимум 8 символов)
3. Подтверждение пароля

## Пример использования

```bash
$ make createadmin
Creating admin user...
Enter admin email: admin@example.com
Enter admin password: 
Confirm admin password: 
✓ Admin user created successfully!
  ID: 1
  Email: admin@example.com
  Role: admin
  Active: true
  Created: 2025-12-21 10:30:45
```

## После создания

Используйте созданный email и пароль для аутентификации через API:

```bash
POST /api/v1/auth/login
Content-Type: application/json

{
  "email": "admin@example.com",
  "password": "your_password"
}
```

Ответ будет содержать `access_token` и `refresh_token` для дальнейшей работы с защищенными эндпоинтами.

## Дополнительная информация

Подробная документация доступна в [cmd/createadmin/README.md](cmd/createadmin/README.md)
