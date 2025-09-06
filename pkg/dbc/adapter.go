package dbc

import (
	"os"
	"path/filepath"

	cdbc "go.einride.tech/can/pkg/dbc"

	"github.com/cockroachdb/errors"
)

// ParseFile parses a DBC file using the can-go (go.einride.tech/can) parser
// and converts it into the local DBCFile structure used by the rest of
// the application. This replaces the previous ad-hoc parser while keeping
// downstream code unchanged.
func ParseFile(filename string) (*DBCFile, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, errors.Wrap(err, "read dbc file")
	}

	parser := cdbc.NewParser(filepath.Base(filename), data)
	if perr := parser.Parse(); perr != nil {
		return nil, errors.Wrap(perr, "parse dbc (can-go)")
	}

	file := parser.File()

	out := &DBCFile{
		Version:   "",
		Messages:  make(map[uint32]*Message),
		CanGoFile: file,
	}

	for _, def := range file.Defs {
		switch m := def.(type) {
		case *cdbc.MessageDef:
			msgID := uint32(m.MessageID)
			// Extended CAN ID handling:
			// can-go keeps the MSB to flag extended ID; for decoding purposes we mask like our previous logic.
			if uint64(m.MessageID)&0x80000000 != 0 {
				msgID = uint32(uint64(m.MessageID) & 0x1FFFFFFF)
			}

			newMsg := &Message{
				ID:       msgID,
				Name:     string(m.Name),
				Size:     int(m.Size),
				SendNode: string(m.Transmitter),
				Signals:  []Signal{},
				CanGoDef: m,
			}

			for _, s := range m.Signals {
				receivers := make([]string, 0, len(s.Receivers))
				for _, r := range s.Receivers {
					receivers = append(receivers, string(r))
				}

				newMsg.Signals = append(newMsg.Signals, Signal{
					Name:      string(s.Name),
					StartBit:  int(s.StartBit),
					BitLength: int(s.Size),
					// can-go: IsBigEndian == true means Motorola (big-endian) which we encode as 0
					// Intel (little-endian) becomes 1.
					ByteOrder: func() int {
						if s.IsBigEndian {
							return 0
						}
						return 1
					}(),
					IsSigned:  s.IsSigned,
					Scale:     s.Factor,
					Offset:    s.Offset,
					Min:       s.Minimum,
					Max:       s.Maximum,
					Unit:      s.Unit,
					Receivers: receivers,
				})
			}

			out.Messages[newMsg.ID] = newMsg
		case *cdbc.VersionDef:
			out.Version = m.Version
		default:
			// Ignore other definition types for now.
		}
	}

	return out, nil
}
