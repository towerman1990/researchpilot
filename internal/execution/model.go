package execution

import (
	"context"
	"net/http"

	provider "researchpilot/internal/model"
	"researchpilot/internal/service"
)

// CallModel executes a request using the task's immutable model configuration.
// The caller must reserve the persisted token budget before invoking it.
func CallModel(ctx context.Context, client *http.Client, config service.ModelConfig, prompt string) (string, uint32, error) {
	provider, err := service.ProviderFor(client, config)
	if err != nil {
		return "", 0, err
	}
	result, err := provider.Generate(ctx, service.ModelRequest(config, prompt))
	return result.Text, result.Tokens, err
}

type modelConfig = service.ModelConfig
type modelError = provider.HTTPError

const modelOutputLimit = service.ModelOutputLimit
