// Package deps exists solely to anchor module dependencies before any
// application code is written.  It is not imported by any production code.
package deps

import (
	_ "github.com/golang-jwt/jwt/v5"
	_ "github.com/google/uuid"
	_ "github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/joho/godotenv"
	_ "github.com/pressly/goose/v3"
	_ "github.com/redis/go-redis/v9"
	_ "github.com/stretchr/testify/assert"
	_ "golang.org/x/crypto/bcrypt"
)
