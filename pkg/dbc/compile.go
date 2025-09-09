package dbc

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/cockroachdb/errors"
	"go.einride.tech/can/pkg/dbc"
	"go.einride.tech/can/pkg/descriptor"
)

// Decoder decodes CAN frames using DBC information
type Compiler struct {
	db     *descriptor.Database
	defs   []dbc.Def
	errors []error
}

func NewCompiler(filePath string) (*Compiler, error) {
	dbcBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read dbc file")
	}

	p := dbc.NewParser(filePath, dbcBytes)
	if err := p.Parse(); err != nil {
		return nil, errors.Wrap(err, "failed to parse dbc file")
	}
	c := &Compiler{
		db:   &descriptor.Database{SourceFile: filePath},
		defs: p.Defs(),
	}

	c.collectDescriptors()
	c.addMetadata()
	c.sortDescriptors()

	return c, nil
}

/*
ref: https://github.com/einride/can-go/internal/generate/compile.go
*/
func (c *Compiler) collectDescriptors() {
	for _, def := range c.defs {
		switch def := def.(type) {
		case *dbc.VersionDef:
			c.db.Version = def.Version
		case *dbc.MessageDef:
			if def.MessageID == dbc.IndependentSignalsMessageID {
				continue // don't compile
			}
			message := &descriptor.Message{
				Name:       string(def.Name),
				ID:         def.MessageID.ToCAN(),
				IsExtended: def.MessageID.IsExtended(),
				Length:     uint8(def.Size),
				SenderNode: string(def.Transmitter),
			}
			for _, signalDef := range def.Signals {
				signal := &descriptor.Signal{
					Name:             string(signalDef.Name),
					IsBigEndian:      signalDef.IsBigEndian,
					IsSigned:         signalDef.IsSigned,
					IsMultiplexer:    signalDef.IsMultiplexerSwitch,
					IsMultiplexed:    signalDef.IsMultiplexed,
					MultiplexerValue: uint(signalDef.MultiplexerSwitch),
					Start:            uint8(signalDef.StartBit),
					Length:           uint8(signalDef.Size),
					Scale:            signalDef.Factor,
					Offset:           signalDef.Offset,
					Min:              signalDef.Minimum,
					Max:              signalDef.Maximum,
					Unit:             signalDef.Unit,
				}
				for _, receiver := range signalDef.Receivers {
					signal.ReceiverNodes = append(signal.ReceiverNodes, string(receiver))
				}
				message.Signals = append(message.Signals, signal)
			}
			c.db.Messages = append(c.db.Messages, message)
		case *dbc.NodesDef:
			for _, node := range def.NodeNames {
				c.db.Nodes = append(c.db.Nodes, &descriptor.Node{Name: string(node)})
			}
		}
	}
}

func (c *Compiler) addMetadata() {
	for _, def := range c.defs {
		switch def := def.(type) {
		case *dbc.SignalValueTypeDef:
			signal, ok := c.db.Signal(def.MessageID.ToCAN(), string(def.SignalName))
			if !ok {
				c.errors = append(c.errors, fmt.Errorf("no declared signal: %v", def))
				continue
			}
			switch def.SignalValueType {
			case dbc.SignalValueTypeInt:
				signal.IsFloat = false
			case dbc.SignalValueTypeFloat32:
				if signal.Length == 32 {
					signal.IsFloat = true
				} else {
					reason := fmt.Sprintf("incorrect float signal length: %d", signal.Length)
					c.errors = append(c.errors, errors.New(reason))
				}
			default:
				reason := fmt.Sprintf("unsupported signal value type: %v", def.SignalValueType)
				c.errors = append(c.errors, errors.New(reason))
			}
		case *dbc.CommentDef:
			switch def.ObjectType {
			case dbc.ObjectTypeMessage:
				if def.MessageID == dbc.IndependentSignalsMessageID {
					continue // don't compile
				}
				message, ok := c.db.Message(def.MessageID.ToCAN())
				if !ok {
					c.errors = append(c.errors, errors.New(fmt.Sprintf("no declared message: %v", def)))
					continue
				}
				message.Description = def.Comment
			case dbc.ObjectTypeSignal:
				if def.MessageID == dbc.IndependentSignalsMessageID {
					continue // don't compile
				}
				signal, ok := c.db.Signal(def.MessageID.ToCAN(), string(def.SignalName))
				if !ok {
					c.errors = append(c.errors, errors.New(fmt.Sprintf("no declared signal: %v", def)))
					continue
				}
				signal.Description = def.Comment
			case dbc.ObjectTypeNetworkNode:
				node, ok := c.db.Node(string(def.NodeName))
				if !ok {
					c.errors = append(c.errors, errors.New(fmt.Sprintf("no declared node: %v", def)))
					continue
				}
				node.Description = def.Comment
			}
		case *dbc.ValueDescriptionsDef:
			if def.MessageID == dbc.IndependentSignalsMessageID {
				continue // don't compile
			}
			if def.ObjectType != dbc.ObjectTypeSignal {
				continue // don't compile
			}
			signal, ok := c.db.Signal(def.MessageID.ToCAN(), string(def.SignalName))
			if !ok {
				c.errors = append(c.errors, errors.New(fmt.Sprintf("no declared signal: %v", def)))
				continue
			}
			for _, valueDescription := range def.ValueDescriptions {
				signal.ValueDescriptions = append(signal.ValueDescriptions, &descriptor.ValueDescription{
					Description: valueDescription.Description,
					Value:       int64(valueDescription.Value),
				})
			}
		case *dbc.AttributeValueForObjectDef:
			switch def.ObjectType {
			case dbc.ObjectTypeMessage:
				msg, ok := c.db.Message(def.MessageID.ToCAN())
				if !ok {
					c.errors = append(c.errors, errors.New(fmt.Sprintf("no declared message: %v", def)))
					continue
				}
				switch def.AttributeName {
				case "GenMsgSendType":
					if err := msg.SendType.UnmarshalString(def.StringValue); err != nil {
						c.errors = append(c.errors, errors.New(fmt.Sprintf("failed to unmarshal message send type: %v", def)))
						continue
					}
				case "GenMsgCycleTime":
					msg.CycleTime = time.Duration(def.IntValue) * time.Millisecond
				case "GenMsgDelayTime":
					msg.DelayTime = time.Duration(def.IntValue) * time.Millisecond
				}
			case dbc.ObjectTypeSignal:
				sig, ok := c.db.Signal(def.MessageID.ToCAN(), string(def.SignalName))
				if !ok {
					c.errors = append(c.errors, errors.New(fmt.Sprintf("no declared signal: %v", def)))
					continue
				}
				if def.AttributeName == "GenSigStartValue" {
					sig.DefaultValue = int(def.IntValue)
				}
			}
		}
	}
}

func (c *Compiler) sortDescriptors() {
	// Sort nodes by name
	sort.Slice(c.db.Nodes, func(i, j int) bool {
		return c.db.Nodes[i].Name < c.db.Nodes[j].Name
	})
	// Sort messages by ID
	sort.Slice(c.db.Messages, func(i, j int) bool {
		return c.db.Messages[i].ID < c.db.Messages[j].ID
	})
	for _, m := range c.db.Messages {
		// Sort signals by start (and multiplexer value)
		sort.Slice(m.Signals, func(j, k int) bool {
			if m.Signals[j].MultiplexerValue < m.Signals[k].MultiplexerValue {
				return true
			}
			return m.Signals[j].Start < m.Signals[k].Start
		})
		// Sort value descriptions by value
		for _, s := range m.Signals {
			sort.Slice(s.ValueDescriptions, func(k, l int) bool {
				return s.ValueDescriptions[k].Value < s.ValueDescriptions[l].Value
			})
		}
	}
}
