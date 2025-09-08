package gen

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
)

// runCantool copies the provided DBC file into can_gen/dbc and runs:
//
//	go tool cantool generate can_gen/dbc can_gen/out
//
// Requirements (from spec):
// - Do NOT clean existing outputs.
// - Always copy the specified DBC file (overwrite if already exists).
// - Use libraries / external tool (cantool) instead of reimplementing logic.
func runCantool(ctx context.Context, dbcPath string, logger slog.Logger) (string, error) {
	source := dbcPath
	base := filepath.Base(source)

	// Derive package name: remove extension then strip '_' and '+'
	nameNoExt := base[:len(base)-len(filepath.Ext(base))]
	packageName := strings.NewReplacer("_", "", "+", "").Replace(nameNoExt)

	// Directories
	dbcDir := "pkg/dbc"
	outDir := filepath.Join("pkg/dbc/generated", packageName)

	// Ensure directories exist
	if err := os.MkdirAll(dbcDir, 0o755); err != nil {
		return "", errors.Wrap(err, "create pkg/dbc/dbc directory")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", errors.Wrap(err, "create pkg/dbc/out directory")
	}

	dest := filepath.Join(dbcDir, base)
	logger.Info("Preparing DBC file for cantool",
		"src", source,
		"dest", dest,
	)

	// Copy (overwrite) the DBC file
	if err := copyFile(source, dest); err != nil {
		return "", errors.Wrap(err, "copy dbc file")
	}

	logger.Info("Running cantool generate",
		"cmd", "go tool cantool generate can_gen/dbc can_gen/out",
	)

	cmd := exec.CommandContext(
		ctx,
		"go", "tool", "cantool", "generate", dbcDir, outDir,
	)
	// Inherit current working directory (repo root)
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		logger.Info("cantool output", "output", string(output))
	}
	if err != nil {
		return "", errors.Wrap(err, "cantool generate failed")
	}

	logger.Info("cantool code generation completed",
		"output_dir", outDir,
	)

	outFileName := base[:len(base)-len(filepath.Ext(base))] + ".dbc.go"

	return outFileName, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	// Ensure file is flushed
	if err := out.Sync(); err != nil {
		return err
	}

	return nil
}
