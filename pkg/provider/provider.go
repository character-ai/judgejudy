package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/character-ai/judgejudy/pkg/models"
)

// Provider is the interface that all generation backends must implement.
type Provider interface {
	// Name returns the provider identifier (e.g. "openai", "anthropic").
	Name() string

	// Generate sends a generation request and returns the response.
	Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error)

	// SupportsModality reports whether this provider can handle the given modality.
	SupportsModality(m models.Modality) bool
}

// Constructor is a function that creates a new Provider given an API key.
type Constructor func(apiKey string) (Provider, error)

var (
	registryMu   sync.RWMutex
	constructors = make(map[string]Constructor)
)

// Register adds a provider constructor to the global registry.
func Register(name string, ctor Constructor) {
	registryMu.Lock()
	defer registryMu.Unlock()
	constructors[name] = ctor
}

// Get retrieves a provider constructor by name.
func Get(name string) (Constructor, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	ctor, ok := constructors[name]
	return ctor, ok
}

// noAPIKeyRequired lists providers that do not need an API key.
var noAPIKeyRequired = map[string]bool{
	"ollama":      true,
	"passthrough": true,
}

// NewProvider creates a provider by name. If apiKey is empty, it reads from
// the environment variable JUDGEJUDY_{PROVIDER}_API_KEY.
// Providers listed in noAPIKeyRequired skip the API key check.
func NewProvider(name, apiKey string) (Provider, error) {
	registryMu.RLock()
	ctor, ok := constructors[name]
	registryMu.RUnlock()
	if !ok {
		return nil, &models.ProviderError{
			Provider: name,
			Message:  fmt.Sprintf("unknown provider %q", name),
		}
	}

	if apiKey == "" && !noAPIKeyRequired[name] {
		envKey := "JUDGEJUDY_" + strings.ToUpper(name) + "_API_KEY"
		apiKey = os.Getenv(envKey)
		if apiKey == "" {
			return nil, &models.ProviderError{
				Provider: name,
				Message:  fmt.Sprintf("API key not provided and %s not set", envKey),
			}
		}
	}

	return ctor(apiKey)
}

// ListProviders returns the names of all registered providers.
func ListProviders() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(constructors))
	for name := range constructors {
		names = append(names, name)
	}
	return names
}
