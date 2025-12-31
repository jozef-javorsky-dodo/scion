package agent

import (
	"os"
	"testing"

	"github.com/ptone/scion-agent/pkg/api"
)

func TestBuildAgentEnv(t *testing.T) {
	// Setup host env for inheritance test
	os.Setenv("INHERITED_KEY", "inherited-value")
	defer os.Unsetenv("INHERITED_KEY")

	scionCfg := &api.ScionConfig{
		Env: map[string]string{
			"NORMAL_KEY":    "normal-value",
			"INHERITED_KEY": "",
		},
	}

	extraEnv := map[string]string{
		"EXTRA_KEY": "extra-value",
	}

	env := buildAgentEnv(scionCfg, extraEnv)

	expected := map[string]string{
		"NORMAL_KEY":    "normal-value",
		"INHERITED_KEY": "inherited-value",
		"EXTRA_KEY":     "extra-value",
	}

	if len(env) != len(expected) {
		t.Errorf("expected %d env vars, got %d: %v", len(expected), len(env), env)
	}

	envMap := make(map[string]string)
	for _, e := range env {
		parts := (func(s string) []string {
			for i := 0; i < len(s); i++ {
				if s[i] == '=' {
					return []string{s[:i], s[i+1:]}
				}
			}
			return []string{s}
		})(e)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	for k, v := range expected {
		if envMap[k] != v {
			t.Errorf("expected env[%s] = %q, got %q", k, v, envMap[k])
		}
	}
}
