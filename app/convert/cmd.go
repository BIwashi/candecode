package convert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BIwashi/candecode/pkg/cli"
	"github.com/BIwashi/candecode/pkg/dbc"
	"github.com/BIwashi/candecode/pkg/pcapng"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

type converter struct {
	dbcFile    string
	pcapngFile string
	mcapFile   string
}

func NewCommand() *cobra.Command {
	s := &converter{
		dbcFile:    "",
		pcapngFile: "",
		mcapFile:   "",
	}

	cmd := &cobra.Command{
		Use:   "convert",
		Short: "Convert CAN data captured with pcapng to MCAP using a DBC file.",
		Long: `
Convert PCAPNG files captured from CAN bus to MCAP format.

This command reads CAN frames from a PCAPNG file, decodes them using a DBC file,
and writes the decoded messages to an MCAP file with protobuf schema.`,
		Example: `
# Convert PCAPNG to MCAP
candecode convert --dbc-file toyota.dbc --pcapng-file capture.pcapng --mcap-file output.mcap`,
		RunE: cli.WithContext(s.run),
	}

	cmd.Flags().StringVar(&s.dbcFile, "dbc-file", s.dbcFile, "DBC file")
	cmd.Flags().StringVar(&s.pcapngFile, "pcapng-file", s.pcapngFile, "PCAPNG file")
	cmd.Flags().StringVar(&s.mcapFile, "mcap-file", s.mcapFile, "MCAP file (optional, defaults to PCAPNG filename with .mcap extension)")

	if err := cmd.MarkFlagRequired("dbc-file"); err != nil {
		fmt.Printf("failed to mark flag as required, err: %v", err)

		return nil
	}
	if err := cmd.MarkFlagRequired("pcapng-file"); err != nil {
		fmt.Printf("failed to mark flag as required, err: %v", err)

		return nil
	}

	return cmd
}

func (s *converter) run(ctx context.Context, input cli.Input) error {
	logger := input.Logger
	// Generate MCAP filename from PCAPNG filename if not specified
	if s.mcapFile == "" {
		// Get base filename without extension
		baseName := strings.TrimSuffix(filepath.Base(s.pcapngFile), filepath.Ext(s.pcapngFile))
		// Save to mcap/ directory with .mcap extension
		s.mcapFile = filepath.Join("mcap", baseName+".mcap")
	}

	input.Logger.Info("Starting PCAPNG to MCAP conversion",
		"dbc_file", s.dbcFile,
		"pcapng_file", s.pcapngFile,
		"mcap_file", s.mcapFile,
	)

	// // Parse DBC file
	// input.Logger.Info("Parsing DBC file...")
	// dbcData, err := dbc.ParseFile(s.dbcFile)
	// if err != nil {
	// 	return fmt.Errorf("failed to parse DBC file: %w", err)
	// }
	// input.Logger.Info(fmt.Sprintf("Found %d messages in DBC file", len(dbcData.Messages)))

	// Open PCAPNG file
	logger.Info("Opening PCAPNG file...")
	pcapFile, err := os.Open(s.pcapngFile)
	if err != nil {
		return fmt.Errorf("failed to open PCAPNG file: %w", err)
	}
	defer pcapFile.Close()

	// Create PCAPNG reader
	reader, err := pcapng.NewReader(pcapFile)
	if err != nil {
		return fmt.Errorf("failed to create PCAPNG reader: %w", err)
	}

	// Create DBC compiler
	compiler, err := dbc.NewCompiler(s.dbcFile)
	if err != nil {
		return fmt.Errorf("failed to create DBC compiler: %w", err)
	}
	decoder := dbc.NewDecoder(compiler)

	// Process frames
	logger.Info("Converting CAN frames...")
	var (
		frameCount = 0
	)

	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "conversion cancelled")
		default:
			// Continue processing
		}

		frame, err := reader.ReadFrame()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return fmt.Errorf("failed to read frame: %w", err)
		}

		decodedSignal, err := decoder.Decode(frame)
		if err != nil {
			// logger.Error("failed to decode frame", "error", err)
			continue
		}

		for _, signal := range decodedSignal {
			if signal.Physical != nil {
				fmt.Printf("%+v: %+v: %+v %+v\n", signal.Timestamp, signal.Signal.Name, *signal.Physical, signal.Signal.Unit)
			}
		}

		frameCount++
	}

	return nil
}
