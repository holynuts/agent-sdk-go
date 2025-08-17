package memory

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ingenimax/agent-sdk-go/pkg/config"
	"github.com/Ingenimax/agent-sdk-go/pkg/interfaces"
	"github.com/Ingenimax/agent-sdk-go/pkg/multitenancy"
	"github.com/surrealdb/surrealdb.go"

	surrealdb_client "github.com/Ingenimax/agent-sdk-go/pkg/datastore/surrealdb"
)

// MockLLM is defined in redis_memory_test.go, so we don't redefine it here.

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

func TestMemory_AddAndGetMessages(t *testing.T) {
	db := setupTestDB(t)
	memory := NewSurrealDBMemory(db)
	ctx := context.Background()
	ctx = multitenancy.WithOrgID(ctx, "test-org-memory")
	ctx = context.WithValue(ctx, ConversationIDKey, "convo-1")

	// Cleanup
	err := memory.Clear(ctx)
	require.NoError(t, err)
	defer memory.Clear(ctx)

	// Add messages
	messages := []interfaces.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}
	for _, msg := range messages {
		err := memory.AddMessage(ctx, msg)
		require.NoError(t, err)
	}

	// Get messages
	retrieved, err := memory.GetMessages(ctx)
	require.NoError(t, err)
	require.Len(t, retrieved, 2)
	assert.Equal(t, "Hello", retrieved[0].Content)
	assert.Equal(t, "Hi there", retrieved[1].Content)
}

func TestMemory_Summarization(t *testing.T) {
	db := setupTestDB(t)
	mockLLM := &MockLLM{}
	// Summarize after 2 messages, keep 1 summary
	memory := NewSurrealDBMemory(db, WithSurrealDBSummarization(mockLLM, 2, 1))

	ctx := context.Background()
	ctx = multitenancy.WithOrgID(ctx, "test-org-memory-summarize")
	ctx = context.WithValue(ctx, ConversationIDKey, "convo-2")

	err := memory.Clear(ctx)
	require.NoError(t, err)
	defer memory.Clear(ctx)

	// Add 3 messages to trigger summarization
	require.NoError(t, memory.AddMessage(ctx, interfaces.Message{Role: "user", Content: "Msg 1"}))
	require.NoError(t, memory.AddMessage(ctx, interfaces.Message{Role: "user", Content: "Msg 2"}))
	// The third message should trigger summarization of the first two
	require.NoError(t, memory.AddMessage(ctx, interfaces.Message{Role: "user", Content: "Msg 3"}))

	// Get messages and check for summary
	retrieved, err := memory.GetMessages(ctx)
	require.NoError(t, err)

	// We expect 1 summary and the 1 message that was not summarized (the last one)
	// The summarization logic summarizes count - threshold/2 messages. 3 - 2/2 = 2 messages.
	// So Msg 1 and Msg 2 are summarized. Msg 3 remains.
	require.Len(t, retrieved, 2)
	assert.Equal(t, "system", retrieved[0].Role)
	assert.Contains(t, retrieved[0].Content, "This is a test summary.")
	assert.Equal(t, "Msg 3", retrieved[1].Content)
}
