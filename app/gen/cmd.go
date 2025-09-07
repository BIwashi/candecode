package gen

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/BIwashi/candecode/pkg/cli"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

type generator struct {
	dbcFile   string
	protoFile string
	outputDir string
	bufPath   string
}

func NewCommand() *cobra.Command {
	s := &generator{
		dbcFile:   "",
		protoFile: "",
		outputDir: "generated/proto/v1",
		bufPath:   "./bin/buf",
	}

	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate proto file from dbc file.",
		RunE:  cli.WithContext(s.run),
	}

	cmd.Flags().StringVar(&s.dbcFile, "dbc-file", s.dbcFile, "DBC file path")
	cmd.Flags().StringVar(&s.protoFile, "proto-file", s.protoFile, "Output proto file path (optional, auto-generated if not specified)")
	cmd.Flags().StringVar(&s.outputDir, "output-dir", s.outputDir, "Output directory for proto file")
	cmd.Flags().StringVar(&s.bufPath, "buf-path", s.bufPath, "Buf binary path")

	if err := cmd.MarkFlagRequired("dbc-file"); err != nil {
		fmt.Printf("failed to mark flag as required, err: %v", err)

		return nil
	}

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

	if err := GenerateFromDBCFile(s.dbcFile, templatePath, s.outputDir, s.bufPath, input.Logger); err != nil {
		return errors.Wrap(err, "failed to generate proto file")
	}

	return nil
}
