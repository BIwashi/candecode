package convert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BIwashi/candecode/pkg/cli"
	"github.com/BIwashi/candecode/pkg/dbc"
	mcapwriter "github.com/BIwashi/candecode/pkg/mcap"
	"github.com/BIwashi/candecode/pkg/pcapng"
	candecodeproto "github.com/BIwashi/candecode/pkg/proto"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type converter struct {
	dbcFile    string
	pcapngFile string
}

func NewCommand() *cobra.Command {
	s := &converter{
		dbcFile:    "",
		pcapngFile: "",
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

	input.Logger.Info("Starting PCAPNG to MCAP conversion",
		"dbc_file", s.dbcFile,
		"pcapng_file", s.pcapngFile,
	)

	// Open PCAPNG file
	logger.Info("Opening PCAPNG file...")
	pcapFile, err := os.Open(s.pcapngFile)
	if err != nil {
		return fmt.Errorf("failed to open PCAPNG file: %w", err)
	}
	defer pcapFile.Close() //nolint:errcheck

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

	// Prepare MCAP output path: /mcap/<pcapng-basename-with-.mcap>
	base := filepath.Base(s.pcapngFile)
	baseNoExt := strings.TrimSuffix(base, filepath.Ext(base))
	outDir := "mcap"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("failed to create mcap output dir: %w", err)
	}
	outPath := filepath.Join(outDir, baseNoExt+".mcap")
	logger.Info("Opening MCAP output file...", "path", outPath)
	mcapFile, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create MCAP file: %w", err)
	}
	defer mcapFile.Close() //nolint:errcheck

	mw, err := mcapwriter.NewWriter(mcapFile)
	if err != nil {
		return fmt.Errorf("failed to init MCAP writer: %w", err)
	}
	defer func() {
		_ = mw.Close()
	}()

	// Process frames
	logger.Info("Converting CAN frames...")
	var (
		frameCount    = 0
		messageCount  = 0
		signalRecords = 0
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

		decodedSignals, err := decoder.Decode(frame)
		if err != nil {
			// Skip frames that can't be decoded (unknown message, shape mismatch, etc.)
			continue
		}

		// Retrieve message descriptor for message name & units
		msgDesc, ok := compiler.Message(frame.ID)
		messageName := fmt.Sprintf("0x%X", frame.ID)
		if ok {
			messageName = msgDesc.Name
		}

		// For each signal produce one DecodedSignal proto and write to MCAP
		for sigName, sig := range decodedSignals {
			ds := &candecodeproto.DecodedSignal{
				MessageName: messageName,
				Name:        sigName,
				Timestamp:   timestamppb.New(sig.Timestamp),
				CanId:       frame.ID,
				IsExtended:  frame.IsExtended,
				FrameBytes:  make([]byte, frame.Length),
				Signal: &candecodeproto.Signal{
					Name:             sig.Signal.Name,
					Start:            uint32(sig.Signal.Start),
					Length:           uint32(sig.Signal.Length),
					IsBigEndian:      sig.Signal.IsBigEndian,
					IsSigned:         sig.Signal.IsSigned,
					IsFloat:          sig.Signal.IsFloat,
					IsMultiplexer:    sig.Signal.IsMultiplexer,
					IsMultiplexed:    sig.Signal.IsMultiplexed,
					MultiplexerValue: uint32(sig.Signal.MultiplexerValue),
					Offset:           sig.Signal.Offset,
					Scale:            sig.Signal.Scale,
					Min:              sig.Signal.Min,
					Max:              sig.Signal.Max,
					Unit:             sig.Signal.Unit,
					Description:      sig.Signal.Description,
					DefaultValue:     int32(sig.Signal.DefaultValue),
					SourceFile:       compiler.SourceFile(),
				},
			}
			// ValueDescriptions
			for _, vd := range sig.Signal.ValueDescriptions {
				ds.Signal.ValueDescriptions = append(ds.Signal.ValueDescriptions, &candecodeproto.ValueDescription{
					Value:       vd.Value,
					Description: vd.Description,
				})
			}
			// Receiver nodes
			for _, rn := range sig.Signal.ReceiverNodes {
				ds.Signal.ReceiverNodes = append(ds.Signal.ReceiverNodes, rn)
			}

			// Physical
			if sig.Physical != nil {
				ds.Physical = sig.Physical
			}
			// Description (value description matched)
			if sig.Description != "" {
				ds.Description = sig.Description
			}

			// Raw oneof
			switch v := sig.Raw.(type) {
			case bool:
				ds.Raw = &candecodeproto.DecodedSignal_RawB{RawB: v}
			case int64:
				ds.Raw = &candecodeproto.DecodedSignal_RawS{RawS: v}
			case uint64:
				ds.Raw = &candecodeproto.DecodedSignal_RawU{RawU: v}
			case float64:
				ds.Raw = &candecodeproto.DecodedSignal_RawF{RawF: v}
			case []byte:
				ds.Raw = &candecodeproto.DecodedSignal_RawBytes{RawBytes: v}
			default:
				// Fallback: skip if unknown type
				continue
			}

			copy(ds.FrameBytes, frame.Data[:frame.Length])

			if err := mw.WriteDecodedSignal(ds); err != nil {
				logger.Error("failed to write decoded signal", "error", err, "signal", sigName)
				continue
			}
			signalRecords++
		}

		frameCount++
		if ok {
			messageCount++
		}

		if frameCount%1000 == 0 {
			logger.Info("Progress", "frames", frameCount, "signals", signalRecords)
		}
	}

	logger.Info("Conversion complete",
		"frames", frameCount,
		"messages_decoded", messageCount,
		"signals_written", signalRecords,
		"output_mcap", outPath,
	)

	return nil
}
