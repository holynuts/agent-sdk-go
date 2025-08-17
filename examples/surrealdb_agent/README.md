# SurrealDB Integration Example

This example demonstrates how to use the Agent SDK with SurrealDB as a unified backend for memory and vector storage.

## Functionality

The example application will:
1. Connect to a SurrealDB instance.
2. Automatically apply a basic schema, defining the necessary tables and a vector index.
3. Instantiate the `SurrealDBMemory` and `SurrealDBVectorStore` components.
4. Perform some basic operations:
   - Add a few messages to the conversation memory and retrieve them.
   - Store a document in the vector store and perform a similarity search.

## Prerequisites

- A running SurrealDB instance. You can use Docker for a quick setup:
  ```sh
  docker run --rm -p 8000:8000 surrealdb/surrealdb:latest start --log trace --user root --pass root
  ```
- An OpenAI API key (for the LLM client used in memory summarization).

## How to Run

1. **Set Environment Variables:**

   Export the following environment variables. You can modify them if your SurrealDB instance has different credentials.

   ```sh
   export SURREALDB_URL="ws://localhost:8000/rpc"
   export SURREALDB_USER="root"
   export SURREALDB_PASS="root"
   export OPENAI_API_KEY="your-openai-api-key"
   ```

2. **Run the Example:**

   From the root of the repository, run the following command:

   ```sh
   go run ./examples/surrealdb_agent/main.go
   ```

## Manual Seeding (Optional)

This example sets up the schema but does not seed any data. If you wish to start with pre-existing data, you can connect to your SurrealDB instance and manually insert records into the `memory_messages`, `memory_summaries`, or `documents` tables within the `test` namespace and `test` database.
