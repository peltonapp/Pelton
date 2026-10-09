package storage

import "testing"

func TestCheckUniqueVersions(t *testing.T) {
	tests := []struct {
		name    string
		in      []migration
		wantErr bool
	}{
		{name: "empty", in: nil},
		{name: "distinct", in: []migration{{version: 1, name: "0001_a.sql"}, {version: 2, name: "0002_b.sql"}}},
		{name: "duplicate", in: []migration{{version: 35, name: "0035_a.sql"}, {version: 35, name: "0035_b.sql"}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkUniqueVersions(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkUniqueVersions() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestEmbeddedMigrationsHaveUniqueVersions guards against two branches each
// adding the same migration number and the merge keeping both files.
func TestEmbeddedMigrationsHaveUniqueVersions(t *testing.T) {
	if _, err := loadMigrations(); err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
}
