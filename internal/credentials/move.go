package credentials

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/zalando/go-keyring"
)

// DefaultService is the keyring service name every build filed its secrets
// under before each channel got its own (see UseService).
const DefaultService = defaultService

// Service is the keyring service name this process files its secrets under.
func Service() string { return service }

// Known names the secrets the app may hold, as the database knows them. The
// keyring is never enumerated: only these entries are looked up.
type Known struct {
	AccountIDs      []int64
	AddressBookIDs  []int64
	PGPFingerprints []string
}

// CopyFrom copies each known secret filed under the service from into the
// current service, where the current service has no entry of that name yet.
// The entries under from are left in place, so a build that still files under
// from keeps working. It returns how many entries it copied; an entry that
// cannot be read or written is skipped and its error returned with the rest.
func CopyFrom(from string, k Known) (int, error) {
	if from == service {
		return 0, nil
	}
	names := []string{proxyPasswordKey, virusTotalKeyName}
	for _, id := range k.AccountIDs {
		names = append(names, key(id), accountProxyPasswordKey(id))
	}
	for _, id := range k.AddressBookIDs {
		names = append(names, addressBookKey(id))
	}
	for _, fp := range k.PGPFingerprints {
		names = append(names, pgpPassphrasePrefix+fp)
	}
	copied := 0
	var errs []error
	for _, name := range names {
		n, err := copyEntry(from, name)
		copied += n
		if err != nil {
			errs = append(errs, err)
		}
	}
	return copied, errors.Join(errs...)
}

// copyEntry copies one entry, and the chunk entries a chunked account secret
// points at, when the current service lacks it.
func copyEntry(from, name string) (int, error) {
	if _, err := keyring.Get(service, name); err == nil {
		return 0, nil
	} else if !errors.Is(err, keyring.ErrNotFound) {
		return 0, fmt.Errorf("credentials: look up %s: %w", name, err)
	}
	value, err := keyring.Get(from, name)
	if errors.Is(err, keyring.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("credentials: read %s under %s: %w", name, from, err)
	}
	copied := 0
	if n, ok := chunkCount(value); ok {
		for i := range n {
			chunk := name + "." + strconv.Itoa(i)
			part, err := keyring.Get(from, chunk)
			if err != nil {
				return copied, fmt.Errorf("credentials: read chunk %d of %s under %s: %w", i, name, from, err)
			}
			if err := keyring.Set(service, chunk, part); err != nil {
				return copied, fmt.Errorf("credentials: write chunk %d of %s: %w", i, name, err)
			}
			copied++
		}
	}
	if err := keyring.Set(service, name, value); err != nil {
		return copied, fmt.Errorf("credentials: write %s: %w", name, err)
	}
	return copied + 1, nil
}
