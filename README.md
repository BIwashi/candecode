# candecode
Convert CAN traffic captured in PCAPNG format into MCAP with rich decoded signal metadata using a DBC file.

## Features
- PCAPNG CAN frame ingestion
- DBC-based message and signal decoding (via OpenDBC)
- Protobuf schema for decoded signals
- MCAP output (channel + schema recorded once, per-signal records appended)
- Progress logging with frame and signal counters
- Deterministic, dependency-tracked build via Makefile targets
- Reproducible proto generation with buf

## Data Flow
```
PCAPNG file
  → pcapng reader (raw CAN frames)
    → DBC compiler + decoder
      → decoded signals (metadata & physical values)
        → protobuf (DecodedSignal)
          → MCAP writer (single output .mcap)
```

## Requirements
- Go 1.24+
- Python + uv (for OpenDBC SCons build)
- curl (to fetch buf)
- make
- protoc / buf (buf binary downloaded automatically to ./bin via Makefile)

## Installation
```bash
git clone --recurse-submodules https://github.com/BIwashi/candecode.git
cd candecode
make setup          # initializes submodules, uv environment, go mod tidy
make build          # builds OpenDBC, generates protobuf, builds binary
```

If cloned without submodules:
```bash
make sync
```

## Build
```bash
make build          # all (opendbc + proto + binary)
make build/opendbc
make build/buf
make build/cmd
```

Artifacts:
- Binary: `./bin/candecode`
- Generated protobuf: `./pkg/proto/*.pb.go`
- MCAP outputs: `./mcap/*.mcap` (created at runtime)

## Usage
Convert a PCAPNG capture using a DBC file:
```bash
./bin/candecode convert \
  --dbc-file path/to/reference.dbc \
  --pcapng-file capture.pcapng
```

Output:
- Creates `mcap/<capture-basename>.mcap`

Required flags:
- `--dbc-file` path to DBC file
- `--pcapng-file` path to PCAPNG file containing CAN frames

## Example
```bash
./bin/candecode convert \
  --dbc-file third_party/opendbc/opendbc/dbc/toyota_adas.dbc \
  --pcapng-file tests/sample_can_capture.pcapng
# Produces mcap/sample_can_capture.mcap
```

## MCAP Content
Each decoded CAN signal is written as a `DecodedSignal` protobuf record including:
- Frame metadata (CAN ID, extended flag, raw frame bytes)
- Signal definition (bit start/length, endian, scale, offset, min, max, unit)
- Physical value (if derivable) and raw value (bool / int / uint / float / bytes)
- Value descriptions (enumerations) and receiver nodes
- Source DBC path

Schema definition: `pkg/proto/dbc.proto` (generated Go types in `pkg/proto/dbc.pb.go`).

## Development
Formatting, imports, lint (strict imports + buf):
```bash
make lint
```

Tests:
```bash
make test
make test/coverage   # generates coverage.html
```

## Cleaning
```bash
make clean          # opendbc build + binaries
make clean/bin
make clean/opendbc
```

## OpenDBC Submodule
`third_party/opendbc` is a git submodule. Update to latest upstream main:
```bash
make sync
```
The OpenDBC SCons build runs automatically during `make build` (stamp file at `./bin/.opendbc_stamp`).

## Project Structure (selected)
```
cmd/main.go                  # CLI entry point
app/convert/cmd.go           # convert subcommand implementation
pkg/pcapng/reader.go         # PCAPNG frame reader
pkg/dbc/                     # DBC compiler & decoder abstraction
pkg/mcap/writer.go           # MCAP writer for DecodedSignal
pkg/proto/dbc.proto          # Protobuf schema (buf generates *.pb.go)
third_party/opendbc/         # OpenDBC database (submodule)
mcap/                        # Output directory (runtime)
pcapng/                      # Placeholder directory
```

## Roadmap (Potential)
- Additional output channels (raw frame stream)
- Filtering by CAN ID or message name
- Parallel decode pipeline
- Additional export formats (Parquet / JSONL)

## License
MIT License (see `LICENSE`).

## Acknowledgements
- OpenDBC (vehicle DBC definitions)
- foxglove MCAP Go library
- buf (Protobuf build tooling)
