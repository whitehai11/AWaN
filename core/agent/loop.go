package agent

import (
	"strings"
	"github.com/whitehai11/AWaN/core/models"
)

// RunLoop executes the current prompt -> model -> response flow.
//
// The function is isolated so future thought -> action -> observation
// loops can evolve without changing the higher-level agent API.
func RunLoop(model models.Model, prompt string) (string, error) {
	return model.Generate(strings.TrimSpace(prompt))
}
