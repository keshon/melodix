// Package innertube holds the YouTube InnerTube client identity — the "who is
// asking" half of every InnerTube request. It is shared by the ytnative parser
// (the /player endpoint, for stream URLs) and the youtube source (/browse,
// /next and /search, for playlist and search metadata) so that the client
// version has exactly one place to be bumped.
//
// It deliberately holds identity only: no endpoints, no request/response types,
// no visitor session handling. Each caller owns its own endpoint and payload,
// and ytnative additionally owns the visitorData session that only /player
// needs (see ytnative/visitor.go).
//
// The client choice is not cosmetic. googlevideo enforces different request
// rules per issuing client: an ANDROID_VR stream URL rejects any open-ended
// request with 403 — a plain GET, or Range: bytes=0- — and serves only bounded
// ranges up to about 1 MiB, which killed every playback on its first read.
// VISIONOS URLs serve all three shapes. Verified against the live CDN, not
// inferred.
package innertube

const (
	// ClientName is the InnerTube client this project impersonates: the YouTube
	// app on Apple Vision Pro. It returns direct (cipher-free) stream URLs
	// anonymously and needs no PO token.
	ClientName = "VISIONOS"

	// ClientVersion is THE maintenance knob for anything InnerTube. When
	// YouTube deprecates it, playback falls back to kkdai/yt-dlp and playlist
	// or search metadata starts failing; bumping this constant (see yt-dlp's
	// INNERTUBE_CLIENTS for a known-good value) is the whole fix.
	ClientVersion = "1.02"

	DeviceMake  = "Apple"
	DeviceModel = "RealityDevice17,1"
	OSName      = "visionOS"
	OSVersion   = "26.5.23O471"

	// UserAgent must accompany both the InnerTube request and any follow-up
	// fetch of a URL it issued — googlevideo checks it.
	UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 15_7_3) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"
)

// Client returns the "client" block of an InnerTube request context. The result
// is a fresh map on every call, so callers may add request-specific keys —
// ytnative adds visitorData — without affecting anyone else.
func Client() map[string]any {
	return map[string]any{
		"clientName":    ClientName,
		"clientVersion": ClientVersion,
		"deviceMake":    DeviceMake,
		"deviceModel":   DeviceModel,
		"osName":        OSName,
		"osVersion":     OSVersion,
		"userAgent":     UserAgent,
		"hl":            "en",
	}
}

// Context wraps Client in the {"context":{"client":…}} envelope every InnerTube
// endpoint expects, as the base of a request body.
func Context() map[string]any {
	return map[string]any{"context": map[string]any{"client": Client()}}
}
