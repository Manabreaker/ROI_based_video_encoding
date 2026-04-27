package roi

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runCommand executes a command while streaming its output to the terminal.
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}

	return nil
}

// commandOutput executes a command and returns stdout with stderr in errors.
func commandOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s failed: %w\n%s", name, err, strings.TrimSpace(stderr.String()))
	}

	return stdout.Bytes(), nil
}

// commandCombinedOutput executes a command and returns stdout and stderr together.
func commandCombinedOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}
