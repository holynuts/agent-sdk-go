package surrealdb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/surrealdb/surrealdb.go"

	"github.com/Ingenimax/agent-sdk-go/pkg/interfaces"
	"github.com/Ingenimax/agent-sdk-go/pkg/multitenancy"
)

// DataStore implements the DataStore interface for SurrealDB.
type DataStore struct {
	db *surrealdb.DB
}

// NewDataStore creates a new SurrealDB DataStore.
func NewDataStore(db *surrealdb.DB) *DataStore {
	return &DataStore{db: db}
}

// Collection returns a reference to a specific collection/table.
func (s *DataStore) Collection(name string) interfaces.CollectionRef {
	return &Collection{
		db:   s.db,
		name: name,
	}
}

// Transaction is not yet supported by this implementation.
func (s *DataStore) Transaction(ctx context.Context, fn func(tx interfaces.Transaction) error) error {
	return fmt.Errorf("transactions are not yet supported by this SurrealDB implementation")
}

// Close is a no-op as the driver manages the connection lifecycle.
func (s *DataStore) Close() error {
	return nil
}

// Collection represents a reference to a collection/table in SurrealDB.
type Collection struct {
	db   *surrealdb.DB
	name string
}

// Insert inserts a document into the collection.
func (c *Collection) Insert(ctx context.Context, data map[string]interface{}) (string, error) {
	orgID, err := multitenancy.GetOrgID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get organization ID: %w", err)
	}
	data["org_id"] = orgID
	data["created_at"] = time.Now().UTC()

	id, ok := data["id"].(string)
	if !ok || id == "" {
		id = uuid.New().String()
		data["id"] = id
	}

	thing := fmt.Sprintf("%s:%s", c.name, id)
	_, err = surrealdb.Create[any](ctx, c.db, thing, data)
	if err != nil {
		return "", fmt.Errorf("failed to insert document in SurrealDB: %w", err)
	}

	return id, nil
}

// Get retrieves a document by ID.
func (c *Collection) Get(ctx context.Context, id string) (map[string]interface{}, error) {
	orgID, err := multitenancy.GetOrgID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization ID: %w", err)
	}

	query := fmt.Sprintf("SELECT * FROM %s WHERE id = $id AND org_id = $org_id LIMIT 1", c.name)
	params := map[string]interface{}{
		"id":     id,
		"org_id": orgID,
	}

	results, err := runQueryForMaps(ctx, c.db, query, params)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("document not found")
	}

	return results[0], nil
}

// Update updates a document by ID.
func (c *Collection) Update(ctx context.Context, id string, data map[string]interface{}) error {
	orgID, err := multitenancy.GetOrgID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get organization ID: %w", err)
	}
	data["updated_at"] = time.Now().UTC()

	var setClauses []string
	params := map[string]interface{}{
		"id":     id,
		"org_id": orgID,
	}
	i := 0
	for k, v := range data {
		paramName := fmt.Sprintf("p%d", i)
		setClauses = append(setClauses, fmt.Sprintf("%s = $%s", k, paramName))
		params[paramName] = v
		i++
	}

	query := fmt.Sprintf("UPDATE %s SET %s WHERE id = $id AND org_id = $org_id", c.name, strings.Join(setClauses, ", "))

	_, err = runQueryForAny(ctx, c.db, query, params)
	return err
}

// Delete deletes a document by ID.
func (c *Collection) Delete(ctx context.Context, id string) error {
	orgID, err := multitenancy.GetOrgID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get organization ID: %w", err)
	}

	query := fmt.Sprintf("DELETE %s WHERE id = $id AND org_id = $org_id", c.name)
	params := map[string]interface{}{
		"id":     id,
		"org_id": orgID,
	}

	_, err = runQueryForAny(ctx, c.db, query, params)
	return err
}

// Query queries documents in the collection.
func (c *Collection) Query(ctx context.Context, filter map[string]interface{}, options ...interfaces.QueryOption) ([]map[string]interface{}, error) {
	orgID, err := multitenancy.GetOrgID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization ID: %w", err)
	}

	opts := &interfaces.QueryOptions{}
	for _, option := range options {
		option(opts)
	}

	whereClauses := []string{"org_id = $org_id"}
	params := map[string]interface{}{"org_id": orgID}

	i := 0
	for k, v := range filter {
		paramName := fmt.Sprintf("p%d", i)
		whereClauses = append(whereClauses, fmt.Sprintf("%s = $%s", k, paramName))
		params[paramName] = v
		i++
	}

	query := fmt.Sprintf("SELECT * FROM %s", c.name)
	if len(whereClauses) > 0 {
		query += " WHERE " + strings.Join(whereClauses, " AND ")
	}

	if opts.OrderBy != "" {
		dir := "ASC"
		if strings.ToLower(opts.OrderDirection) == "desc" {
			dir = "DESC"
		}
		query += fmt.Sprintf(" ORDER BY %s %s", opts.OrderBy, dir)
	}
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	if opts.Offset > 0 {
		query += fmt.Sprintf(" START %d", opts.Offset)
	}

	return runQueryForMaps(ctx, c.db, query, params)
}

// --- Placeholder for Transaction implementation ---
type Transaction struct{}
func (t *Transaction) Collection(name string) interfaces.CollectionRef { return nil }
func (t *Transaction) Commit() error                                { return nil }
func (t *Transaction) Rollback() error                              { return nil }

// --- Helper functions to process query results ---
func runQueryForMaps(ctx context.Context, db *surrealdb.DB, query string, params map[string]interface{}) ([]map[string]interface{}, error) {
	queryResult, err := surrealdb.Query[[]map[string]interface{}](ctx, db, query, params)
	if err != nil {
		return nil, err
	}
	if len(*queryResult) == 0 {
		return nil, fmt.Errorf("query returned no result sets")
	}
	firstStatementResult := (*queryResult)[0]
	if firstStatementResult.Error != nil {
		return nil, fmt.Errorf("surrealdb query error: %s", firstStatementResult.Error.Message)
	}
	return firstStatementResult.Result, nil
}

func runQueryForAny(ctx context.Context, db *surrealdb.DB, query string, params map[string]interface{}) (any, error) {
	queryResult, err := surrealdb.Query[any](ctx, db, query, params)
	if err != nil {
		return nil, err
	}
	if len(*queryResult) == 0 {
		return nil, fmt.Errorf("query returned no result sets")
	}
	firstStatementResult := (*queryResult)[0]
	if firstStatementResult.Error != nil {
		return nil, fmt.Errorf("surrealdb query error: %s", firstStatementResult.Error.Message)
	}
	return firstStatementResult.Result, nil
}
