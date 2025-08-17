package memory

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

const (
	messagesCollection  = "memory_messages"
	summariesCollection = "memory_summaries"
)

// SurrealDBMemory implements a SurrealDB-backed memory store.
type SurrealDBMemory struct {
	db                   *surrealdb.DB
	summarizationEnabled bool
	llmClient            interfaces.LLM
	messageThreshold     int
	summaryCount         int
}

// SurrealDBMemoryOption represents an option for configuring the SurrealDB memory.
type SurrealDBMemoryOption func(*SurrealDBMemory)

// NewSurrealDBMemory creates a new SurrealDB-backed memory store.
func NewSurrealDBMemory(db *surrealdb.DB, options ...SurrealDBMemoryOption) *SurrealDBMemory {
	mem := &SurrealDBMemory{
		db:                   db,
		summarizationEnabled: false,
		messageThreshold:     50,
		summaryCount:         5,
	}
	for _, option := range options {
		option(mem)
	}
	return mem
}

// WithSurrealDBSummarization enables automatic summarization of old messages.
func WithSurrealDBSummarization(llm interfaces.LLM, messageThreshold int, summaryCount int) SurrealDBMemoryOption {
	return func(m *SurrealDBMemory) {
		m.summarizationEnabled = true
		m.llmClient = llm
		m.messageThreshold = messageThreshold
		m.summaryCount = summaryCount
	}
}

// AddMessage adds a message to memory.
func (m *SurrealDBMemory) AddMessage(ctx context.Context, message interfaces.Message) error {
	orgID, err := multitenancy.GetOrgID(ctx)
	if err != nil {
		orgID = "default"
	}
	conversationID, err := getConversationID(ctx)
	if err != nil {
		return err
	}

	storableMessage := map[string]interface{}{
		"id":              uuid.New().String(),
		"org_id":          orgID,
		"conversation_id": conversationID,
		"role":            message.Role,
		"content":         message.Content,
		"metadata":        message.Metadata,
		"tool_call_id":    message.ToolCallID,
		"tool_calls":      message.ToolCalls,
		"created_at":      time.Now().UTC(),
	}

	thing := fmt.Sprintf("%s:%s", messagesCollection, storableMessage["id"])
	_, err = surrealdb.Create[any](ctx, m.db, thing, storableMessage)
	if err != nil {
		return fmt.Errorf("failed to add message to surrealdb: %w", err)
	}

	if m.summarizationEnabled {
		if err := m.checkAndSummarize(ctx, orgID, conversationID); err != nil {
			fmt.Printf("summarization check failed: %v\n", err)
		}
	}

	return nil
}

// GetMessages retrieves messages from memory.
func (m *SurrealDBMemory) GetMessages(ctx context.Context, options ...interfaces.GetMessagesOption) ([]interfaces.Message, error) {
	orgID, err := multitenancy.GetOrgID(ctx)
	if err != nil {
		orgID = "default"
	}
	conversationID, err := getConversationID(ctx)
	if err != nil {
		return nil, err
	}

	opts := &interfaces.GetMessagesOptions{}
	for _, option := range options {
		option(opts)
	}

	var allMessages []interfaces.Message

	// Get summaries
	summaryQuery := "SELECT * FROM type::table($col) WHERE org_id = $org_id AND conversation_id = $conv_id ORDER BY created_at ASC"
	summaryParams := map[string]interface{}{"col": summariesCollection, "org_id": orgID, "conv_id": conversationID}
	summariesData, err := runQueryForMaps(ctx, m.db, summaryQuery, summaryParams)
	if err == nil {
		allMessages = append(allMessages, mapsToMessages(summariesData)...)
	}

	// Get messages
	messageQuery := "SELECT * FROM type::table($col) WHERE org_id = $org_id AND conversation_id = $conv_id ORDER BY created_at ASC"
	messageParams := map[string]interface{}{"col": messagesCollection, "org_id": orgID, "conv_id": conversationID}
	messagesData, err := runQueryForMaps(ctx, m.db, messageQuery, messageParams)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages from surrealdb: %w", err)
	}
	allMessages = append(allMessages, mapsToMessages(messagesData)...)

	// Filter and limit
	if len(opts.Roles) > 0 {
		var filtered []interfaces.Message
		roles := make(map[string]struct{})
		for _, role := range opts.Roles {
			roles[role] = struct{}{}
		}
		for _, msg := range allMessages {
			if _, ok := roles[msg.Role]; ok {
				filtered = append(filtered, msg)
			}
		}
		allMessages = filtered
	}

	if opts.Limit > 0 && len(allMessages) > opts.Limit {
		allMessages = allMessages[len(allMessages)-opts.Limit:]
	}

	return allMessages, nil
}

// Clear clears the memory for a conversation.
func (m *SurrealDBMemory) Clear(ctx context.Context) error {
	orgID, err := multitenancy.GetOrgID(ctx)
	if err != nil {
		orgID = "default"
	}
	conversationID, err := getConversationID(ctx)
	if err != nil {
		return err
	}

	params := map[string]interface{}{"org_id": orgID, "conv_id": conversationID}
	query := `
		DELETE type::table($messages_col) WHERE org_id = $org_id AND conversation_id = $conv_id;
		DELETE type::table($summaries_col) WHERE org_id = $org_id AND conversation_id = $conv_id;
	`
	params["messages_col"] = messagesCollection
	params["summaries_col"] = summariesCollection

	_, err = surrealdb.Query[any](ctx, m.db, query, params)
	return err
}

func (m *SurrealDBMemory) checkAndSummarize(ctx context.Context, orgID, conversationID string) error {
	countQuery := "SELECT count() FROM type::table($col) WHERE org_id = $org_id AND conversation_id = $conv_id GROUP BY all"
	params := map[string]interface{}{"col": messagesCollection, "org_id": orgID, "conv_id": conversationID}
	countResult, err := runQueryForMaps(ctx, m.db, countQuery, params)
	if err != nil || len(countResult) == 0 {
		return fmt.Errorf("failed to count messages: %w", err)
	}
	count := 0
	if c, ok := countResult[0]["count"].(float64); ok {
		count = int(c)
	}

	if count < m.messageThreshold {
		return nil
	}

	summarizeCount := count - (m.messageThreshold / 2)
	fetchQuery := "SELECT * FROM type::table($col) WHERE org_id = $org_id AND conversation_id = $conv_id ORDER BY created_at ASC LIMIT $limit"
	fetchParams := map[string]interface{}{"col": messagesCollection, "org_id": orgID, "conv_id": conversationID, "limit": summarizeCount}
	messagesData, err := runQueryForMaps(ctx, m.db, fetchQuery, fetchParams)
	if err != nil {
		return fmt.Errorf("failed to fetch messages for summarization: %w", err)
	}
	messages := mapsToMessages(messagesData)
	if len(messages) == 0 {
		return nil
	}

	summary, err := m.createSummary(ctx, messages)
	if err != nil {
		return err
	}

	var idsToDelete []string
	for _, msgData := range messagesData {
		if id, ok := msgData["id"].(string); ok {
			idsToDelete = append(idsToDelete, fmt.Sprintf("%s:%s", messagesCollection, id))
		}
	}

	summaryID := uuid.New().String()
	summaryThing := fmt.Sprintf("%s:%s", summariesCollection, summaryID)
	summaryStorable := map[string]interface{}{
		"id":              summaryID,
		"org_id":          orgID,
		"conversation_id": conversationID,
		"role":            summary.Role,
		"content":         summary.Content,
		"metadata":        summary.Metadata,
		"created_at":      time.Now().UTC(),
	}

	txQuery := `CREATE type::thing($summary_thing) CONTENT $summary_data; DELETE $ids_to_delete;`
	txParams := map[string]interface{}{
		"summary_thing": summaryThing,
		"summary_data":  summaryStorable,
		"ids_to_delete": idsToDelete,
	}

	_, err = surrealdb.Query[any](ctx, m.db, txQuery, txParams)
	if err != nil {
		return fmt.Errorf("failed to store summary and delete old messages: %w", err)
	}

	return m.rotateSummaries(ctx, orgID, conversationID)
}

func (m *SurrealDBMemory) createSummary(ctx context.Context, messages []interfaces.Message) (interfaces.Message, error) {
	var sb strings.Builder
	sb.WriteString("Summarize the following conversation concisely, preserving key information and context:\n\n")
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("%s: %s\n", msg.Role, msg.Content))
	}
	sb.WriteString("\nProvide a concise summary that captures the essential information from this conversation.")

	summaryContent, err := m.llmClient.Generate(ctx, sb.String(), func(o *interfaces.GenerateOptions) {
		o.LLMConfig = &interfaces.LLMConfig{Temperature: 0.3}
	})
	if err != nil {
		return interfaces.Message{}, err
	}

	return interfaces.Message{
		Role: "system",
		Content: fmt.Sprintf("Previous conversation summary (%d messages): %s", len(messages), strings.TrimSpace(summaryContent)),
		Metadata: map[string]interface{}{"is_summary": true, "message_count": len(messages), "summarized_at": time.Now().UTC()},
	}, nil
}

func (m *SurrealDBMemory) rotateSummaries(ctx context.Context, orgID, conversationID string) error {
	// Implementation similar to checkAndSummarize
	return nil // Placeholder
}

// --- Helper functions ---
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

func mapsToMessages(data []map[string]interface{}) []interfaces.Message {
	var messages []interfaces.Message
	for _, res := range data {
		var msg interfaces.Message
		if role, ok := res["role"].(string); ok {
			msg.Role = role
		}
		if content, ok := res["content"].(string); ok {
			msg.Content = content
		}
		if metadata, ok := res["metadata"].(map[string]interface{}); ok {
			msg.Metadata = metadata
		}
		if toolCallID, ok := res["tool_call_id"].(string); ok {
			msg.ToolCallID = toolCallID
		}
		messages = append(messages, msg)
	}
	return messages
}
