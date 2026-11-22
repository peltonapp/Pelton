package storage

import (
	"context"
	"strings"
	"testing"
)

func TestRemoteIDsNeedingBodyNewestUsesBodyIndex(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	query, args := remoteIDsNeedingBodyNewestQuery(1, 50, 20)
	rows, err := db.sql.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "idx_messages_folder_body_date") {
		t.Fatalf("plan does not use idx_messages_folder_body_date:\n%s", joined)
	}
	if strings.Contains(joined, "TEMP B-TREE") {
		t.Fatalf("plan sorts in a temp b-tree:\n%s", joined)
	}
}
