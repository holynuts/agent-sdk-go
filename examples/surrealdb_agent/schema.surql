-- Define tables for the agent's memory and vector store.
-- Using SCHEMALESS to allow for flexible fields.

DEFINE TABLE documents SCHEMALESS;
DEFINE TABLE memory_messages SCHEMALESS;
DEFINE TABLE memory_summaries SCHEMALESS;

-- Define a vector index on the 'documents' table for similarity search.
-- The dimension must match the dimension of the vectors being stored.
-- Our mock embedder uses a dimension of 16.
DEFINE INDEX document_vector_index ON TABLE documents COLUMNS vector MTREE DIMENSION 16;
