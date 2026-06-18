package runtime

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/workspace"
)

type runtimeEnv struct {
	projectRoot   string
	projectIsRepo bool
	values        map[string]string
	lookupEnv     func(string) (string, bool)
}

func loadRuntimeEnv(startDir string, lookupEnv func(string) (string, bool)) runtimeEnv {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}

	rootBase := strings.TrimSpace(startDir)
	if explicitRoot, ok := lookupEnv(ProjectRootEnvVar); ok {
		rootBase = strings.TrimSpace(explicitRoot)
	}
	root := workspace.DetectRoot(rootBase)
	values := map[string]string{}
	if root.Path != "" {
		if parsed, err := workspace.ParseDotEnvFile(filepath.Join(root.Path, workspace.DotEnvFileName)); err == nil {
			values = parsed
		}
	}

	return runtimeEnv{
		projectRoot:   root.Path,
		projectIsRepo: root.IsRepo,
		values:        values,
		lookupEnv:     lookupEnv,
	}
}

func runtimeEnvGetenv(startDir string, lookupEnv func(string) (string, bool)) func(string) string {
	env := loadRuntimeEnv(startDir, lookupEnv)
	return env.Get
}

func (r runtimeEnv) Get(key string) string {
	if r.lookupEnv != nil {
		if value, ok := r.lookupEnv(key); ok {
			return strings.TrimSpace(value)
		}
	}
	return strings.TrimSpace(r.values[key])
}
