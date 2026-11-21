package storage

import (
	"database/sql"
	"encoding/json"
)

func encodeThreadingIDs(ids []string) any {
	if ids == nil {
		return nil
	}
	// A string slice always marshals successfully.
	value, _ := json.Marshal(ids)
	return string(value)
}

func decodeThreadingIDs(value sql.NullString) []string {
	if !value.Valid {
		return nil
	}
	var elements []any
	if err := json.Unmarshal([]byte(value.String), &elements); err != nil || elements == nil {
		return nil
	}
	ids := make([]string, len(elements))
	for i, element := range elements {
		id, ok := element.(string)
		if !ok {
			return nil
		}
		ids[i] = id
	}
	return ids
}
