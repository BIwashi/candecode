package mcap

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/BIwashi/candecode/pkg/can"
	"github.com/BIwashi/candecode/pkg/dbc"
	"github.com/foxglove/mcap/go/mcap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Writer writes decoded CAN messages to MCAP format
type Writer struct {
	writer      *mcap.Writer
	dbcFile     *dbc.DBCFile
	channelIDs  map[uint32]uint16 // CAN ID to channel ID mapping
	schemaIDs   map[uint32]uint16 // CAN ID to schema ID mapping
	protoSchema []byte            // Compiled proto schema
	nextID      uint16
}

// NewWriter creates a new MCAP writer
func NewWriter(w io.Writer, dbcFile *dbc.DBCFile, protoSchema []byte) (*Writer, error) {
	writer, err := mcap.NewWriter(w, &mcap.WriterOptions{
		Chunked:     true,
		ChunkSize:   1024 * 1024, // 1MB chunks
		Compression: mcap.CompressionZSTD,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MCAP writer: %w", err)
	}

	// Write header (no ROS2 profile – using generic candecode proto schema)
	err = writer.WriteHeader(&mcap.Header{
		Profile: "",
		Library: "candecode",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to write header: %w", err)
	}

	mw := &Writer{
		writer:      writer,
		dbcFile:     dbcFile,
		channelIDs:  make(map[uint32]uint16),
		schemaIDs:   make(map[uint32]uint16),
		protoSchema: protoSchema,
		nextID:      1,
	}

	// Register schemas and channels for each message
	if err := mw.registerSchemasAndChannels(); err != nil {
		return nil, err
	}

	return mw, nil
}

// registerSchemasAndChannels registers schemas and channels for all DBC messages
func (w *Writer) registerSchemasAndChannels() error {
	for _, msg := range w.dbcFile.Messages {
		// Create schema for this message
		schemaID := w.nextID
		w.nextID++

		schemaName := fmt.Sprintf("candecode.%s", dbc.ToProtoMessageName(msg.Name))

		// Build schema data (proto descriptor)
		schemaData, err := w.buildProtoSchemaForMessage(msg)
		if err != nil {
			return fmt.Errorf("failed to build schema for message %s: %w", msg.Name, err)
		}

		// Write schema
		err = w.writer.WriteSchema(&mcap.Schema{
			ID:       schemaID,
			Name:     schemaName,
			Encoding: "protobuf",
			Data:     schemaData,
		})
		if err != nil {
			return fmt.Errorf("failed to write schema: %w", err)
		}
		w.schemaIDs[msg.ID] = schemaID

		// Create channel for this message
		channelID := w.nextID
		w.nextID++

		topic := fmt.Sprintf("/can/%s", msg.Name)
		err = w.writer.WriteChannel(&mcap.Channel{
			ID:              channelID,
			SchemaID:        schemaID,
			Topic:           topic,
			MessageEncoding: "protobuf",
			Metadata: map[string]string{
				"can_id":      fmt.Sprintf("0x%X", msg.ID),
				"dbc_message": msg.Name,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to write channel: %w", err)
		}
		w.channelIDs[msg.ID] = channelID
	}

	return nil
}

// buildProtoSchemaForMessage builds proto schema data for a specific message
func (w *Writer) buildProtoSchemaForMessage(msg *dbc.Message) ([]byte, error) {
	// Create a FileDescriptorProto for this message
	fd := &descriptorpb.FileDescriptorProto{
		Name:    proto.String(fmt.Sprintf("%s.proto", dbc.ToProtoMessageName(msg.Name))),
		Package: proto.String("candecode"),
		Syntax:  proto.String("proto3"),
	}

	// Create message descriptor
	msgDesc := &descriptorpb.DescriptorProto{
		Name: proto.String(dbc.ToProtoMessageName(msg.Name)),
	}

	// Add standard fields
	fieldNumber := int32(1)
	msgDesc.Field = append(msgDesc.Field, &descriptorpb.FieldDescriptorProto{
		Name:   proto.String("can_id"),
		Number: proto.Int32(fieldNumber),
		Type:   descriptorpb.FieldDescriptorProto_TYPE_UINT32.Enum(),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
	})
	fieldNumber++

	msgDesc.Field = append(msgDesc.Field, &descriptorpb.FieldDescriptorProto{
		Name:   proto.String("raw_data"),
		Number: proto.Int32(fieldNumber),
		Type:   descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
	})
	fieldNumber++

	msgDesc.Field = append(msgDesc.Field, &descriptorpb.FieldDescriptorProto{
		Name:   proto.String("timestamp_ns"),
		Number: proto.Int32(fieldNumber),
		Type:   descriptorpb.FieldDescriptorProto_TYPE_UINT64.Enum(),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
	})
	fieldNumber++

	// Add fields for each signal (both physical and raw values)
	for _, signal := range msg.Signals {
		// Physical value field
		msgDesc.Field = append(msgDesc.Field, &descriptorpb.FieldDescriptorProto{
			Name:   proto.String(dbc.ToProtoFieldName(signal.Name)),
			Number: proto.Int32(fieldNumber),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum(),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		})
		fieldNumber++

		// Raw value field
		msgDesc.Field = append(msgDesc.Field, &descriptorpb.FieldDescriptorProto{
			Name:   proto.String(dbc.ToProtoFieldName(signal.Name) + "_raw"),
			Number: proto.Int32(fieldNumber),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		})
		fieldNumber++
	}

	fd.MessageType = append(fd.MessageType, msgDesc)

	// Serialize the FileDescriptorProto
	return proto.Marshal(fd)
}

// WriteMessage writes a decoded CAN message to MCAP
func (w *Writer) WriteMessage(msg *can.DecodedMessage, timestamp time.Time) error {
	channelID, ok := w.channelIDs[msg.MessageID]
	if !ok {
		// Unknown message ID, skip
		return nil
	}

	// Build proto message data
	data, err := w.buildProtoMessageData(msg, timestamp)
	if err != nil {
		return fmt.Errorf("failed to build proto message data: %w", err)
	}

	// Write message
	err = w.writer.WriteMessage(&mcap.Message{
		ChannelID:   channelID,
		Sequence:    0, // Not used for CAN messages
		LogTime:     uint64(timestamp.UnixNano()),
		PublishTime: uint64(timestamp.UnixNano()),
		Data:        data,
	})
	if err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	return nil
}

// buildProtoMessageData builds protobuf encoded message data
func (w *Writer) buildProtoMessageData(msg *can.DecodedMessage, timestamp time.Time) ([]byte, error) {
	// Create a dynamic protobuf message
	var buf bytes.Buffer
	enc := &protoEncoder{w: &buf}

	// Write can_id (field 1)
	enc.writeUint32(1, msg.MessageID)

	// Write raw_data (field 2)
	enc.writeBytes(2, msg.RawData)

	// Write timestamp_ns (field 3)
	enc.writeUint64(3, uint64(timestamp.UnixNano()))

	// Write signal values in deterministic DBC order to match schema numbering.
	fieldNum := uint32(4)
	dbcMsg, ok := w.dbcFile.Messages[msg.MessageID]
	if ok {
		for _, sig := range dbcMsg.Signals {
			if val, found := msg.Signals[sig.Name]; found {
				enc.writeDouble(fieldNum, val.PhysicalValue)
			}
			fieldNum++
			if val, found := msg.Signals[sig.Name]; found {
				enc.writeInt64(fieldNum, int64(val.RawValue))
			}
			fieldNum++
		}
	}

	return buf.Bytes(), nil
}

// Close closes the MCAP writer
func (w *Writer) Close() error {
	return w.writer.Close()
}

// protoEncoder is a simple protobuf encoder
type protoEncoder struct {
	w io.Writer
}

func (e *protoEncoder) writeTag(fieldNum uint32, wireType uint8) error {
	tag := (fieldNum << 3) | uint32(wireType)
	return e.writeVarint(uint64(tag))
}

func (e *protoEncoder) writeVarint(v uint64) error {
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(buf, v)
	_, err := e.w.Write(buf[:n])
	return err
}

func (e *protoEncoder) writeUint32(fieldNum, value uint32) error {
	if err := e.writeTag(fieldNum, 0); err != nil { // Varint
		return err
	}
	return e.writeVarint(uint64(value))
}

func (e *protoEncoder) writeUint64(fieldNum uint32, value uint64) error {
	if err := e.writeTag(fieldNum, 0); err != nil { // Varint
		return err
	}
	return e.writeVarint(value)
}

func (e *protoEncoder) writeInt64(fieldNum uint32, value int64) error {
	if err := e.writeTag(fieldNum, 0); err != nil { // Varint
		return err
	}
	// Use zigzag encoding for signed integers
	encoded := uint64(value<<1) ^ uint64(value>>63)
	return e.writeVarint(encoded)
}

func (e *protoEncoder) writeDouble(fieldNum uint32, value float64) error {
	if err := e.writeTag(fieldNum, 1); err != nil { // Fixed64
		return err
	}
	buf := make([]byte, 8)
	bits := math.Float64bits(value)
	binary.LittleEndian.PutUint64(buf, bits)
	_, err := e.w.Write(buf)
	return err
}

func (e *protoEncoder) writeBytes(fieldNum uint32, value []byte) error {
	if err := e.writeTag(fieldNum, 2); err != nil { // Length-delimited
		return err
	}
	if err := e.writeVarint(uint64(len(value))); err != nil {
		return err
	}
	_, err := e.w.Write(value)
	return err
}
