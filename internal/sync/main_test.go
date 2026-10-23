package sync

import (
	"os"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage/storagetest"
)

// TestMain removes the migrated database the test stores are copied from.
func TestMain(m *testing.M) {
	os.Exit(storagetest.Main(m))
}
