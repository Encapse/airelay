package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/airelay/airelay/internal/config"
	"github.com/airelay/airelay/internal/db"
	"github.com/airelay/airelay/internal/encrypt"
	"github.com/airelay/airelay/proxy"
	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	pool, err := db.Connect(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()
	ctx := context.Background()

	// User
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	var userID string
	err = pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, plan)
		 VALUES ($1, $2, 'pro')
		 ON CONFLICT (email) DO UPDATE SET plan='pro'
		 RETURNING id`,
		"dev@airelay.dev", string(hash),
	).Scan(&userID)
	if err != nil {
		log.Fatalf("create user: %v", err)
	}

	// Project
	var projectID string
	err = pool.QueryRow(ctx,
		`INSERT INTO projects (user_id, name, slug)
		 VALUES ($1, 'Dev Project', 'dev-project')
		 ON CONFLICT (slug) DO UPDATE SET name='Dev Project'
		 RETURNING id`,
		userID,
	).Scan(&projectID)
	if err != nil {
		log.Fatalf("create project: %v", err)
	}

	// API key — always recreate so the printed key is guaranteed to work on re-runs
	fullKey, prefix, keyHash := proxy.GenerateKey()
	pool.Exec(ctx, `DELETE FROM api_keys WHERE project_id=$1 AND name='dev-key'`, projectID)
	_, err = pool.Exec(ctx,
		`INSERT INTO api_keys (project_id, key_hash, key_prefix, name)
		 VALUES ($1, $2, $3, 'dev-key')`,
		projectID, keyHash, prefix,
	)
	if err != nil {
		log.Fatalf("create api key: %v", err)
	}

	// Provider credentials (OpenAI)
	if cfg.OpenAIKey != "" {
		encKey, err := encrypt.Encrypt(cfg.CredentialEncryptionKey, cfg.OpenAIKey)
		if err != nil {
			log.Fatalf("encrypt openai key: %v", err)
		}
		_, err = pool.Exec(ctx,
			`INSERT INTO provider_credentials (project_id, provider, encrypted_key)
			 VALUES ($1, 'openai', $2)
			 ON CONFLICT (project_id, provider) WHERE revoked_at IS NULL DO UPDATE SET encrypted_key=$2`,
			projectID, encKey,
		)
		if err != nil {
			log.Fatalf("create openai credential: %v", err)
		}
	}

	// Provider credentials (Anthropic)
	if cfg.AnthropicKey != "" {
		encKey, err := encrypt.Encrypt(cfg.CredentialEncryptionKey, cfg.AnthropicKey)
		if err != nil {
			log.Fatalf("encrypt anthropic key: %v", err)
		}
		pool.Exec(ctx,
			`INSERT INTO provider_credentials (project_id, provider, encrypted_key)
			 VALUES ($1, 'anthropic', $2)
			 ON CONFLICT (project_id, provider) WHERE revoked_at IS NULL DO UPDATE SET encrypted_key=$2`,
			projectID, encKey,
		)
	}

	// Monthly budget: $10 hard limit
	pool.Exec(ctx,
		`INSERT INTO budgets (project_id, amount_usd, period, hard_limit)
		 VALUES ($1, 10.00, 'monthly', true)
		 ON CONFLICT (project_id, period) DO NOTHING`,
		projectID,
	)

	fmt.Printf("Seed complete\n\n")
	fmt.Printf("  Email:      dev@airelay.dev\n")
	fmt.Printf("  Password:   password123\n")
	fmt.Printf("  Project ID: %s\n", projectID)
	fmt.Printf("  API Key:    %s\n\n", fullKey)

	fmt.Printf("Test the proxy:\n")
	fmt.Printf("  export OPENAI_BASE_URL=http://localhost:8081/proxy/openai\n")
	fmt.Printf("  curl http://localhost:8081/proxy/openai/v1/models \\\n")
	fmt.Printf("    -H 'Authorization: Bearer %s'\n\n", fullKey)

	if cfg.OpenAIKey == "" {
		fmt.Fprintln(os.Stderr, "OPENAI_API_KEY not set in .env - OpenAI calls won't work")
	}
}
