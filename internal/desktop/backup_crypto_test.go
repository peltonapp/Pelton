package desktop

import "testing"

// Tests may lower backupScryptN for a round trip; a build never does. A
// backup sealed with a cheaper N would open only in a build with the same N.
func TestBackupKDFCostIsTheExportCost(t *testing.T) {
	if backupScryptN != scryptN || scryptN != 1<<15 {
		t.Fatalf("backup scrypt N = %d (scryptN %d), want 2^15", backupScryptN, scryptN)
	}
}
