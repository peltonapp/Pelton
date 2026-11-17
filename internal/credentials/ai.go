package credentials

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// aiPrefix namespaces an AI provider's API key, apart from every other entry.
const aiPrefix = "ai-"

// StoreAIKey saves the API key of the AI provider with this id, replacing any
// earlier one, or removes it when key is empty.
func StoreAIKey(id, key string) error {
	if key == "" {
		return DeleteAIKey(id)
	}
	remember(key)
	if err := keyring.Set(service, aiPrefix+id, key); err != nil {
		return fmt.Errorf("credentials: store the AI key: %w", err)
	}
	return nil
}

// LoadAIKey returns the API key of the AI provider with this id, "" when it
// has none.
func LoadAIKey(id string) (string, error) {
	raw, err := keyring.Get(service, aiPrefix+id)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("credentials: load the AI key: %w", err)
	}
	remember(raw)
	return raw, nil
}

// DeleteAIKey removes the API key of the AI provider with this id. A missing
// entry is not an error.
func DeleteAIKey(id string) error {
	err := keyring.Delete(service, aiPrefix+id)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("credentials: delete the AI key: %w", err)
	}
	return nil
}
