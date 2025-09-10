package pcapng

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/cockroachdb/errors"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	ecan "go.einride.tech/can"

	"github.com/BIwashi/candecode/pkg/can"
)

// Reader reads CAN frames from PCAPNG file
type Reader struct {
	reader      *pcapgo.NgReader
	linkType    layers.LinkType
	packetCount uint64
}

// NewReader creates a new PCAPNG reader
func NewReader(r io.Reader) (*Reader, error) {
	ngReader, err := pcapgo.NewNgReader(r, pcapgo.DefaultNgReaderOptions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create pcapng reader")
	}

	// Get link type from the first interface
	linkType := ngReader.LinkType()

	return &Reader{
		reader:   ngReader,
		linkType: linkType,
	}, nil
}

// ReadNext reads the next CAN frame from the PCAPNG file
func (r *Reader) ReadNext() (*can.TimedFrame, error) {
	for {
		data, ci, err := r.reader.ReadPacketData()
		if err != nil {
			if err == io.EOF {
				return nil, io.EOF
			}
			return nil, errors.Wrap(err, "failed to read packet data")
		}

		r.packetCount++

		// Parse the packet based on link type
		packet := gopacket.NewPacket(data, r.linkType, gopacket.Default)

		// Extract CAN frame based on the link type
		canFrame, err := r.extractCANFrame(packet, ci)
		if err != nil {
			// Skip non-CAN packets
			continue
		}

		return canFrame, nil
	}
}

// extractCANFrame extracts CAN frame from the packet
func (r *Reader) extractCANFrame(packet gopacket.Packet, ci gopacket.CaptureInfo) (*can.TimedFrame, error) {
	var payload []byte
	switch r.linkType {
	case layers.LinkTypeLinuxSLL:
		// Check if this is a Linux SLL (Linux cooked capture) packet
		if sllLayer := packet.Layer(layers.LayerTypeLinuxSLL); sllLayer != nil {
			sll := sllLayer.(*layers.LinuxSLL)
			payload = sll.Payload
		} else {
			// Try to parse as raw data
			payload = packet.Data()
		}
	case 227: // LinkTypeCAN ref: https://www.tcpdump.org/linktypes.html
		// Check if this is raw CAN
		payload = packet.Data()
	default:
		return nil, fmt.Errorf("unsupported link type: %v", r.linkType)
	}

	canFrame, isError, err := r.extractRawCANFrame(payload, ci)
	if err != nil {
		return nil, errors.Wrap(err, "failed to extract RawCAN frame")
	}
	if isError {
		return nil, errors.New("error in RawCAN frame")
	}

	return canFrame, nil
}

const (
	idFlagExtended = 0x80000000
	idFlagRemote   = 0x40000000
	idFlagError    = 0x20000000
	idMaskExtended = 0x1fffffff
	idMaskStandard = 0x7ff
)

// extractRawCANFrame extracts CAN frame from raw CAN format
func (r *Reader) extractRawCANFrame(data []byte, ci gopacket.CaptureInfo) (*can.TimedFrame, bool, error) {
	// Raw CAN frame format (similar to SocketCAN but without SLL header)
	if len(data) < 8 {
		return nil, false, errors.New(fmt.Sprintf("data too short for CAN frame: %d", len(data)))
	}

	var (
		// Parse CAN ID and flags
		canIDRaw = binary.LittleEndian.Uint32(data[0:4])

		// Extract flags from CAN ID
		isExtended = (canIDRaw & idFlagExtended) != 0
		isRemote   = (canIDRaw & idFlagRemote) != 0
		isError    = (canIDRaw & idFlagError) != 0
	)

	// Extract actual CAN ID
	var canID uint32
	if isExtended {
		canID = canIDRaw & idMaskExtended
	} else {
		canID = canIDRaw & idMaskStandard
	}

	// Get data length
	dataLen := data[4]
	if dataLen > 8 {
		dataLen = 8
	}

	// Extract data
	var canData ecan.Data
	if len(data) >= 8+int(dataLen) {
		copy(canData[:], data[8:8+dataLen])
	}

	return &can.TimedFrame{
		Frame: ecan.Frame{
			ID:         canID,
			Length:     dataLen,
			Data:       canData,
			IsRemote:   isRemote,
			IsExtended: isExtended,
		},
		Timestamp: ci.Timestamp,
	}, isError, nil
}

// GetPacketCount returns the number of packets read
func (r *Reader) GetPacketCount() uint64 {
	return r.packetCount
}

// ReadFrame provides backward-compatible name expected by converter code.
func (r *Reader) ReadFrame() (*can.TimedFrame, error) {
	return r.ReadNext()
}
