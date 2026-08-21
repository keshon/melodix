package ytdlp

import (
	"os/exec"
	"sync"
)

// yt-dlp's own defaults are wrong for this project, and the difference only
// shows on live streams — which is exactly why it stayed hidden. Left alone,
// yt-dlp falls back to YouTube's android_vr client, and googlevideo serves that
// client under restrictions: on a live broadcast its segments start answering
// 403 after twenty-odd seconds and playback stops. It is the same client this
// project already moved off in ytnative, for the same reason.
//
// The fix is two flags, and they are passed on every invocation rather than
// written to a config file. A file would have to live somewhere: the user's own
// yt-dlp config is shared with everything else they run, and a generated one
// passed with --config-location suppresses every other config yt-dlp would have
// loaded, including the container's. Arguments have neither problem, cannot go
// stale against the code that depends on them, and keep the bot from writing
// files at runtime.
const (
	// embeddedWebClient avoids the formats that need a PO token, which is what
	// the container's config has always said in its comment.
	embeddedWebClient = "youtube:player_client=web_embedded"
)

// jsRuntimeCandidates are the runtimes yt-dlp can drive, in preference order.
// Only deno is enabled by default; the rest need naming explicitly.
var jsRuntimeCandidates = []string{"deno", "node", "bun"}

// lookPath is a seam for tests.
var lookPath = exec.LookPath

var (
	runtimeOnce sync.Once
	youtubeOpts []string
)

// youtubeArgs returns the extra arguments for a yt-dlp invocation, or nil when
// the environment cannot support them.
//
// The embedded web client cannot solve YouTube's challenges without a
// JavaScript runtime — it fails outright rather than degrading — so asking for
// it where none is installed would trade a partial failure (live streams) for a
// total one (nothing plays). Where no runtime is found this returns nil and
// yt-dlp keeps its own defaults.
func youtubeArgs() []string {
	runtimeOnce.Do(func() {
		for _, rt := range jsRuntimeCandidates {
			if _, err := lookPath(rt); err != nil {
				continue
			}
			youtubeOpts = []string{"--js-runtimes", rt, "--extractor-args", embeddedWebClient}
			l := logger()
			l.Info().Str("js_runtime", rt).Str("player_client", "web_embedded").
				Msg("ytdlp_youtube_client_configured")
			return
		}
		l := logger()
		l.Warn().Strs("looked_for", jsRuntimeCandidates).
			Msg("ytdlp_no_js_runtime_live_streams_will_fail")
	})
	return youtubeOpts
}

// args builds a full yt-dlp argument list, prefixing the YouTube options.
// The prefix is copied rather than appended to: youtubeArgs returns the shared
// slice, and appending into it would let one invocation scribble on the next.
func args(rest ...string) []string {
	opts := youtubeArgs()
	out := make([]string, 0, len(opts)+len(rest))
	out = append(out, opts...)
	return append(out, rest...)
}
