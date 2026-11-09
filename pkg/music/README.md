# music

Use Melodix's Discord-independent Go engine to resolve tracks, manage a queue
and deliver 20 ms Opus packets to a speaker or a custom sink. YouTube's compatible
Opus streams pass through without re-encoding; other paths use FFmpeg.

## Requirements

Use Go 1.26+. The speaker sink uses `ebitengine/oto`; on Linux its build needs a
C compiler and ALSA development files. Install FFmpeg on `PATH` for SoundCloud,
radio and transcoding fallbacks. YouTube live broadcasts require yt-dlp and Node,
Deno or Bun. Native YouTube passthrough needs neither media program.

## Install

```sh
go get github.com/keshon/melodix/pkg/music/...
```

## Quick start

This complete program plays a supplied URL through the default speaker. Run it
with a real track URL. It stays alive until you press Enter, and stops playback
before closing the sink provider.

```go
package main

import (
    "bufio"
    "fmt"
    "log"
    "os"
    "sync"

    "github.com/keshon/melodix/pkg/music/player"
    "github.com/keshon/melodix/pkg/music/resolve"
    "github.com/keshon/melodix/pkg/music/sink"
)

func main() {
    if len(os.Args) != 2 {
        log.Fatal("usage: player <track-url>")
    }
    provider := sink.NewSpeakerProvider()
    defer provider.Close()
    p := player.New(provider, resolve.New())
    defer p.Stop(true)

    done := make(chan struct{})
    var watcher sync.WaitGroup
    watcher.Add(1)
    go func() {
        defer watcher.Done()
        for {
            select {
            case <-done:
                return
            case status := <-p.PlayerStatus:
                fmt.Println(status)
            }
        }
    }()
    defer func() { close(done); watcher.Wait() }()

    if err := p.Enqueue(os.Args[1], "", ""); err != nil {
        log.Print(err)
        return
    }
    if err := p.PlayNext(""); err != nil {
        log.Print(err)
        return
    }
    fmt.Println("Press Enter to stop.")
    _, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}
```

For an interactive example, see [examples/clispeaker](examples/clispeaker).
That example has a smaller command set than the application in `cmd/cli`.

## Lifecycle and status

Create one player per target. Use exactly one long-lived receiver for
`PlayerStatus`; competing receivers take events from each other. Stop the player
before releasing its sink provider. The example owns and joins its watcher;
the application frontends provide their own long-lived status consumers.

`PlayNext` can start opening a stream before its first packet arrives. When
integrating a UI, use the player options/hooks and the
[status-delivery contract](../../docs/architecture.md#status-delivery-single-consumer-contract)
to distinguish synchronous command results from later playback transitions.
The Discord-specific message lifecycle is owned by its voice service, not this library.

## Playback and recovery

<a id="how-it-works-high-level"></a>
<a id="algorithms-by-stage"></a>
<a id="1-resolve-input--trackinfo"></a>
<a id="2-enqueue-trackinfo--queue"></a>
<a id="3-start-playback-dequeue--open-resilient-stream"></a>
<a id="4-open-stream-choose-parser"></a>
<a id="5-media-recovery-parserffmpeg-level"></a>
<a id="6-sink-streaming--voice-transport-recovery"></a>
<a id="discord--ui-errors-and-playerstatus"></a>
<a id="track-cache--anti-skip-buffer-optional"></a>

The resolver produces metadata and an ordered parser list. The player queues
tracks, opens a recovery stream and hands packets to the sink. A failed first
read advances the parser chain; an early interruption can reopen the source.
Voice transport recovery has a separate retry budget. Full flows and diagrams
are maintained in [Architecture](../../docs/architecture.md#playback-pipeline).

The sink owns the read loop. After `RecoveryStream.Start` opens once,
`RequestReopen` asks the reading goroutine to reopen; another goroutine must not
mutate its run state. See [ownership rules](../../docs/ownership.md), especially
rules 2 and 3, before implementing a parser or custom sink.

## Optional playback layers

Direct library consumption starts with cache and read-ahead disabled.
Configure `stream.SetCache` and `stream.SetBufferAhead` once, before playback.
The application configures these separately: its read-ahead default is 30 seconds.

The cache stores complete tracks under content keys and checks disk before
opening parsers; radio is not cached. The read-ahead buffer wraps recovery so its
lead can cover a reconnect. Consume it through `RecoveryStream.Packets()`.
The [architecture section](../../docs/architecture.md#track-cache--anti-skip-buffer)
owns the layering explanation; API comments describe setup and storage format.

## Key extension points

- Implement `player.Resolver` for different input resolution.
- Implement `sources.Searcher` for ranked results on a searchable source.
- Implement `sink.AudioSink` and `sink.Provider` for another output. Their API
  comments specify read-loop, transport-failure and frame-protection obligations.
- Implement `parsers.Streamer.Open`, returning `parsers.Opened`, and register the
  parser under its frozen key. See the [extension checklist](../../docs/conventions.md#adding-things).

Parsers can report through package-level loggers; wire their `SetLogger` methods
once at startup if diagnostics are needed. FFmpeg and yt-dlp default to `PATH`;
their package path variables support explicit executable locations.

## Documentation

- [player](player): queue and playback engine
- [resolve](resolve): input resolution
- [sink](sink): output contracts and speaker implementation
- [sources](sources): metadata sources and search
- [parsers](parsers): stream backends
- [stream](stream): opening and recovery
- [cache](cache): content-keyed disk cache
- [Architecture](../../docs/architecture.md): system flows
- [Ownership](../../docs/ownership.md): lifecycle contracts and enforcement limits

## License

music is licensed under the [MIT License](LICENSE).
