package mcap

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/foxglove/mcap/go/mcap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"

	candecodeproto "github.com/BIwashi/candecode/pkg/proto"
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

type WriterOption interface {
	apply(*writerOptions)
}

type writerOptions struct {
	chunked     bool
	compression mcap.CompressionFormat
	chunkSize   int64
}

// NewWriter initializes an MCAP writer with the DecodedSignal schema registered.
// The provided io.Writer should be an opened file (will not be closed here).
func NewWriter(out io.Writer, opts ...WriterOption) (*Writer, error) {
	opt := &writerOptions{
		chunked:     true,
		chunkSize:   100 * 1024 * 1024, // 100MB chunks
		compression: mcap.CompressionZSTD,
	}
	for _, o := range opts {
		o.apply(opt)
	}

	w, err := mcap.NewWriter(out, &mcap.WriterOptions{
		Chunked:     opt.chunked,
		ChunkSize:   opt.chunkSize,
		Compression: opt.compression,
	})
	if err != nil {
		return nil, errors.Wrap(err, "create MCAP writer")
	}

	if err := w.WriteHeader(&mcap.Header{
		Profile: "",
		Library: "candecode",
	}); err != nil {
		return nil, errors.Wrap(err, "write header")
	}

	var (
		// Prepare schema descriptor bytes as FileDescriptorSet (include dependencies).
		fdMain      = protodesc.ToFileDescriptorProto(candecodeproto.File_pkg_proto_dbc_proto)
		fdTimestamp = protodesc.ToFileDescriptorProto(timestamppb.File_google_protobuf_timestamp_proto)
		fdSet       = &descriptorpb.FileDescriptorSet{
			File: []*descriptorpb.FileDescriptorProto{
				fdMain,
				fdTimestamp,
			},
		}
	)

	data, err := proto.Marshal(fdSet)
	if err != nil {
		return nil, errors.Wrap(err, "marshal FileDescriptorSet")
	}

	// Set mcap schema (protobuf encoded FileDescriptorSet)
	schemaID := uint16(1)
	if err := w.WriteSchema(&mcap.Schema{
		ID:       schemaID,
		Name:     "candecode.proto.v1.DecodedSignal",
		Encoding: "protobuf",
		Data:     data,
	}); err != nil {
		return nil, errors.Wrap(err, "write schema")
	}

	return &Writer{
		writer:     w,
		schemaID:   schemaID,
		nextChanID: 1, // first channel will get ID=1
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

	var (
		hexID = fmt.Sprintf("0x%X", canID)
		key   = channelKey(hexID, signalName)
	)
	if id, ok := w.channels[key]; ok {
		return id, nil
	}

	// allocate new channel id (post-increment style so first channel=1)
	var (
		chID     = w.nextChanID
		topic    = fmt.Sprintf("/can/%s/%s", messageName, signalName)
		metadata = map[string]string{
			"can_id":      hexID,
			"message":     messageName,
			"signal":      signalName,
			"is_extended": fmt.Sprintf("%t", isExtended),
		}
	)
	w.nextChanID++
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
		return 0, errors.Wrap(err, fmt.Sprintf("write channel (topic=%s)", topic))
	}

	w.channels[key] = chID
	return chID, nil
}

// WriteDecodedSignal writes a single DecodedSignal proto instance as an MCAP message.
// ds.Timestamp must be set. LogTime/PublishTime use that timestamp.
func (w *Writer) WriteDecodedSignal(ds *candecodeproto.DecodedSignal) error {
	if ds == nil {
		return errors.New("nil DecodedSignal")
	}

	var ts time.Time // fallback to zero time
	if t := ds.GetTimestamp(); t != nil {
		ts = t.AsTime()
	}

	channelID, err := w.ensureChannel(ds.GetCanId(), ds.GetIsExtended(), ds.GetMessageName(), ds.GetName(), ds.GetSignal().GetUnit())
	if err != nil {
		return errors.Wrap(err, "ensure channel")
	}

	data, err := proto.Marshal(ds)
	if err != nil {
		return errors.Wrap(err, "marshal DecodedSignal")
	}

	if err := w.writer.WriteMessage(&mcap.Message{
		ChannelID:   channelID,
		Sequence:    0,
		LogTime:     uint64(ts.UnixNano()),
		PublishTime: uint64(time.Now().UnixNano()),
		Data:        data,
	}); err != nil {
		return errors.Wrap(err, "write message")
	}
	return nil
}

// Close finalizes the MCAP file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Close()
}
