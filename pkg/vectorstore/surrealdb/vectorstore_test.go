package surrealdb

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surrealdb/surrealdb.go"

	"github.com/Ingenimax/agent-sdk-go/pkg/config"
	"github.com/Ingenimax/agent-sdk-go/pkg/interfaces"
	surrealdb_client "github.com/Ingenimax/agent-sdk-go/pkg/datastore/surrealdb"
)

// MockEmbedder for testing
type MockEmbedder struct{}
func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}
func (m *MockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return [][]float32{{0.1, 0.2, 0.3}}, nil
}
func (m *MockEmbedder) CalculateSimilarity(v1, v2 []float32, mtr string) (float32, error) {
	return 0.99, nil
}

func setupTestDB(t *testing.T) *surrealdb.DB {
	t.Helper()
	url := os.Getenv("SURREALDB_URL")
	if url == "" {
		t.Skip("SURREALDB_URL not set, skipping integration tests")
	}
	cfg := &config.SurrealDBConfig{
		URL: url,
		Username: os.Getenv("SURREALDB_USER"),
		Password: os.Getenv("SURREALDB_PASS"),
		NS: "test",
		DB: "test",
	}
	db, err := surrealdb_client.New(cfg)
	require.NoError(t, err, "Failed to connect to SurrealDB")
	return db
}

func TestVectorStore_StoreAndSearch(t *testing.T) {
	db := setupTestDB(t)
	embedder := &MockEmbedder{}
	vectorStore := New(db, embedder)
	ctx := context.Background()
	className := "test_vectors"

	// Store a document
	doc := interfaces.Document{
		ID: "doc1",
		Content: "This is a test document.",
		Vector: []float32{0.1, 0.2, 0.3},
	}
	err := vectorStore.Store(ctx, []interfaces.Document{doc}, interfaces.WithClass(className))
	require.NoError(t, err)

	// Clean up
	defer func() {
		_ = vectorStore.Delete(ctx, []string{doc.ID}, interfaces.WithClassDelete(className))
	}()

	// Search for the document
	searchResults, err := vectorStore.SearchByVector(ctx, []float32{0.1, 0.2, 0.3}, 1, interfaces.WithClassSearch(className))
	require.NoError(t, err)
	require.Len(t, searchResults, 1)
	assert.Equal(t, "doc1", searchResults[0].Document.ID)
	assert.GreaterOrEqual(t, searchResults[0].Score, float32(0.99))
}

func TestVectorStore_TenantManagement(t *testing.T) {
	db := setupTestDB(t)
	embedder := &MockEmbedder{}
	vectorStore := New(db, embedder)
	ctx := context.Background()
	tenantName := "test-tenant-1"

	// Cleanup any previous tenant
	_ = vectorStore.DeleteTenant(ctx, tenantName)

	// Create Tenant
	err := vectorStore.CreateTenant(ctx, tenantName)
	require.NoError(t, err)

	// List Tenants
	tenants, err := vectorStore.ListTenants(ctx)
	require.NoError(t, err)
	assert.Contains(t, tenants, tenantName)

	// Delete Tenant
	err = vectorStore.DeleteTenant(ctx, tenantName)
	require.NoError(t, err)

	// Verify deletion
	tenants, err = vectorStore.ListTenants(ctx)
	require.NoError(t, err)
	assert.NotContains(t, tenants, tenantName)
}
