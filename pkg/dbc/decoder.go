package dbc

import (
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	"go.einride.tech/can/pkg/descriptor"

	"github.com/BIwashi/candecode/pkg/can"
)

type DecodedSignal struct {
	Raw         any
	Physical    *float64
	Description string
	Signal      *descriptor.Signal
	Timestamp   time.Time
}

type Decoder struct {
	compiler *Compiler
}

func NewDecoder(compiler *Compiler) *Decoder {
	return &Decoder{
		compiler: compiler,
	}
}

func (d *Decoder) Decode(f *can.TimedFrame) (map[string]DecodedSignal, error) {
	message, ok := d.compiler.db.Message(f.ID)
	if !ok {
		return nil, errors.New(fmt.Sprintf("unknown message id: 0x%X", f.ID))
	}
	if f.Length != message.Length || f.IsExtended != message.IsExtended || f.IsRemote {
		return nil, errors.New("frame shape mismatch")
	}

	var (
		signalsMap = make(map[string]DecodedSignal)
		mux        *descriptor.Signal
		muxVal     uint64
	)

	// decode non-multiplexed signals
	for _, s := range message.Signals {
		if s.IsMultiplexed {
			continue
		}
		if s.IsMultiplexer {
			mux = s
			muxVal = s.UnmarshalUnsigned(f.Data)
			signalsMap[s.Name] = decodeSignal(s, *f)
			continue
		}
		signalsMap[s.Name] = decodeSignal(s, *f)
	}

	// decode multiplexed signals
	if mux != nil {
		for _, s := range message.Signals {
			if !s.IsMultiplexed {
				continue
			}
			if muxVal == uint64(s.MultiplexerValue) {
				signalsMap[s.Name] = decodeSignal(s, *f)
			}
		}
	}

	return signalsMap, nil
}

func decodeSignal(s *descriptor.Signal, f can.TimedFrame) DecodedSignal {
	var (
		raw         any
		physical    *float64
		description string
	)
	switch {
	case s.Length == 1:
		raw = s.UnmarshalBool(f.Data)
	case s.IsFloat:
		raw = s.UnmarshalFloat(f.Data)
	case s.IsSigned:
		raw = s.UnmarshalSigned(f.Data)
	default:
		raw = s.UnmarshalUnsigned(f.Data)
	}

	if !s.IsFloat && (s.Scale != 0 || s.Offset != 0 || s.Min != 0 || s.Max != 0) {
		switch v := raw.(type) {
		case int64:
			pv := s.ToPhysical(float64(v))
			physical = &pv
		case uint64:
			pv := s.ToPhysical(float64(v))
			physical = &pv
		}
	}
	vd, ok := s.UnmarshalValueDescription(f.Data)
	if ok {
		description = vd
	}

	return DecodedSignal{
		Raw:         raw,
		Physical:    physical,
		Description: description,
		Signal:      s,
		Timestamp:   f.Timestamp,
	}
}
