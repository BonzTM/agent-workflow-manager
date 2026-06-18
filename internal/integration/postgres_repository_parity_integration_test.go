//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	postgresrepo "github.com/bonztm/agent-workflow-manager/internal/adapters/postgres"
	"github.com/bonztm/agent-workflow-manager/internal/testutil/repositorycontract"
)

func TestPostgresRepositoryParity(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(integrationDSNEnvVar))
	if dsn == "" {
		t.Skipf("%s is required", integrationDSNEnvVar)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	repo, err := postgresrepo.New(ctx, postgresrepo.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("new postgres repository: %v", err)
	}
	t.Cleanup(repo.Close)

	repositorycontract.RunRepositoryParity(t, repositorycontract.ContractConfig{
		BackendLabel:        "postgres",
		ProjectID:           fmt.Sprintf("project.postgres.%d", time.Now().UTC().UnixNano()),
		Repo:                repo,
		IncludeServiceFlows: true,
	})
}
