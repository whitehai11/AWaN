package interfaces

import (
	"encoding/json"
	"net/http"

	"github.com/awan/awan/core/runtime"
	"github.com/awan/awan/core/types"
)

// API exposes the local HTTP contract used by UI clients.
type API struct {
	runtime *runtime.Runtime
}

// NewAPI creates a new local API handler set.
func NewAPI(rt *runtime.Runtime) *API {
	return &API{runtime: rt}
}

// Handler returns the full HTTP handler tree.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/run", a.handleAgentRun)
	mux.HandleFunc("/agent/chat", a.handleAgentChat)
	mux.HandleFunc("/memory", a.handleMemory)
	mux.HandleFunc("/memory/store", a.handleMemoryStore)
	mux.HandleFunc("/agents", a.handleAgents)
	mux.HandleFunc("/files", a.handleFiles)
	mux.HandleFunc("/tools/execute", a.handleToolExecute)
	mux.HandleFunc("/healthz", a.handleHealth)
	return mux
}

func (a *API) handleAgentRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var request types.AgentRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	response, err := a.runtime.Run(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (a *API) handleAgentChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var request types.AgentRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	response, err := a.runtime.Chat(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (a *API) handleMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	agentName := r.URL.Query().Get("agent")
	snapshot, err := a.runtime.MemorySnapshot(agentName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, snapshot)
}

func (a *API) handleMemoryStore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var request types.MemoryStoreRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	record, err := a.runtime.StoreMemory(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, record)
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	definitions := a.runtime.RegisteredAgents()
	type payload struct {
		Name        string   `json:"name"`
		Model       string   `json:"model"`
		Memory      bool     `json:"memory"`
		Tools       []string `json:"tools"`
		Description string   `json:"description"`
	}

	agents := make([]payload, 0, len(definitions))
	for _, definition := range definitions {
		agents = append(agents, payload{
			Name:        definition.Name,
			Model:       definition.Model,
			Memory:      definition.Memory,
			Tools:       definition.Tools,
			Description: definition.Description,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

func (a *API) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	files, err := a.runtime.ListFiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (a *API) handleToolExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var request struct {
		Agent string         `json:"agent"`
		Tool  string         `json:"tool"`
		Args  map[string]any `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := a.runtime.ExecuteTool(request.Agent, request.Tool, request.Args)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
