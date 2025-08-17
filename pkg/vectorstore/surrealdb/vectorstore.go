package surrealdb

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/surrealdb/surrealdb.go"

	"github.com/Ingenimax/agent-sdk-go/pkg/interfaces"
)

// Store implements the VectorStore interface for SurrealDB.
type Store struct {
	db       *surrealdb.DB
	embedder interfaces.Embedder
}

// New creates a new SurrealDB vector store.
func New(db *surrealdb.DB, embedder interfaces.Embedder) *Store {
	return &Store{
		db:       db,
		embedder: embedder,
	}
}

// Store stores documents in SurrealDB.
func (s *Store) Store(ctx context.Context, documents []interfaces.Document, options ...interfaces.StoreOption) error {
	opts := &interfaces.StoreOptions{BatchSize: 100}
	for _, option := range options {
		option(opts)
	}

	className := "documents"
	if opts.Class != "" {
		className = opts.Class
	}

	for _, doc := range documents {
		if doc.ID == "" {
			doc.ID = uuid.New().String()
		}
		if doc.Vector == nil {
			if s.embedder == nil || !opts.GenerateVectors {
				return fmt.Errorf("document %s has no vector and embedder is not available", doc.ID)
			}
			vector, err := s.embedder.Embed(ctx, doc.Content)
			if err != nil {
				return fmt.Errorf("failed to generate embedding for doc %s: %w", doc.ID, err)
			}
			doc.Vector = vector
		}

		storableDoc := map[string]interface{}{
			"id":       doc.ID,
			"content":  doc.Content,
			"vector":   doc.Vector,
			"metadata": doc.Metadata,
		}
		if opts.Tenant != "" {
			storableDoc["tenant_id"] = opts.Tenant
		}

		thing := fmt.Sprintf("%s:%s", className, doc.ID)
		_, err := surrealdb.Create[any](ctx, s.db, thing, storableDoc)
		if err != nil {
			return fmt.Errorf("failed to store document %s: %w", doc.ID, err)
		}
	}

	return nil
}

// Get retrieves a document by ID.
func (s *Store) Get(ctx context.Context, id string, options ...interfaces.StoreOption) (*interfaces.Document, error) {
	opts := &interfaces.StoreOptions{}
	for _, option := range options {
		option(opts)
	}
	className := "documents"
	if opts.Class != "" {
		className = opts.Class
	}

	thing := fmt.Sprintf("%s:%s", className, id)
	results, err := surrealdb.Select[[]map[string]interface{}](ctx, s.db, thing)
	if err != nil {
		return nil, err
	}
	if len(*results) == 0 {
		return nil, fmt.Errorf("document not found")
	}

	return mapToDocument((*results)[0])
}

// Search searches for similar documents by text query.
func (s *Store) Search(ctx context.Context, query string, limit int, options ...interfaces.SearchOption) ([]interfaces.SearchResult, error) {
	if s.embedder == nil {
		return nil, fmt.Errorf("embedder is required for text search")
	}
	vector, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedding for query: %w", err)
	}
	return s.SearchByVector(ctx, vector, limit, options...)
}

// SearchByVector searches for similar documents by vector.
func (s *Store) SearchByVector(ctx context.Context, vector []float32, limit int, options ...interfaces.SearchOption) ([]interfaces.SearchResult, error) {
	opts := &interfaces.SearchOptions{}
	for _, option := range options {
		option(opts)
	}
	className := "documents"
	if opts.Class != "" {
		className = opts.Class
	}

	whereClauses := []string{}
	params := map[string]interface{}{
		"query_vector": vector,
		"limit":        limit,
	}

	if opts.Tenant != "" {
		whereClauses = append(whereClauses, "tenant_id = $tenant_id")
		params["tenant_id"] = opts.Tenant
	}

	if opts.Filters != nil {
		filterClause, filterParams, err := buildWhereFilter(opts.Filters, "p")
		if err != nil {
			return nil, fmt.Errorf("failed to build filter: %w", err)
		}
		if filterClause != "" {
			whereClauses = append(whereClauses, filterClause)
			for k, v := range filterParams {
				params[k] = v
			}
		}
	}

	query := fmt.Sprintf("SELECT *, vector::similarity::cosine(vector, $query_vector) AS score FROM %s", className)
	if len(whereClauses) > 0 {
		query += " WHERE " + strings.Join(whereClauses, " AND ")
	}
	query += " ORDER BY score DESC LIMIT $limit"

	results, err := runQueryForMaps(ctx, s.db, query, params)
	if err != nil {
		return nil, err
	}

	var searchResults []interfaces.SearchResult
	for _, res := range results {
		doc, err := mapToDocument(res)
		if err != nil {
			continue
		}
		score := float32(0.0)
		if s, ok := res["score"].(float64); ok {
			score = float32(s)
		}
		if score >= opts.MinScore {
			searchResults = append(searchResults, interfaces.SearchResult{
				Document: *doc,
				Score:    score,
			})
		}
	}

	return searchResults, nil
}

// Delete deletes documents by ID.
func (s *Store) Delete(ctx context.Context, ids []string, options ...interfaces.DeleteOption) error {
	opts := &interfaces.DeleteOptions{}
	for _, option := range options {
		option(opts)
	}
	className := "documents"
	if opts.Class != "" {
		className = opts.Class
	}

	for _, id := range ids {
		thing := fmt.Sprintf("%s:%s", className, id)
		// This doesn't allow for tenant check. A more robust implementation would query first.
		_, err := surrealdb.Delete[any](ctx, s.db, thing)
		if err != nil {
			// Log and continue
		}
	}
	return nil
}

func (s *Store) GlobalStore(ctx context.Context, documents []interfaces.Document, options ...interfaces.StoreOption) error {
	return s.Store(ctx, documents, options...)
}
func (s *Store) GlobalSearch(ctx context.Context, query string, limit int, options ...interfaces.SearchOption) ([]interfaces.SearchResult, error) {
	return s.Search(ctx, query, limit, options...)
}
func (s *Store) GlobalSearchByVector(ctx context.Context, vector []float32, limit int, options ...interfaces.SearchOption) ([]interfaces.SearchResult, error) {
	return s.SearchByVector(ctx, vector, limit, options...)
}
func (s *Store) GlobalDelete(ctx context.Context, ids []string, options ...interfaces.DeleteOption) error {
	return s.Delete(ctx, ids, options...)
}

func (s *Store) CreateTenant(ctx context.Context, tenantName string) error {
	query := "DEFINE NAMESPACE $name"
	params := map[string]interface{}{"name": tenantName}
	_, err := runQueryForAny(ctx, s.db, query, params)
	return err
}

func (s *Store) DeleteTenant(ctx context.Context, tenantName string) error {
	query := "REMOVE NAMESPACE $name"
	params := map[string]interface{}{"name": tenantName}
	_, err := runQueryForAny(ctx, s.db, query, params)
	return err
}

func (s *Store) ListTenants(ctx context.Context) ([]string, error) {
	results, err := runQueryForMaps(ctx, s.db, "INFO FOR KV", nil)
	if err != nil || len(results) == 0 {
		return nil, fmt.Errorf("could not parse KV info: %w", err)
	}
	var tenants []string
	if ns, ok := results[0]["ns"].(map[string]interface{}); ok {
		for name := range ns {
			tenants = append(tenants, name)
		}
	}
	return tenants, nil
}

// --- Helper Functions ---
func mapToDocument(data map[string]interface{}) (*interfaces.Document, error) {
	doc := &interfaces.Document{}
	if id, ok := data["id"].(string); ok { doc.ID = id }
	if content, ok := data["content"].(string); ok { doc.Content = content }
	if metadata, ok := data["metadata"].(map[string]interface{}); ok { doc.Metadata = metadata }
	if vector, ok := data["vector"].([]interface{}); ok {
		doc.Vector = make([]float32, len(vector))
		for i, v := range vector {
			if f, ok := v.(float64); ok { doc.Vector[i] = float32(f) }
		}
	}
	return doc, nil
}

func buildWhereFilter(filter map[string]interface{}, paramPrefix string) (string, map[string]interface{}, error) {
	var clauses []string
	params := make(map[string]interface{})
	i := 0
	for key, value := range filter {
		paramName := fmt.Sprintf("%s%d", paramPrefix, i)
		clauses = append(clauses, fmt.Sprintf("metadata.%s = $%s", key, paramName))
		params[paramName] = value
		i++
	}
	if len(clauses) == 0 { return "", nil, nil }
	return strings.Join(clauses, " AND "), params, nil
}

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
