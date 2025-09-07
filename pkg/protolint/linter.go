package protolint

import (
	"bytes"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/cockroachdb/errors"
)

// Linter provides proto file linting functionality
type Linter struct {
	logger slog.Logger
}

// NewLinter creates a new Linter instance
func NewLinter(logger slog.Logger) *Linter {
	return &Linter{
		logger: logger,
	}
}

// LintResult contains the result of linting
type LintResult struct {
	Success  bool
	Messages []string
	Errors   []string
}

// LintWithBuf runs buf lint on the specified proto file or directory
func (l *Linter) LintWithBuf(path string) (*LintResult, error) {
	l.logger.Info("Running buf lint", "path", path)

	// Check if buf is installed
	if err := l.checkBufInstalled(); err != nil {
		return nil, errors.Wrap(err, "buf is not installed")
	}

	// Run buf lint
	cmd := exec.Command("buf", "lint", path)
	output, err := cmd.CombinedOutput()

	result := &LintResult{
		Success:  err == nil,
		Messages: []string{},
		Errors:   []string{},
	}

	// Parse output - buf outputs lint errors to stdout/stderr
	if len(output) > 0 {
		outputStr := strings.TrimSpace(string(output))
		if outputStr != "" {
			// Split by newline to get individual lint errors
			lines := strings.Split(outputStr, "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					result.Errors = append(result.Errors, line)
				}
			}
		}
	}

	if err != nil {
		// buf lint returns non-zero exit code when there are lint errors
		// Exit code 100 is used when lint errors are found
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			if exitCode == 100 || exitCode == 1 {
				l.logger.Warn("Proto file has lint issues", "issues", len(result.Errors))
				// This is expected behavior when lint errors are found
				// Don't return an error, just mark success as false
			} else {
				// Unexpected exit code
				return nil, errors.Wrapf(err, "failed to run buf lint (exit code: %d)", exitCode)
			}
		} else {
			return nil, errors.Wrap(err, "failed to run buf lint")
		}
	} else {
		l.logger.Info("Proto file passed lint check")
		result.Messages = append(result.Messages, "Proto file passed all lint checks")
	}

	return result, nil
}

// LintWithProtolint runs protolint on the specified proto file
func (l *Linter) LintWithProtolint(path string) (*LintResult, error) {
	l.logger.Info("Running protolint", "path", path)

	// Check if protolint is installed
	if err := l.checkProtolintInstalled(); err != nil {
		return nil, errors.Wrap(err, "protolint is not installed")
	}

	// Run protolint
	cmd := exec.Command("protolint", "lint", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &LintResult{
		Success:  err == nil,
		Messages: []string{},
		Errors:   []string{},
	}

	// Parse output
	if stdout.Len() > 0 {
		output := strings.TrimSpace(stdout.String())
		if output != "" {
			result.Errors = strings.Split(output, "\n")
		}
	}

	if stderr.Len() > 0 {
		errOutput := strings.TrimSpace(stderr.String())
		if errOutput != "" {
			result.Errors = append(result.Errors, strings.Split(errOutput, "\n")...)
		}
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			l.logger.Warn("Proto file has lint warnings (protolint)", "warnings", len(result.Errors))
		} else {
			return result, errors.Wrap(err, "failed to run protolint")
		}
	} else {
		l.logger.Info("Proto file passed protolint check")
		result.Messages = append(result.Messages, "Proto file passed all protolint checks")
	}

	return result, nil
}

// checkBufInstalled checks if buf is installed
func (l *Linter) checkBufInstalled() error {
	cmd := exec.Command("buf", "--version")
	if err := cmd.Run(); err != nil {
		return errors.New("buf is not installed. Please install it from https://docs.buf.build/installation")
	}
	return nil
}

// checkProtolintInstalled checks if protolint is installed
func (l *Linter) checkProtolintInstalled() error {
	cmd := exec.Command("protolint", "--version")
	if err := cmd.Run(); err != nil {
		return errors.New("protolint is not installed. Please install it from https://github.com/yoheimuta/protolint")
	}
	return nil
}

// FormatResult formats the lint result for display
func (r *LintResult) FormatResult() string {
	var output strings.Builder

	if r.Success {
		output.WriteString("Lint check passed\n")
		for _, msg := range r.Messages {
			output.WriteString(fmt.Sprintf("  %s\n", msg))
		}
	} else {
		output.WriteString("Lint check found issues:\n")
		for _, err := range r.Errors {
			output.WriteString(fmt.Sprintf("  %s\n", err))
		}
	}

	return output.String()
}

// Format formats a proto file using buf format
func (l *Linter) Format(path string) error {
	l.logger.Info("Formatting proto file with buf", "path", path)

	// Run buf format with -w flag to write changes
	cmd := exec.Command("buf", "format", "-w", path)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Check if it's an exit error
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			l.logger.Error("buf format failed",
				"exit_code", exitCode,
				"output", string(output))
			return errors.Wrapf(err, "buf format failed with exit code %d: %s", exitCode, string(output))
		}
		return errors.Wrap(err, "failed to run buf format")
	}

	if len(output) > 0 {
		l.logger.Debug("buf format output", "output", string(output))
	}

	l.logger.Info("Successfully formatted proto file")
	return nil
}
