package convert

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/BIwashi/candecode/pkg/can"
	"github.com/BIwashi/candecode/pkg/cli"
	"github.com/BIwashi/candecode/pkg/dbc"
	"github.com/BIwashi/candecode/pkg/mcap"
	"github.com/BIwashi/candecode/pkg/pcapng"
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
		Long: `Convert PCAPNG files captured from CAN bus to MCAP format.
		
This command reads CAN frames from a PCAPNG file, decodes them using a DBC file,
and writes the decoded messages to an MCAP file with protobuf schema.`,
		Example: `  # Convert PCAPNG to MCAP
  candecode convert --dbc-file toyota.dbc --pcapng-file capture.pcapng --mcap-file output.mcap`,
		RunE: cli.WithContext(s.run),
	}

	cmd.Flags().StringVar(&s.dbcFile, "dbc-file", s.dbcFile, "DBC file")
	cmd.Flags().StringVar(&s.pcapngFile, "pcapng-file", s.pcapngFile, "PCAPNG file")
	cmd.Flags().StringVar(&s.mcapFile, "mcap-file", s.mcapFile, "MCAP file")

	cmd.MarkFlagRequired("dbc-file")
	cmd.MarkFlagRequired("pcapng-file")
	cmd.MarkFlagRequired("mcap-file")

	return cmd
}

func (s *converter) run(ctx context.Context, input cli.Input) error {
	input.Logger.Info("Starting PCAPNG to MCAP conversion",
		"dbc_file", s.dbcFile,
		"pcapng_file", s.pcapngFile,
		"mcap_file", s.mcapFile,
	)

	// Parse DBC file
	input.Logger.Info("Parsing DBC file...")
	dbcData, err := dbc.ParseFile(s.dbcFile)
	if err != nil {
		return fmt.Errorf("failed to parse DBC file: %w", err)
	}
	input.Logger.Info(fmt.Sprintf("Found %d messages in DBC file", len(dbcData.Messages)))

	// Open PCAPNG file
	input.Logger.Info("Opening PCAPNG file...")
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

	// Create MCAP file
	input.Logger.Info("Creating MCAP file...")
	mcapOutFile, err := os.Create(s.mcapFile)
	if err != nil {
		return fmt.Errorf("failed to create MCAP file: %w", err)
	}
	defer mcapOutFile.Close()

	// Create MCAP writer (with empty proto schema, will be generated from DBC)
	writer, err := mcap.NewWriter(mcapOutFile, dbcData, []byte{})
	if err != nil {
		return fmt.Errorf("failed to create MCAP writer: %w", err)
	}
	defer writer.Close()

	// Create CAN decoder
	decoder := can.NewDecoder(dbcData)

	// Process frames
	input.Logger.Info("Converting CAN frames...")
	frameCount := 0
	messageCount := 0
	skippedCount := 0
	outOfRangeCount := 0
	startTime := time.Now()
	msgCounts := make(map[uint32]int)

	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return fmt.Errorf("conversion cancelled: %w", ctx.Err())
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

		frameCount++

		// Decode CAN frame
		decodedMsg, err := decoder.DecodeFrame(frame)
		if err != nil {
			// Skip frames that can't be decoded (unknown message IDs)
			skippedCount++
			continue
		}

		// Write to MCAP
		timestamp := frame.Timestamp

		// Out-of-range validation (no clamp) against DBC metadata
		if dbcMsg, ok := dbcData.GetMessage(decodedMsg.MessageID); ok {
			for _, sigDef := range dbcMsg.Signals {
				if sv, ok := decodedMsg.Signals[sigDef.Name]; ok {
					if sigDef.Min != 0 || sigDef.Max != 0 {
						if sv.PhysicalValue < sigDef.Min || sv.PhysicalValue > sigDef.Max {
							outOfRangeCount++
							input.Logger.Debug("signal_out_of_range",
								"can_id", fmt.Sprintf("0x%03X", decodedMsg.MessageID),
								"message", decodedMsg.MessageName,
								"signal", sigDef.Name,
								"value", sv.PhysicalValue,
								"min", sigDef.Min,
								"max", sigDef.Max,
							)
						}
					}
				}
			}
		}

		err = writer.WriteMessage(decodedMsg, timestamp)
		if err != nil {
			return fmt.Errorf("failed to write message: %w", err)
		}

		messageCount++
		msgCounts[decodedMsg.MessageID]++

		// Progress reporting every 10000 frames
		if frameCount%10000 == 0 {
			input.Logger.Info(fmt.Sprintf("Progress: %d frames processed, %d messages decoded, %d skipped",
				frameCount, messageCount, skippedCount))
		}
	}

	duration := time.Since(startTime)

	// Print summary
	input.Logger.Info("Conversion completed successfully!",
		"total_frames", frameCount,
		"decoded_messages", messageCount,
		"skipped_frames", skippedCount,
		"out_of_range_signals", outOfRangeCount,
		"output_file", s.mcapFile,
		"duration", duration,
		"rate_fps", fmt.Sprintf("%.2f", float64(frameCount)/duration.Seconds()),
	)

	// Print message breakdown
	if len(msgCounts) > 0 {
		input.Logger.Info(fmt.Sprintf("Found %d unique message types", len(msgCounts)))
		for msgID, count := range msgCounts {
		if msg, ok := dbcData.GetMessage(msgID); ok {
			input.Logger.Debug(fmt.Sprintf("  0x%03X (%s): %d messages", msgID, msg.Name, count))
		} else {
			input.Logger.Debug(fmt.Sprintf("  0x%03X: %d messages", msgID, count))
		}
		}
	}

	return nil
}
