package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/awan/awan/core/filesystem"
	"github.com/awan/awan/core/types"
)

// Message is a lightweight view used by the agent loop.
type Message struct {
	Role    string
	Content string
}

// Manager coordinates short-term and long-term memory.
type Manager struct {
	short *ShortTermMemory
	long  *LongTermMemory
}

// NewManager creates runtime memory stores backed by the agent filesystem.
func NewManager(fs *filesystem.AgentFS) (*Manager, error) {
	longPath := filepath.Join(fs.Paths().Memory, "memory.json")
	longMemory, err := NewLongTermMemory(longPath)
	if err != nil {
		return nil, err
	}

	return &Manager{
		short: NewShortTermMemory(),
		long:  longMemory,
	}, nil
}

// Store persists a memory event to both short-term and long-term storage.
func (m *Manager) Store(record types.MemoryRecord) error {
	if record.ID == "" {
		record.ID = time.Now().UTC().Format("20060102150405.000000000")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	m.short.Append(record)
	return m.long.Append(record)
}

// Snapshot returns the memory state for an agent.
func (m *Manager) Snapshot(agent string) (*types.MemorySnapshot, error) {
	longTerm, err := m.long.List(agent)
	if err != nil {
		return nil, err
	}

	return &types.MemorySnapshot{
		Agent:      agent,
		ShortTerm:  m.short.List(agent),
		LongTerm:   longTerm,
		StoredAt:   time.Now().UTC(),
		Vectorized: false,
	}, nil
}

// MemoryIDs returns the most recent long-term memory IDs for an agent.
func (m *Manager) MemoryIDs(agent string, limit int) ([]string, error) {
	records, err := m.long.List(agent)
	if err != nil {
		return nil, err
	}
	if len(records) > limit {
		records = records[len(records)-limit:]
	}

	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}

	return ids, nil
}

// ShortTermMemory stores recent messages in RAM.
type ShortTermMemory struct {
	mu      sync.RWMutex
	records []types.MemoryRecord
}

// NewShortTermMemory creates an in-memory store.
func NewShortTermMemory() *ShortTermMemory {
	return &ShortTermMemory{
		records: []types.MemoryRecord{},
	}
}

// Append stores a memory record in RAM.
func (m *ShortTermMemory) Append(record types.MemoryRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, record)
}

// List returns records for the requested agent.
func (m *ShortTermMemory) List(agent string) []types.MemoryRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]types.MemoryRecord, 0)
	for _, record := range m.records {
		if agent == "" || record.Agent == agent {
			result = append(result, record)
		}
	}

	return result
}

// LongTermMemory stores records in a JSON file under ~/.awan/memory.
type LongTermMemory struct {
	mu   sync.Mutex
	path string
}

// NewLongTermMemory creates the JSON-backed memory store.
func NewLongTermMemory(path string) (*LongTermMemory, error) {
	store := &LongTermMemory{path: path}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte("[]\n"), 0o600); err != nil {
			return nil, err
		}
	}
	return store, nil
}

// Append adds a record to persistent storage.
func (m *LongTermMemory) Append(record types.MemoryRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	records, err := m.readAll()
	if err != nil {
		return err
	}

	records = append(records, record)
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.path, data, 0o600)
}

// Get returns a stored record by ID for explicit memory lookup flows.
func (m *LongTermMemory) Get(id string) (*types.MemoryRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	records, err := m.readAll()
	if err != nil {
		return nil, err
	}

	for _, record := range records {
		if record.ID == id {
			copyRecord := record
			return &copyRecord, nil
		}
	}

	return nil, os.ErrNotExist
}

// List returns all stored records for an agent.
func (m *LongTermMemory) List(agent string) ([]types.MemoryRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	records, err := m.readAll()
	if err != nil {
		return nil, err
	}

	result := make([]types.MemoryRecord, 0)
	for _, record := range records {
		if agent == "" || record.Agent == agent {
			result = append(result, record)
		}
	}

	return result, nil
}

func (m *LongTermMemory) readAll() ([]types.MemoryRecord, error) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return nil, err
	}

	var records []types.MemoryRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}

	return records, nil
}
