package proxy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/airelay/airelay/internal/encrypt"
	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const keyPrefix = "air_sk_"
const keyCacheTTL = 5 * time.Minute

// HashKey returns the SHA-256 hex hash of an API key.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// GenerateKey creates a new AIRelay API key.
// Returns (fullKey, displayPrefix, hash).
func GenerateKey() (string, string, string) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	full := keyPrefix + hex.EncodeToString(b)
	prefix := full[:16]
	return full, prefix, HashKey(full)
}

// KeyLookup holds the resolved data for an inbound API key.
type KeyLookup struct {
	APIKeyID  uuid.UUID
	ProjectID uuid.UUID
	Provider  models.AIProvider
	PlainKey  string
}

// KeyResolver resolves AIRelay API keys to projects and decrypted provider credentials.
type KeyResolver struct {
	db     *pgxpool.Pool
	redis  *redis.Client
	encKey string
}

func NewKeyResolver(db *pgxpool.Pool, rdb *redis.Client, encKey string) *KeyResolver {
	return &KeyResolver{db: db, redis: rdb, encKey: encKey}
}

// Resolve looks up an inbound key from Redis cache, falling back to Postgres.
func (r *KeyResolver) Resolve(ctx context.Context, inboundKey string, provider models.AIProvider) (*KeyLookup, error) {
	hash := HashKey(inboundKey)
	cacheKey := fmt.Sprintf("keycache:%s:%s", hash, provider)

	if val, err := r.redis.Get(ctx, cacheKey).Result(); err == nil {
		return parseKeyLookup(val)
	}

	lookup, err := r.resolveFromDB(ctx, hash, provider)
	if err != nil {
		return nil, err
	}

	r.redis.Set(ctx, cacheKey, encodeKeyLookup(lookup), keyCacheTTL)
	return lookup, nil
}

func (r *KeyResolver) resolveFromDB(ctx context.Context, keyHash string, provider models.AIProvider) (*KeyLookup, error) {
	var lookup KeyLookup
	var encryptedKey string
	err := r.db.QueryRow(ctx, `
		SELECT ak.id, ak.project_id, pc.provider, pc.encrypted_key
		FROM api_keys ak
		JOIN provider_credentials pc ON pc.project_id = ak.project_id
		    AND pc.provider = $2
		    AND pc.revoked_at IS NULL
		WHERE ak.key_hash = $1
		  AND ak.revoked_at IS NULL`,
		keyHash, string(provider),
	).Scan(&lookup.APIKeyID, &lookup.ProjectID, &lookup.Provider, &encryptedKey)
	if err != nil {
		return nil, fmt.Errorf("key not found or no credential for provider: %w", err)
	}
	plain, err := encrypt.Decrypt(r.encKey, encryptedKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt credential: %w", err)
	}
	lookup.PlainKey = plain
	return &lookup, nil
}

// encodeKeyLookup serialises a KeyLookup for Redis caching.
// PlainKey may contain arbitrary characters so we hex encode the last field.
func encodeKeyLookup(l *KeyLookup) string {
	encodedKey := hex.EncodeToString([]byte(l.PlainKey))
	return fmt.Sprintf("%s|%s|%s|%s", l.APIKeyID, l.ProjectID, l.Provider, encodedKey)
}

func parseKeyLookup(s string) (*KeyLookup, error) {
	parts := strings.SplitN(s, "|", 4)
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid cache format")
	}
	akID, err := uuid.Parse(parts[0])
	if err != nil {
		return nil, fmt.Errorf("parse api key id: %w", err)
	}
	pID, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, fmt.Errorf("parse project id: %w", err)
	}
	plainBytes, err := hex.DecodeString(parts[3])
	if err != nil {
		return nil, fmt.Errorf("decode plain key: %w", err)
	}
	return &KeyLookup{
		APIKeyID:  akID,
		ProjectID: pID,
		Provider:  models.AIProvider(parts[2]),
		PlainKey:  string(plainBytes),
	}, nil
}
