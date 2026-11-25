package backup_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// writeFakeRestic writes an executable fake restic script and returns its path.
// The write happens under syscall.ForkLock so no concurrent fork can inherit
// the open write fd; otherwise exec of the script fails with ETXTBSY
// ("text file busy", golang/go#22315).
func writeFakeRestic(t *testing.T, script string) string {
	t.Helper()
	fake := filepath.Join(t.TempDir(), "restic")
	syscall.ForkLock.Lock()
	err := os.WriteFile(fake, []byte(script), 0o755)
	syscall.ForkLock.Unlock()
	if err != nil {
		t.Fatalf("write fake restic: %v", err)
	}
	return fake
}
