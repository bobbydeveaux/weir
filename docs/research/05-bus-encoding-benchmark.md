# Bus encoding benchmark: JSON or Protocol Buffers, Go or Rust

Run on 11 November 2026 on an Apple silicon Mac. The benchmark code is not in this repository.

Protocol Buffers is faster and smaller than JSON, and Rust is faster than Go, but neither difference matters at the volumes Weir expects. Go's slowest path still handles about 200,000 events a second on one core.

## What was measured

10,000 events shaped like a Weir step or run event (ids, a type, an attempt number, a timestamp, an actor, cost, tokens, and a four-entry string payload of about 300 bytes). Each program encodes all 10,000 and then decodes all 10,000, for 101 rounds, and reports the median. Each was run twice with consistent results.

- Go 1.26.0: `encoding/json` from the standard library, and `google.golang.org/protobuf` with generated code.
- Rust 1.94.1, release build with link-time optimisation: `serde_json` and `prost`.

## Results

Time for 10,000 events, in milliseconds:

| | JSON encode | JSON decode | Protobuf encode | Protobuf decode | JSON round trip | Protobuf round trip |
| --- | --- | --- | --- | --- | --- | --- |
| Go | 10.5 | 39.8 | 10.2 | 12.0 | 50.3 | 22.3 |
| Rust | 4.9 | 9.2 | 1.6 | 7.0 | 14.1 | 8.6 |

Size per event: 498 bytes as JSON, 381 bytes as Protocol Buffers (23% smaller). The payload is mostly strings, which neither format compresses.

- Protocol Buffers against JSON in Go: 2.3 times faster on the round trip, almost all of it on decode.
- Rust against Go: 2.6 times faster on Protocol Buffers, 3.6 times on JSON.

## What it means

- **Speed is not a reason to choose either.** A round trip costs 2 to 5 microseconds an event in Go. A database write or a model call costs far more, though those were not measured here.
- **Use Protocol Buffers for the schema.** A typed, versioned event definition shared across languages is the real benefit. Send binary on the internal bus and JSON to third-party hooks.
- **Stay with Go for the engine.** Rust would earn its place only in a high-volume ingest path, such as a collector streaming raw logs at tens of thousands of events a second. That component would meet the engine only at the bus.

A worked example: 500 people receiving 200 emails a day each is 100,000 events a day, about one a second on average and perhaps 25 a second at peak. That is four orders of magnitude below what Go handled here. The 200-emails figure is an assumption, not a measurement.

## Limits of this benchmark

- It measures serialisation only. There is no network, broker or database in the loop.
- Go's JSON figure uses the standard library, which is known to be slow at decoding. Faster libraries exist and were not tested.
- One machine, one event shape, one payload size.

## Running it

```sh
cd poc/bus-bench
./bin/weirbench-go
./rust/target/release/weirbench
```

To rebuild: `cargo build --release` in `rust/`, and `go build -o ../bin/weirbench-go .` in `go/`. The Go event type in `go/eventpb` is generated from `proto/event.proto` with `buf generate`.
