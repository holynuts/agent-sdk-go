package surrealdb

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ingenimax/agent-sdk-go/pkg/config"
	"github.com/Ingenimax/agent-sdk-go/pkg/interfaces"
	"github.com/Ingenimax/agent-sdk-go/pkg/multitenancy"
	"github.com/surrealdb/surrealdb.go"
)

func setupTestDB(t *testing.T) *surrealdb.DB {
	t.Helper()

	url := os.Getenv("SURREALDB_URL")
	if url == "" {
		t.Skip("SURREALDB_URL not set, skipping integration tests")
	}

	cfg := &config.SurrealDBConfig{
		URL:      url,
		Username: os.Getenv("SURREALDB_USER"),
		Password: os.Getenv("SURREALDB_PASS"),
		NS:       "test",
		DB:       "test",
	}

	db, err := New(cfg)
	require.NoError(t, err, "Failed to connect to SurrealDB")

	return db
}

func TestDataStore_InsertAndGet(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewDataStore(db)
	ctx := multitenancy.WithOrgID(context.Background(), "test-org-datastore")
	collectionName := "test_items"

	// Cleanup any previous test data
	_, _ = runQueryForAny(ctx, db, fmt.Sprintf("DELETE %s", collectionName), nil)

	defer func() {
		// Final cleanup
		_, _ = runQueryForAny(ctx, db, fmt.Sprintf("DELETE %s", collectionName), nil)
	}()

	// Insert
	data := map[string]interface{}{
		"name": "Test Item",
		"value": 123.45,
	}
	id, err := dataStore.Collection(collectionName).Insert(ctx, data)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	// Get
	retrieved, err := dataStore.Collection(collectionName).Get(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Assert
	assert.Equal(t, "Test Item", retrieved["name"])
	assert.Equal(t, 123.45, retrieved["value"])
	assert.Equal(t, "test-org-datastore", retrieved["org_id"])
}

func TestDataStore_Update(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewDataStore(db)
	ctx := multitenancy.WithOrgID(context.Background(), "test-org-datastore")
	collectionName := "test_items_update"

	_, _ = runQueryForAny(ctx, db, fmt.Sprintf("DELETE %s", collectionName), nil)
	defer func() {
		_, _ = runQueryForAny(ctx, db, fmt.Sprintf("DELETE %s", collectionName), nil)
	}()

	// Insert
	originalData := map[string]interface{}{"name": "Original", "status": "active"}
	id, err := dataStore.Collection(collectionName).Insert(ctx, originalData)
	require.NoError(t, err)

	// Update
	updateData := map[string]interface{}{"status": "inactive"}
	err = dataStore.Collection(collectionName).Update(ctx, id, updateData)
	require.NoError(t, err)

	// Get and Assert
	retrieved, err := dataStore.Collection(collectionName).Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "inactive", retrieved["status"])
	assert.Contains(t, retrieved, "updated_at")
}

func TestDataStore_Query(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewDataStore(db)
	ctx := multitenancy.WithOrgID(context.Background(), "test-org-query")
	collectionName := "test_items_query"

	_, _ = runQueryForAny(ctx, db, fmt.Sprintf("DELETE %s", collectionName), nil)
	defer func() {
		_, _ = runQueryForAny(ctx, db, fmt.Sprintf("DELETE %s", collectionName), nil)
	}()

	// Insert test data
	_, err := dataStore.Collection(collectionName).Insert(ctx, map[string]interface{}{"category": "A", "value": 10})
	require.NoError(t, err)
	_, err = dataStore.Collection(collectionName).Insert(ctx, map[string]interface{}{"category": "B", "value": 20})
	require.NoError(t, err)
	_, err = dataStore.Collection(collectionName).Insert(ctx, map[string]interface{}{"category": "A", "value": 30})
	require.NoError(t, err)

	// Query
	filter := map[string]interface{}{"category": "A"}
	options := interfaces.QueryWithOrderBy("value", "DESC")
	results, err := dataStore.Collection(collectionName).Query(ctx, filter, options)

	// Assert
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, 30.0, results[0]["value"]) // Note: numbers come back as float64
	assert.Equal(t, 10.0, results[1]["value"])
}
