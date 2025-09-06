package dbc

import (
"bufio"
"os"
"regexp"
"strconv"
"strings"

cdbc "go.einride.tech/can/pkg/dbc"
"github.com/cockroachdb/errors"
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
	Name       string
	StartBit   int
	BitLength  int
	ByteOrder  int // 0 = Motorola (big-endian), 1 = Intel (little-endian)
	IsSigned   bool
	Scale      float64
	Offset     float64
	Min        float64
	Max        float64
	Unit       string
	Receivers  []string
}

// DBCFile represents a parsed DBC file
type DBCFile struct {
Messages  map[uint32]*Message // Map of message ID to Message
Version   string
CanGoFile *cdbc.File // reference to original can-go parsed file (nil if legacy parser used)
}

 // ParseDBCFile parses a DBC file and returns a DBCFile structure.
 // DEPRECATED: Prefer using ParseFile (adapter over go.einride.tech/can) for all new code.
 // This legacy parser is kept temporarily for reference and will be removed once all
 // callers have migrated to ParseFile.
func ParseDBCFile(filename string) (*DBCFile, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open DBC file")
	}
	defer file.Close()

	dbc := &DBCFile{
		Messages: make(map[uint32]*Message),
	}

	scanner := bufio.NewScanner(file)
	var currentMessage *Message

	// Regular expressions for parsing
	messageRegex := regexp.MustCompile(`^BO_ (\d+) (\w+): (\d+) (\w+)`)
	signalRegex := regexp.MustCompile(`^\s*SG_ (\w+) : (\d+)\|(\d+)@(\d+)([\+\-]) \(([^,]+),([^)]+)\) \[([^\]]*)\] "([^"]*)" (.*)`)
	versionRegex := regexp.MustCompile(`^VERSION\s+"([^"]*)"`)

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		if line == "" || strings.HasPrefix(line, "CM_") {
			continue
		}

		// Parse VERSION
		if matches := versionRegex.FindStringSubmatch(line); matches != nil {
			dbc.Version = matches[1]
			continue
		}

		// Parse message definition
		if matches := messageRegex.FindStringSubmatch(line); matches != nil {
			id, _ := strconv.ParseUint(matches[1], 10, 32)
			size, _ := strconv.Atoi(matches[3])
			
			currentMessage = &Message{
				ID:       uint32(id),
				Name:     matches[2],
				Size:     size,
				SendNode: matches[4],
				Signals:  []Signal{},
			}
			dbc.Messages[uint32(id)] = currentMessage
			continue
		}

		// Parse signal definition
		if matches := signalRegex.FindStringSubmatch(line); matches != nil && currentMessage != nil {
			startBit, _ := strconv.Atoi(matches[2])
			bitLength, _ := strconv.Atoi(matches[3])
			byteOrder, _ := strconv.Atoi(matches[4])
			isSigned := matches[5] == "-"
			scale, _ := strconv.ParseFloat(matches[6], 64)
			offset, _ := strconv.ParseFloat(matches[7], 64)
			
			// Parse min/max range
			var min, max float64
			if matches[8] != "" {
				rangeParts := strings.Split(matches[8], "|")
				if len(rangeParts) == 2 {
					min, _ = strconv.ParseFloat(rangeParts[0], 64)
					max, _ = strconv.ParseFloat(rangeParts[1], 64)
				}
			}

			receivers := strings.Fields(matches[10])
			
			signal := Signal{
				Name:      matches[1],
				StartBit:  startBit,
				BitLength: bitLength,
				ByteOrder: byteOrder,
				IsSigned:  isSigned,
				Scale:     scale,
				Offset:    offset,
				Min:       min,
				Max:       max,
				Unit:      matches[9],
				Receivers: receivers,
			}
			
			currentMessage.Signals = append(currentMessage.Signals, signal)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to read DBC file")
	}

	return dbc, nil
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
	
	// Ensure it doesn't start with a number
	if matched, _ := regexp.MatchString(`^\d`, name); matched {
		name = "_" + name
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
