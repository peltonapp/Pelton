package credentials

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/zalando/go-keyring"
)

// calendarPrefix namespaces a calendar account's keyring entry, apart from
// mail accounts and address books with the same numeric id.
const calendarPrefix = "calendar-"

// StoreCalendarPassword saves the login for a CalDAV calendar account, or
// clears it when empty. Like every other secret it stays out of the settings
// db.
func StoreCalendarPassword(accountID int64, password string) error {
	if password == "" {
		return DeleteCalendarPassword(accountID)
	}
	remember(password)
	if err := keyring.Set(service, calendarKey(accountID), password); err != nil {
		return fmt.Errorf("credentials: store calendar account %d password: %w", accountID, err)
	}
	return nil
}

// LoadCalendarPassword returns a calendar account's stored password, or ""
// when none is set.
func LoadCalendarPassword(accountID int64) (string, error) {
	raw, err := keyring.Get(service, calendarKey(accountID))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("credentials: load calendar account %d password: %w", accountID, err)
	}
	remember(raw)
	return raw, nil
}

// DeleteCalendarPassword removes a calendar account's password. A missing
// entry is not an error.
func DeleteCalendarPassword(accountID int64) error {
	err := keyring.Delete(service, calendarKey(accountID))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("credentials: delete calendar account %d password: %w", accountID, err)
	}
	return nil
}

func calendarKey(accountID int64) string {
	return calendarPrefix + strconv.FormatInt(accountID, 10)
}
