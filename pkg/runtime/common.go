package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/util"
)

// buildCommonRunArgs constructs the common arguments for 'run' command across different runtimes.
func buildCommonRunArgs(config RunConfig) ([]string, error) {
	args := []string{"run", "-d", "-i"}
	addArg := func(flag string, values ...string) {
		for _, v := range values {
			args = append(args, flag, v)
		}
	}
	addEnv := func(name, value string) {
		if value != "" {
			addArg("-e", fmt.Sprintf("%s=%s", name, value))
		}
	}

	hostHome, _ := os.UserHomeDir()
	expandPath := func(path string, isTarget bool) string {
		if strings.HasPrefix(path, "~/") {
			if isTarget {
				return filepath.Join(util.GetHomeDir(config.UnixUsername), path[2:])
			}
			return filepath.Join(hostHome, path[2:])
		}
		if path == "~" {
			if isTarget {
				return util.GetHomeDir(config.UnixUsername)
			}
			return hostHome
		}
		return path
	}

	addVolume := func(v api.VolumeMount) {
		src := expandPath(v.Source, false)
		tgt := expandPath(v.Target, true)
		val := fmt.Sprintf("%s:%s", src, tgt)
		if v.ReadOnly {
			val += ":ro"
		}
		addArg("-v", val)
	}

	addArg("--name", config.Name)

	if config.HomeDir != "" {
		addArg("-v", fmt.Sprintf("%s:%s", config.HomeDir, util.GetHomeDir(config.UnixUsername)))
	}
	if config.RepoRoot != "" && config.Workspace != "" {
		relWorkspace, err := filepath.Rel(config.RepoRoot, config.Workspace)
		if err == nil && !strings.HasPrefix(relWorkspace, "..") {
			// Mount .git
			addArg("-v", fmt.Sprintf("%s/.git:/repo-root/.git", config.RepoRoot))
			// Mount workspace at same relative path
			containerWorkspace := filepath.Join("/repo-root", relWorkspace)
			addArg("-v", fmt.Sprintf("%s:%s", config.Workspace, containerWorkspace))
			addArg("--workdir", containerWorkspace)
		} else {
			// Fallback if workspace is outside repo root
			addArg("-v", fmt.Sprintf("%s:/workspace", config.Workspace))
			addArg("--workdir", "/workspace")
		}
	} else if config.Workspace != "" {
		addArg("-v", fmt.Sprintf("%s:/workspace", config.Workspace))
		addArg("--workdir", "/workspace")
	}

	// Add generic volumes from config
	for _, v := range config.Volumes {
		addVolume(v)
	}

	// Use Harness for file propagation and env
	if config.Harness != nil {
		if config.HomeDir != "" {
			if err := config.Harness.PropagateFiles(config.HomeDir, config.UnixUsername, config.Auth); err != nil {
				return nil, err
			}
		} else {
			for _, v := range config.Harness.GetVolumes(config.UnixUsername, config.Auth) {
				addVolume(v)
			}
		}
		for k, v := range config.Harness.GetEnv(config.Name, config.UnixUsername, config.Auth) {
			addEnv(k, v)
		}
	}

	// Mount gcloud config if it exists
	home, _ := os.UserHomeDir()
	gcloudConfigDir := filepath.Join(home, ".config", "gcloud")
	if _, err := os.Stat(gcloudConfigDir); err == nil {
		addArg("-v", fmt.Sprintf("%s:/home/%s/.config/gcloud:ro", gcloudConfigDir, config.UnixUsername))
	}

	for _, e := range config.Env {
		addArg("-e", e)
	}

	if config.UseTmux {
		if config.Labels == nil {
			config.Labels = make(map[string]string)
		}
		config.Labels["scion.tmux"] = "true"
	}

	for k, v := range config.Labels {
		addArg("--label", fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range config.Annotations {
		addArg("--label", fmt.Sprintf("%s=%s", k, v))
	}
	if config.Template != "" {
		addArg("--label", fmt.Sprintf("scion.template=%s", config.Template))
	}

	args = append(args, config.Image)

	// Get command from harness
	var harnessArgs []string
	if config.Harness != nil {
		harnessArgs = config.Harness.GetCommand(config.Task, config.Resume, config.CommandArgs)
	} else {
		return nil, fmt.Errorf("no harness provided")
	}

	if config.UseTmux {
		var quotedArgs []string
		for _, a := range harnessArgs {
			// Use %q to quote arguments that might have spaces or special characters
			if strings.ContainsAny(a, " \t\n\"'$") {
				quotedArgs = append(quotedArgs, fmt.Sprintf("%q", a))
			} else {
				quotedArgs = append(quotedArgs, a)
			}
		}
		cmdLine := strings.Join(quotedArgs, " ")
		args = append(args, "tmux", "new-session", "-s", "scion", cmdLine)
	} else {
		args = append(args, harnessArgs...)
	}

	return args, nil
}

func runSimpleCommand(ctx context.Context, command string, args ...string) (string, error) {
	if os.Getenv("SCION_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "Debug: %s %s\n", command, strings.Join(args, " "))
	}
	cmd := exec.CommandContext(ctx, command, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s failed: %w", command, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func runInteractiveCommand(command string, args ...string) error {
	if os.Getenv("SCION_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "Debug: %s %s\n", command, strings.Join(args, " "))
	}
	cmd := exec.Command(command, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}