# music

Queue-based music playback library for Go with pluggable audio sinks and track resolvers. Resolves URLs and search queries (YouTube, SoundCloud, radio), opens PCM streams via multiple parsers (yt-dlp, kkdai, ffmpeg), and plays through a sink of your choice (e.g. speaker or custom Discord voice).

## Install

```bash
go get github.com/keshon/melodix/pkg/music/...
```

## Quick start

Create a sink provider (e.g. speaker for local playback), a resolver, and a player; then enqueue and play:

```go
provider := sink.NewSpeakerProvider()
defer provider.Close()

res := resolver.New()
p := player.New(provider, res)

// Enqueue a URL or search query, then start playback
_ = p.Enqueue("https://www.youtube.com/watch?v=...", "", "")
_ = p.PlayNext("")  // "" for local; use voice channel ID for Discord
```

Listen to `p.PlayerStatus` for status updates (Playing, Added, Stopped, Error). See [examples/cli_speaker](examples/cli_speaker) for a full runnable CLI.

## Requirements

- **ffmpeg** — Must be installed and on your `PATH`. Used by most parsers to decode audio to PCM.
- **yt-dlp** — Optional. If installed, the ytdlp-link and ytdlp-pipe parsers are available; otherwise the library falls back to kkdai/ffmpeg parsers.
- **ebitengine/oto** — The speaker sink (`sink.NewSpeakerProvider()`) uses [oto](https://github.com/ebitengine/oto/v3) for audio output. Omit the speaker sink if you only need a custom sink (e.g. Discord).

## Documentation

- [player](player) — Queue-based playback engine
- [resolver](resolver) — Resolve URLs and search to track metadata
- [sink](sink) — Audio sink interfaces and speaker implementation
- [sources](sources) — Source interface and track types
- [parsers](parsers) — Streamer interface and track type
- [stream](stream) — Track stream opening and recovery

## License

music is licensed under the [MIT License](https://opensource.org/licenses/MIT).