package sqlite

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	storagedomain "github.com/bonztm/agent-workflow-manager/internal/storage/domain"
)

func normalizePhase(value string) string {
	return storagedomain.NormalizePhase(value)
}

func normalizeLimit(v int, fallback int) int {
	if v <= 0 {
		v = fallback
	}
	if v > maxQueryLimit {
		v = maxQueryLimit
	}
	return v
}

func normalizeStringList(values []string) []string {
	return storagedomain.NormalizeStringList(values)
}

func newReceiptID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate receipt id: %w", err)
	}
	return fmt.Sprintf("receipt-%d-%s", time.Now().UTC().UnixNano(), hex.EncodeToString(b[:])), nil
}
