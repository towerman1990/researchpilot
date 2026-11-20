package execution

import (
	"context"
	"net/http"

	provider "researchpilot/internal/model"
	"researchpilot/internal/service"
)

// CallModel executes a request using the task's immutable model configuration.
// The caller must reserve the persisted token budget before invoking it.
func CallModel(ctx context.Context, client *http.Client, config service.ModelConfig, prompt string) (provider.Response, error) {
	backend, err := service.ProviderFor(client, config)
	if err != nil {
		return provider.Response{}, err
	}
	return backend.Generate(ctx, service.ModelRequest(config, prompt))
}

type modelConfig = service.ModelConfig
type modelError = provider.HTTPError

const modelOutputLimit = service.ModelOutputLimit
