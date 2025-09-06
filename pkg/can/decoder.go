package can

import (
	"fmt"
	"math"

	"github.com/BIwashi/candecode/pkg/dbc"
	"github.com/BIwashi/candecode/pkg/pcapng"
	"github.com/cockroachdb/errors"
)

// DecodedMessage represents a decoded CAN message with all signal values
type DecodedMessage struct {
	MessageName string
	MessageID   uint32
	RawData     []byte
	TimestampNs uint64
	Signals     map[string]SignalValue
}

// SignalValue contains both raw and physical values of a signal
type SignalValue struct {
	Name          string
	RawValue      uint64
	PhysicalValue float64
	Unit          string
}

// Decoder decodes CAN frames using DBC information
type Decoder struct {
	dbcFile *dbc.DBCFile
}

// NewDecoder creates a new CAN decoder
func NewDecoder(dbcFile *dbc.DBCFile) *Decoder {
	return &Decoder{
		dbcFile: dbcFile,
	}
}

// DecodeFrame decodes a CAN frame into a DecodedMessage
func (d *Decoder) DecodeFrame(frame *pcapng.CANFrame) (*DecodedMessage, error) {
	// Get message definition from DBC
	message, ok := d.dbcFile.GetMessage(frame.CanID)
	if !ok {
		// Unknown message ID
		return nil, fmt.Errorf("unknown CAN ID: 0x%X", frame.CanID)
	}

	decoded := &DecodedMessage{
		MessageName: message.Name,
		MessageID:   frame.CanID,
		RawData:     frame.Data,
		TimestampNs: frame.TimestampNs,
		Signals:     make(map[string]SignalValue),
	}

	// Decode each signal
	for _, signal := range message.Signals {
		rawValue, err := extractSignalValue(frame.Data, &signal)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to extract signal %s", signal.Name)
		}

		// Apply scale and offset to get physical value
		physicalValue := float64(rawValue)*signal.Scale + signal.Offset

		decoded.Signals[signal.Name] = SignalValue{
			Name:          signal.Name,
			RawValue:      rawValue,
			PhysicalValue: physicalValue,
			Unit:          signal.Unit,
		}
	}

	return decoded, nil
}

// extractSignalValue extracts the raw value of a signal from CAN data
func extractSignalValue(data []byte, signal *dbc.Signal) (uint64, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("empty data")
	}

	var value uint64

	if signal.ByteOrder == 1 { // Intel (Little-endian)
		value = extractIntelSignal(data, signal.StartBit, signal.BitLength)
	} else { // Motorola (Big-endian)
		value = extractMotorolaSignal(data, signal.StartBit, signal.BitLength)
	}

	// Handle signed values
	if signal.IsSigned && signal.BitLength < 64 {
		// Check if the sign bit is set
		signBit := uint64(1) << (signal.BitLength - 1)
		if value&signBit != 0 {
			// Extend the sign bit
			mask := (uint64(1) << signal.BitLength) - 1
			value |= ^mask
		}
	}

	return value, nil
}

// extractIntelSignal extracts a signal value in Intel byte order (little-endian)
func extractIntelSignal(data []byte, startBit, bitLength int) uint64 {
	var result uint64
	currentBit := 0

	for i := 0; i < bitLength; i++ {
		bitPosition := startBit + i
		byteIndex := bitPosition / 8
		bitIndex := bitPosition % 8

		if byteIndex >= len(data) {
			break
		}

		// Extract bit from data
		bit := (data[byteIndex] >> bitIndex) & 1
		if bit == 1 {
			result |= uint64(1) << currentBit
		}
		currentBit++
	}

	return result
}

// extractMotorolaSignal extracts a signal value in Motorola byte order (big-endian)
func extractMotorolaSignal(data []byte, startBit, bitLength int) uint64 {
	var result uint64

	// For Motorola byte order, calculate the actual start position
	// Start bit is given as the MSB position
	msb := startBit
	lsb := msb - bitLength + 1

	// Handle negative LSB (spans across byte boundaries)
	if lsb < 0 {
		// Signal spans multiple bytes
		for i := 0; i < bitLength; i++ {
			bitPos := msb - i
			if bitPos < 0 {
				continue
			}

			byteIndex := bitPos / 8
			bitIndex := 7 - (bitPos % 8) // Motorola uses MSB first

			if byteIndex >= len(data) {
				continue
			}

			// Extract bit from data
			bit := (data[byteIndex] >> bitIndex) & 1
			if bit == 1 {
				result |= uint64(1) << (bitLength - 1 - i)
			}
		}
	} else {
		// Signal within byte boundaries
		startByte := msb / 8
		endByte := lsb / 8

		// Extract bits
		for byteIdx := startByte; byteIdx >= endByte && byteIdx >= 0; byteIdx-- {
			if byteIdx >= len(data) {
				continue
			}

			for bitIdx := 7; bitIdx >= 0; bitIdx-- {
				bitPos := byteIdx*8 + (7 - bitIdx)
				if bitPos > msb || bitPos < lsb {
					continue
				}

				bit := (data[byteIdx] >> bitIdx) & 1
				if bit == 1 {
					shiftAmount := bitPos - lsb
					result |= uint64(1) << shiftAmount
				}
			}
		}
	}

	return result
}

// ApplyScaleOffset applies scale and offset to convert raw value to physical value
func ApplyScaleOffset(rawValue uint64, scale, offset float64, isSigned bool, bitLength int) float64 {
	var value float64

	if isSigned {
		// Convert to signed integer
		signBit := uint64(1) << (bitLength - 1)
		if rawValue&signBit != 0 {
			// Negative value
			mask := (uint64(1) << bitLength) - 1
			signedValue := int64(rawValue | ^mask)
			value = float64(signedValue)
		} else {
			value = float64(rawValue)
		}
	} else {
		value = float64(rawValue)
	}

	return value*scale + offset
}

// ValidatePhysicalValue checks if the physical value is within the specified range
func ValidatePhysicalValue(value, min, max float64) bool {
	// If min and max are both 0, no range checking
	if min == 0 && max == 0 {
		return true
	}

	// Allow for some floating point tolerance
	const epsilon = 1e-9
	return value >= (min-epsilon) && value <= (max+epsilon)
}

// FormatSignalValue formats a signal value with its unit
func FormatSignalValue(value float64, unit string) string {
	// Format based on value magnitude
	formatted := ""
	absValue := math.Abs(value)

	if absValue == 0 {
		formatted = "0"
	} else if absValue >= 1000 || absValue < 0.01 {
		formatted = fmt.Sprintf("%.3e", value)
	} else if absValue >= 100 {
		formatted = fmt.Sprintf("%.1f", value)
	} else if absValue >= 10 {
		formatted = fmt.Sprintf("%.2f", value)
	} else {
		formatted = fmt.Sprintf("%.3f", value)
	}

	if unit != "" {
		return fmt.Sprintf("%s %s", formatted, unit)
	}
	return formatted
}
