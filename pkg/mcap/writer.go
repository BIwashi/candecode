package mcap

import (
	"fmt"
	"io"
	"sync"
	"time"

	candecodeproto "github.com/BIwashi/candecode/pkg/proto"
	"github.com/foxglove/mcap/go/mcap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
)

// Writer writes DecodedSignal proto messages into an MCAP file.
//
// Design decisions:
//   - Single protobuf schema (candecode.proto.v1.DecodedSignal) reused by all channels.
//   - Channel granularity = (CAN message, Signal) i.e. one signal per channel/topic.
//   - Topic naming: /can/<MessageName>/<SignalName>
//   - Channel metadata includes: can_id (hex), message (dbc BO_ name), signal, unit (if any), is_extended.
//
// A new channel is created lazily on first occurrence of a (can_id, signal_name) combination.
type Writer struct {
	mu         sync.Mutex
	writer     *mcap.Writer
	schemaID   uint16
	nextChanID uint16
	channels   map[string]uint16 // key: canID_hex + ":" + signalName
}

// NewWriter initializes an MCAP writer with the DecodedSignal schema registered.
// The provided io.Writer should be an opened file (will not be closed here).
func NewWriter(out io.Writer) (*Writer, error) {
	w, err := mcap.NewWriter(out, &mcap.WriterOptions{
		Chunked:     true,
		ChunkSize:   2 * 1024 * 1024, // 2MB chunks
		Compression: mcap.CompressionZSTD,
	})
	if err != nil {
		return nil, fmt.Errorf("create MCAP writer: %w", err)
	}

	if err := w.WriteHeader(&mcap.Header{
		Profile: "",
		Library: "candecode",
	}); err != nil {
		return nil, fmt.Errorf("write header: %w", err)
	}

	// Prepare schema descriptor bytes (FileDescriptorProto marshal).
	fdProto := protodesc.ToFileDescriptorProto(candecodeproto.File_pkg_proto_dbc_proto)
	data, err := proto.Marshal(fdProto)
	if err != nil {
		return nil, fmt.Errorf("marshal schema descriptor: %w", err)
	}

	schemaID := uint16(1)
	if err := w.WriteSchema(&mcap.Schema{
		ID:       schemaID,
		Name:     "candecode.proto.v1.DecodedSignal",
		Encoding: "protobuf",
		Data:     data,
	}); err != nil {
		return nil, fmt.Errorf("write schema: %w", err)
	}

	return &Writer{
		writer:     w,
		schemaID:   schemaID,
		nextChanID: 1, // channels start at 1 (schema already used 1 but MCAP allows independent IDs)
		channels:   make(map[string]uint16),
	}, nil
}

// channelKey builds internal key.
func channelKey(hexID, signalName string) string {
	return hexID + ":" + signalName
}

// ensureChannel ensures a channel exists for a given signal; returns channel ID.
func (w *Writer) ensureChannel(canID uint32, isExtended bool, messageName, signalName, unit string) (uint16, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	hexID := fmt.Sprintf("0x%X", canID)
	key := channelKey(hexID, signalName)
	if id, ok := w.channels[key]; ok {
		return id, nil
	}

	// allocate new channel id
	w.nextChanID++
	chID := w.nextChanID

	topic := fmt.Sprintf("/can/%s/%s", messageName, signalName)
	metadata := map[string]string{
		"can_id":      hexID,
		"message":     messageName,
		"signal":      signalName,
		"is_extended": fmt.Sprintf("%t", isExtended),
	}
	if unit != "" {
		metadata["unit"] = unit
	}

	if err := w.writer.WriteChannel(&mcap.Channel{
		ID:              chID,
		SchemaID:        w.schemaID,
		Topic:           topic,
		MessageEncoding: "protobuf",
		Metadata:        metadata,
	}); err != nil {
		return 0, fmt.Errorf("write channel (topic=%s): %w", topic, err)
	}

	w.channels[key] = chID
	return chID, nil
}

// WriteDecodedSignal writes a single DecodedSignal proto instance as an MCAP message.
// ds.Timestamp must be set. LogTime/PublishTime use that timestamp.
func (w *Writer) WriteDecodedSignal(ds *candecodeproto.DecodedSignal) error {
	if ds == nil {
		return fmt.Errorf("nil DecodedSignal")
	}
	ts := time.Now()
	if t := ds.GetTimestamp(); t != nil {
		ts = t.AsTime()
	}
	channelID, err := w.ensureChannel(ds.GetCanId(), ds.GetIsExtended(), ds.GetMessageName(), ds.GetName(), ds.GetSignal().GetUnit())
	if err != nil {
		return err
	}
	data, err := proto.Marshal(ds)
	if err != nil {
		return fmt.Errorf("marshal DecodedSignal: %w", err)
	}
	if err := w.writer.WriteMessage(&mcap.Message{
		ChannelID:   channelID,
		Sequence:    0,
		LogTime:     uint64(ts.UnixNano()),
		PublishTime: uint64(ts.UnixNano()),
		Data:        data,
	}); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	return nil
}

// Close finalizes the MCAP file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Close()
}
