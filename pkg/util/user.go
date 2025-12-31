package util

import (
	"path/filepath"
)

// GetHomeDir returns the home directory for the given username.
// It handles "root" specifically as /root, and others as /home/<username>.
func GetHomeDir(username string) string {
	if username == "root" {
		return "/root"
	}
	return filepath.Join("/home", username)
}
