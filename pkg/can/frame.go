package can

import (
	"time"

	ecan "go.einride.tech/can"
)

// TimedFrame wraps einride can.Frame to add capture timestamp information.
// Embedding keeps field access (ID, Length, Data, IsExtended, IsRemote, ...) identical.
type TimedFrame struct {
	ecan.Frame
	// Timestamp is the original capture time from the pcap (host monotonic not required;
	// wall-clock provided by gopacket CaptureInfo).
	Timestamp time.Time
}

type Data ecan.Data
