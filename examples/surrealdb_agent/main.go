package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Ingenimax/agent-sdk-go/pkg/config"
	"github.com/Ingenimax/agent-sdk-go/pkg/interfaces"
	"github.com/Ingenimax/agent-sdk-go/pkg/llm/openai"
	"github.com/Ingenimax/agent-sdk-go/pkg/multitenancy"
	"github.com/surrealdb/surrealdb.go"

	surrealdb_client "github.com/Ingenimax/agent-sdk-go/pkg/datastore/surrealdb"
	surrealdb_memory "github.com/Ingenimax/agent-sdk-go/pkg/memory"
	surrealdb_vectorstore "github.com/Ingenimax/agent-sdk-go/pkg/vectorstore/surrealdb"
)

// MockEmbedder is a simple mock implementation of the Embedder interface for testing.
type MockEmbedder struct{}

func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return make([]float32, 16), nil
}
func (m *MockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i := range texts {
		embeddings[i] = make([]float32, 16)
	}
	return embeddings, nil
}
func (m *MockEmbedder) CalculateSimilarity(vec1, vec2 []float32, metric string) (float32, error) {
	return 0.9, nil
}

func main() {
	fmt.Println("--- SurrealDB Integration Example ---")
	ctx := context.Background()

	// 1. Configure and connect to SurrealDB
	cfg := &config.SurrealDBConfig{
		URL:      getEnv("SURREALDB_URL", "ws://localhost:8000/rpc"),
		Username: getEnv("SURREALDB_USER", "root"),
		Password: getEnv("SURREALDB_PASS", "root"),
		NS:       getEnv("SURREALDB_NS", "test"),
		DB:       getEnv("SURREALDB_DB", "test"),
	}

	fmt.Printf("Connecting to SurrealDB at %s...\n", cfg.URL)
	db, err := surrealdb_client.New(cfg)
	if err != nil {
		fmt.Printf("Failed to connect to SurrealDB: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Successfully connected to SurrealDB.")

	// 2. Apply database schema
	err = applySchema(ctx, db, "examples/surrealdb_agent/schema.surql")
	if err != nil {
		fmt.Printf("Failed to apply database schema: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Successfully applied database schema.")

	// 3. Instantiate components
	mockEmbedder := &MockEmbedder{}

	llm := openai.NewClient(os.Getenv("OPENAI_API_KEY"))

	vectorStore := surrealdb_vectorstore.New(db, mockEmbedder)
	memory := surrealdb_memory.NewSurrealDBMemory(db,
		surrealdb_memory.WithSurrealDBSummarization(llm, 5, 2),
	)

	// 4. Use the components
	ctx = multitenancy.WithOrgID(ctx, "example-org")
	ctx = context.WithValue(ctx, surrealdb_memory.ConversationIDKey, "convo-123")

	fmt.Println("\nAdding messages to memory...")
	messages := []interfaces.Message{
		{Role: "user", Content: "Hello, who are you?"},
		{Role: "assistant", Content: "I am an agent using a SurrealDB backend."},
	}
	for _, msg := range messages {
		if err := memory.AddMessage(ctx, msg); err != nil {
			fmt.Printf("Failed to add message: %v\n", err)
		}
	}

	retrievedMessages, err := memory.GetMessages(ctx)
	if err != nil {
		fmt.Printf("Failed to retrieve messages: %v\n", err)
	} else {
		fmt.Printf("Successfully retrieved %d messages.\n", len(retrievedMessages))
	}

	fmt.Println("\nTesting Vector Store...")
	docID := "doc-1"
	doc := interfaces.Document{
		ID:      docID,
		Content: "SurrealDB is a scalable, distributed, collaborative, document-graph database.",
		Metadata: map[string]interface{}{"source": "example"},
	}

	storeOpts := []interfaces.StoreOption{
		interfaces.WithGenerateVectors(true),
	}
	err = vectorStore.Store(ctx, []interfaces.Document{doc}, storeOpts...)
	if err != nil {
		fmt.Printf("Failed to store document: %v\n", err)
	} else {
		fmt.Println("Successfully stored document.")
	}

	searchResults, err := vectorStore.Search(ctx, "What is SurrealDB?", 1)
	if err != nil {
		fmt.Printf("Failed to search for document: %v\n", err)
	} else if len(searchResults) > 0 {
		fmt.Printf("Found doc ID: %s with score %f\n", searchResults[0].Document.ID, searchResults[0].Score)
	} else {
		fmt.Println("Did not find any documents.")
	}

	fmt.Println("\n--- Example Finished ---")
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func applySchema(ctx context.Context, db *surrealdb.DB, filepath string) error {
	schema, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("failed to read schema file: %w", err)
	}

	if len(schema) == 0 {
		return nil // Nothing to apply
	}

	queryResult, err := surrealdb.Query[any](ctx, db, string(schema), nil)
	if err != nil {
		return err
	}

	for _, res := range *queryResult {
		if res.Error != nil {
			return fmt.Errorf("schema query failed: %s", res.Error.Message)
		}
	}

	return nil
}
