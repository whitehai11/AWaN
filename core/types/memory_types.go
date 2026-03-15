package types

import "time"

// MemoryRecord stores a single memory event for an agent.
type MemoryRecord struct {
	ID        string    `json:"id"`
	Agent     string    `json:"agent"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// MemoryStoreRequest is used by the local API to persist memory.
type MemoryStoreRequest struct {
	Agent   string `json:"agent"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

// MemorySnapshot groups short-term and long-term memory state.
type MemorySnapshot struct {
	Agent      string         `json:"agent"`
	ShortTerm  []MemoryRecord `json:"shortTerm"`
	LongTerm   []MemoryRecord `json:"longTerm"`
	StoredAt   time.Time      `json:"storedAt"`
	Vectorized bool           `json:"vectorized"`
}
