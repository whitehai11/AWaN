package memory

// VectorMemory is a placeholder for future semantic retrieval support.
//
// The runtime exposes this type now so later implementations can add
// embeddings and nearest-neighbor search without reshaping the memory API.
type VectorMemory struct {
	Enabled bool
}
