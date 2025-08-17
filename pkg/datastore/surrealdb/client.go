package surrealdb

import (
	"context"
	"fmt"

	"github.com/Ingenimax/agent-sdk-go/pkg/config"
	"github.com/surrealdb/surrealdb.go"
)

// New creates a new SurrealDB client.
func New(cfg *config.SurrealDBConfig) (*surrealdb.DB, error) {
	if cfg == nil {
		return nil, fmt.Errorf("SurrealDB config cannot be nil")
	}

	db, err := surrealdb.FromEndpointURLString(context.Background(), cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to create surrealdb client: %w", err)
	}

	// Sign in to the database
	if cfg.Username != "" && cfg.Password != "" {
		_, err = db.SignIn(context.Background(), map[string]interface{}{
			"user": cfg.Username,
			"pass": cfg.Password,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to sign in to surrealdb: %w", err)
		}
	}

	// Select namespace and database
	if cfg.NS != "" && cfg.DB != "" {
		err = db.Use(context.Background(), cfg.NS, cfg.DB)
		if err != nil {
			return nil, fmt.Errorf("failed to use ns/db: %w", err)
		}
	}

	return db, nil
}
