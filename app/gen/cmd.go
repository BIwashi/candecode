package gen

import (
	"context"
	"path/filepath"

	"github.com/BIwashi/candecode/pkg/cli"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

type generator struct {
	dbcFile   string
	protoFile string
	outputDir string
}

func NewCommand() *cobra.Command {
	s := &generator{
		dbcFile:   "",
		protoFile: "",
		outputDir: "generated/proto",
	}

	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate proto file from dbc file.",
		RunE:  cli.WithContext(s.run),
	}

	cmd.Flags().StringVar(&s.dbcFile, "dbc-file", s.dbcFile, "DBC file path")
	cmd.Flags().StringVar(&s.protoFile, "proto-file", s.protoFile, "Output proto file path (optional, auto-generated if not specified)")
	cmd.Flags().StringVar(&s.outputDir, "output-dir", s.outputDir, "Output directory for proto file")

	cmd.MarkFlagRequired("dbc-file")

	return cmd
}

func (s *generator) run(ctx context.Context, input cli.Input) error {
	// Use default template path
	templatePath := "template/dbc_to_proto.tmpl"

	// If proto file is not specified, it will be auto-generated
	if s.protoFile != "" {
		// Use specified proto file path
		s.outputDir = filepath.Dir(s.protoFile)
	}

	input.Logger.Info("Generating proto file from DBC",
		"dbc_file", s.dbcFile,
		"output_dir", s.outputDir,
		"template", templatePath,
	)

	// Generate proto file
	if err := GenerateFromDBCFile(s.dbcFile, templatePath, s.outputDir); err != nil {
		return errors.Wrap(err, "failed to generate proto file")
	}

	return nil
}
