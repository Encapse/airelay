package models

import (
	"time"

	"github.com/google/uuid"
)

// UserPlan represents the subscription tier of a user.
type UserPlan string

const (
	PlanFree UserPlan = "free"
	PlanPro  UserPlan = "pro"
	PlanTeam UserPlan = "team"
)

// AIProvider identifies an upstream AI API provider.
type AIProvider string

const (
	ProviderOpenAI    AIProvider = "openai"
	ProviderAnthropic AIProvider = "anthropic"
	ProviderGoogle    AIProvider = "google"
)

// BudgetPeriod defines the time window for a budget.
type BudgetPeriod string

const (
	PeriodDaily   BudgetPeriod = "daily"
	PeriodMonthly BudgetPeriod = "monthly"
)

type User struct {
	ID               uuid.UUID
	Email            string
	PasswordHash     string
	Plan             UserPlan
	StripeCustomerID *string
	CreatedAt        time.Time
}

type Project struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Name       string
	Slug       string
	CreatedAt  time.Time
	ArchivedAt *time.Time
}

type APIKey struct {
	ID         uuid.UUID
	ProjectID  uuid.UUID
	KeyHash    string
	KeyPrefix  string
	Name       string
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

type ProviderCredential struct {
	ID           uuid.UUID
	ProjectID    uuid.UUID
	Provider     AIProvider
	EncryptedKey string
	RevokedAt    *time.Time
	CreatedAt    time.Time
}

type Budget struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	AmountUSD float64
	Period    BudgetPeriod
	HardLimit bool
	CreatedAt time.Time
}

type AlertThreshold struct {
	ID               uuid.UUID
	BudgetID         uuid.UUID
	ThresholdPct     int
	NotifyEmail      bool
	NotifyWebhookURL *string
	LastFiredAt      *time.Time
}

type UsageEvent struct {
	ID               uuid.UUID
	ProjectID        uuid.UUID
	APIKeyID         uuid.UUID
	Provider         string
	Model            string
	PromptTokens     int
	CompletionTokens int
	CostUSD          *float64
	DurationMS       int
	StatusCode       int
	Metadata         map[string]any
	FailOpen         bool
	CreatedAt        time.Time
}

type ModelPricing struct {
	Provider        string
	Model           string
	InputCostPer1k  float64
	OutputCostPer1k float64
	SyncedFrom      string
	ManualOverride  bool
	LastSyncedAt    time.Time
}
