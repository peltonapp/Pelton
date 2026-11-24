package credentials

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// chatPrefix namespaces a chat account's keyring entry, apart from mail
// accounts, address books and calendars with the same id.
const chatPrefix = "chat-"

// ChatSecret is what a signed-in chat account keeps out of every database: the
// access token the homeserver issued and the key the local end to end
// encryption store is pickled with.
type ChatSecret struct {
	AccessToken string `json:"accessToken"`
	PickleKey   string `json:"pickleKey"`
}

// StoreChatSecret saves the secret of the chat account named key, replacing any
// earlier one.
func StoreChatSecret(key string, s ChatSecret) error {
	remember(s.AccessToken, s.PickleKey)
	encoded, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("credentials: encode chat secret: %w", err)
	}
	if err := keyring.Set(service, chatPrefix+key, string(encoded)); err != nil {
		return fmt.Errorf("credentials: store chat secret: %w", err)
	}
	return nil
}

// LoadChatSecret returns the secret of the chat account named key, or
// ErrNotFound when it never signed in.
func LoadChatSecret(key string) (ChatSecret, error) {
	raw, err := keyring.Get(service, chatPrefix+key)
	if errors.Is(err, keyring.ErrNotFound) {
		return ChatSecret{}, ErrNotFound
	}
	if err != nil {
		return ChatSecret{}, fmt.Errorf("credentials: load chat secret: %w", err)
	}
	var s ChatSecret
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return ChatSecret{}, fmt.Errorf("credentials: decode chat secret: %w", err)
	}
	remember(s.AccessToken, s.PickleKey)
	return s, nil
}

// DeleteChatSecret removes the secret of the chat account named key. A missing
// entry is not an error, so signing out twice is harmless.
func DeleteChatSecret(key string) error {
	err := keyring.Delete(service, chatPrefix+key)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("credentials: delete chat secret: %w", err)
	}
	return nil
}
