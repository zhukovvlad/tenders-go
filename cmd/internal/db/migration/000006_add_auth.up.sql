-- =====================================================================================
-- 000006_add_auth.up.sql
-- Описание:
-- Добавляет базовую авторизацию (локальные пользователи + refresh-сессии).
-- Вариант A: Go выдает access JWT (короткий) + refresh token (длинный, ротируемый).
-- Refresh token хранится в БД только в виде HASH.
-- =====================================================================================

-- === 1) Пользователи ===
CREATE TABLE "users" (
  "id" BIGSERIAL PRIMARY KEY,

  "email" varchar(255) NOT NULL,
  "password_hash" text NOT NULL,

  -- Минимальная RBAC-модель на старте (можно расширить таблицами roles позже)
  "role" varchar(50) NOT NULL DEFAULT 'operator',
  "is_active" boolean NOT NULL DEFAULT true,

  "last_login_at" timestamptz,

  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now()),

  CONSTRAINT "uq_users_email" UNIQUE ("email"),
  CONSTRAINT "chk_users_email_normalized" CHECK ("email" = LOWER(BTRIM("email"))),
  CONSTRAINT "chk_users_role" CHECK ("role" IN ('admin', 'operator', 'viewer'))
);

COMMENT ON TABLE "users"
IS 'Локальные пользователи приложения (вариант A). Пароли храним только в виде password_hash.';

COMMENT ON COLUMN "users"."email" IS 'Email пользователя (уникальный логин). ВАЖНО: всегда нормализовать в Go через strings.ToLower(strings.TrimSpace(email)) перед INSERT/UPDATE.';
COMMENT ON COLUMN "users"."password_hash" IS 'Хеш пароля (bcrypt/argon2).';
COMMENT ON COLUMN "users"."role" IS 'Роль доступа: admin/operator/viewer.';
COMMENT ON COLUMN "users"."is_active" IS 'Флаг активности. При false — запрет логина.';
COMMENT ON COLUMN "users"."last_login_at" IS 'Последний успешный вход.';


-- === 2) Refresh-сессии (храним HASH refresh токена) ===
CREATE TABLE "user_sessions" (
  "id" BIGSERIAL PRIMARY KEY,

  "user_id" BIGINT NOT NULL,
  "refresh_token_hash" char(64) NOT NULL,

  -- Метаданные (полезно для безопасности/аудита)
  "user_agent" text,
  "ip_address" inet,

  -- Жизненный цикл
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "expires_at" timestamptz NOT NULL,
  "revoked_at" timestamptz,

  CONSTRAINT "fk_user_sessions_user"
    FOREIGN KEY ("user_id")
    REFERENCES "users"("id")
    ON DELETE CASCADE,

  CONSTRAINT "uq_user_sessions_refresh_hash" UNIQUE ("refresh_token_hash"),
  CONSTRAINT "chk_user_sessions_expires_at" CHECK ("expires_at" > "created_at"),
  CONSTRAINT "chk_user_sessions_refresh_hash_hex" CHECK ("refresh_token_hash" ~ '^[0-9a-f]{64}$')
);

COMMENT ON TABLE "user_sessions"
IS 'Refresh-сессии. refresh_token хранится только в виде hash. Использовать ротацию refresh при обновлении.';

COMMENT ON COLUMN "user_sessions"."refresh_token_hash"
IS 'Хеш refresh токена (SHA-256 hex, 64 символа). Сам токен в БД не хранить.';

COMMENT ON COLUMN "user_sessions"."revoked_at"
IS 'Если заполнено — сессия отозвана (logout/компрометация/ротация).';

-- Индексы для типовых операций
CREATE INDEX IF NOT EXISTS "idx_user_sessions_user_id" ON "user_sessions" ("user_id");
CREATE INDEX IF NOT EXISTS "idx_user_sessions_expires_at" ON "user_sessions" ("expires_at");

-- Составной индекс для активных сессий (основной рабочий индекс)
CREATE INDEX IF NOT EXISTS "idx_user_sessions_user_active" 
  ON "user_sessions" ("user_id", "expires_at") 
  WHERE "revoked_at" IS NULL;
