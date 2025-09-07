package dbc

import (
	"regexp"
	"strings"

	cdbc "go.einride.tech/can/pkg/dbc"
)

// Message represents a CAN message from DBC file
type Message struct {
	ID       uint32
	Name     string
	Size     int
	Signals  []Signal
	SendNode string
	CanGoDef *cdbc.MessageDef // populated when parsed via can-go adapter
}

// Signal represents a signal within a CAN message
type Signal struct {
	Name      string
	StartBit  int
	BitLength int
	ByteOrder int // 0 = Motorola (big-endian), 1 = Intel (little-endian)
	IsSigned  bool
	Scale     float64
	Offset    float64
	Min       float64
	Max       float64
	Unit      string
	Receivers []string
}

// DBCFile represents a parsed DBC file
type DBCFile struct {
	Messages  map[uint32]*Message // Map of message ID to Message
	Version   string
	CanGoFile *cdbc.File // reference to original can-go parsed file (nil if legacy parser used)
}

// GetMessage returns a message by ID
func (d *DBCFile) GetMessage(id uint32) (*Message, bool) {
	msg, ok := d.Messages[id]
	return msg, ok
}

// GetMessageByName returns a message by name
func (d *DBCFile) GetMessageByName(name string) (*Message, bool) {
	for _, msg := range d.Messages {
		if msg.Name == name {
			return msg, true
		}
	}
	return nil, false
}

// ToProtoFieldName converts a signal name to a valid protobuf field name
func ToProtoFieldName(signalName string) string {
	// Convert to lowercase and replace invalid characters with underscore
	name := strings.ToLower(signalName)
	name = regexp.MustCompile(`[^a-z0-9_]+`).ReplaceAllString(name, "_")

	// Handle underscore prefix - replace with "field_"
	if strings.HasPrefix(name, "_") {
		name = "field" + name
	}

	// Ensure it doesn't start with a number
	if matched, _ := regexp.MatchString(`^\d`, name); matched {
		name = "field_" + name
	}

	return name
}

// ToProtoMessageName converts a message name to a valid protobuf message name
func ToProtoMessageName(messageName string) string {
	// Convert to CamelCase
	parts := strings.FieldsFunc(messageName, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})

	for i, part := range parts {
		parts[i] = strings.Title(strings.ToLower(part))
	}

	return strings.Join(parts, "")
}

// GetProtoType returns the protobuf type for a signal
func (s *Signal) GetProtoType() string {
	// If it's a floating point scale, use double
	if s.Scale != 0 && s.Scale != 1 {
		return "double"
	}

	// For integer values
	if s.IsSigned {
		if s.BitLength <= 32 {
			return "int32"
		}
		return "int64"
	}

	if s.BitLength <= 32 {
		return "uint32"
	}
	return "uint64"
}
