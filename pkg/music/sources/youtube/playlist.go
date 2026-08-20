package youtube

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/keshon/melodix/pkg/music/innertube"
)

// Playlist expansion goes through InnerTube rather than page scraping: the watch
// and playlist pages moved to a "view model" JSON shape that carries no stable
// video list, while the API still answers with the classic renderers.
//
// Two endpoints are needed because YouTube treats the two kinds of list
// differently. A real playlist (PL…, UU… channel uploads, LL…) is a stored,
// browsable object and comes from /browse. A mix (RD…, including RDMM personal
// mixes, RDAMVM song radio and RDCLAK curated lists) is generated per request,
// has no browsable page at all — /browse answers "This playlist type is
// unviewable" — and comes from /next instead.
const (
	// MaxPlaylistItems caps how many tracks one link may contribute. Playlists
	// run to thousands of entries and mixes are formally endless, so an
	// unbounded expansion is a way to fill a guild's queue by accident. It is
	// exported because a limit nobody can see is indistinguishable from a bug:
	// /queue names it in its footer.
	MaxPlaylistItems = 100

	// playlistPageSize is what /browse returns per continuation. Informational:
	// paging stops on the cap or a missing token, never on this number.
	playlistPageSize = 20
)

var (
	// ErrPlaylistEmpty means the list resolved but held nothing playable.
	ErrPlaylistEmpty = errors.New("youtube: playlist has no playable videos")
	// ErrPlaylistUnavailable means YouTube refused the list — private, deleted,
	// or a type it will not serve.
	ErrPlaylistUnavailable = errors.New("youtube: playlist is unavailable")
)

// PlaylistEntry is one video in a list. Duration is deliberately absent:
// sources.TrackInfo has no such field, and parsers fill it at open time.
type PlaylistEntry struct {
	VideoID string
	Title   string
}

// PlaylistResult is an expanded list: its own title (for the reply) and entries
// in playback order.
type PlaylistResult struct {
	Title   string
	Entries []PlaylistEntry
}

// PlaylistFetcher expands a YouTube list id into entries. BaseURL and Client are
// fields so tests can point it at an httptest server, matching Searcher.
type PlaylistFetcher struct {
	BaseURL string
	Client  *http.Client
}

// NewPlaylistFetcher creates a PlaylistFetcher with production defaults.
func NewPlaylistFetcher() *PlaylistFetcher {
	return &PlaylistFetcher{
		BaseURL: "https://www.youtube.com",
		Client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// IsMixID reports whether a list id names a generated mix rather than a stored
// playlist. Every mix flavour shares the RD prefix.
func IsMixID(listID string) bool {
	return strings.HasPrefix(listID, "RD")
}

// Fetch expands a list id into entries, choosing the endpoint by list kind.
// seedVideoID is optional and only used for mixes, where it pins the generated
// order to start at the video the user actually linked.
func (p *PlaylistFetcher) Fetch(listID, seedVideoID string) (PlaylistResult, error) {
	if strings.TrimSpace(listID) == "" {
		return PlaylistResult{}, errors.New("youtube: empty playlist id")
	}
	if IsMixID(listID) {
		return p.fetchMix(listID, seedVideoID)
	}
	return p.fetchPlaylist(listID)
}

// fetchPlaylist walks /browse, following legacy continuation tokens until the
// list ends or MaxPlaylistItems is reached.
func (p *PlaylistFetcher) fetchPlaylist(listID string) (PlaylistResult, error) {
	body := innertube.Context()
	body["browseId"] = "VL" + listID
	body["contentCheckOk"] = true
	body["racyCheckOk"] = true

	var out PlaylistResult
	var token string

	for {
		var resp browseResponse
		if err := p.post("/youtubei/v1/browse", body, innertube.UserAgent, &resp); err != nil {
			// 400/404 is how a bad, deleted or private list id comes back; the
			// raw API body is noise in a chat reply, so only the status carries
			// over. (The 200-with-ERROR-alert shape below is the other refusal.)
			var he *httpError
			if errors.As(err, &he) && he.Code >= 400 && he.Code < 500 {
				return PlaylistResult{}, fmt.Errorf("%w (%s)", ErrPlaylistUnavailable, he.Status)
			}
			return PlaylistResult{}, err
		}
		if reason := resp.errorAlert(); reason != "" {
			return PlaylistResult{}, fmt.Errorf("%w: %s", ErrPlaylistUnavailable, reason)
		}
		if out.Title == "" {
			out.Title = resp.Header.PlaylistHeader.Title.String()
		}

		list := resp.videoList()
		if list == nil {
			// First page with no list at all is a refusal we could not read from
			// an alert; a later page simply ends the walk.
			if out.Entries == nil {
				return PlaylistResult{}, ErrPlaylistUnavailable
			}
			break
		}
		for _, item := range list.Contents {
			if item.PlaylistVideoRenderer == nil || item.PlaylistVideoRenderer.VideoID == "" {
				continue
			}
			out.Entries = append(out.Entries, PlaylistEntry{
				VideoID: item.PlaylistVideoRenderer.VideoID,
				Title:   item.PlaylistVideoRenderer.Title.String(),
			})
			if len(out.Entries) >= MaxPlaylistItems {
				return out, nil
			}
		}

		token = list.continuation()
		if token == "" {
			break
		}
		// A continuation request carries the token instead of the browseId.
		body = innertube.Context()
		body["continuation"] = token
	}

	if len(out.Entries) == 0 {
		return PlaylistResult{}, ErrPlaylistEmpty
	}
	return out, nil
}

// fetchMix reads a generated mix from /next.
//
// This one call uses the WEB client rather than the shared one: VISIONOS answers
// /next without any playlist panel, so a mix comes back empty. WEB is not a
// second maintenance knob in practice — it is the site itself, and it still
// serves this endpoint against a clientVersion from 2022, whereas app clients
// get retired. The CDN's per-issuing-client rules do not apply here: nothing in
// this response is a stream URL.
func (p *PlaylistFetcher) fetchMix(listID, seedVideoID string) (PlaylistResult, error) {
	body := map[string]any{
		"context": map[string]any{"client": map[string]any{
			"clientName":    webClientName,
			"clientVersion": webClientVersion,
			"hl":            "en",
			"gl":            "US",
		}},
		"playlistId":     listID,
		"contentCheckOk": true,
		"racyCheckOk":    true,
	}
	if seedVideoID != "" {
		body["videoId"] = seedVideoID
	}

	var resp nextResponse
	if err := p.post("/youtubei/v1/next", body, webUserAgent, &resp); err != nil {
		return PlaylistResult{}, err
	}

	mix := resp.Contents.TwoColumn.Playlist.Playlist
	out := PlaylistResult{Title: mix.Title}
	for _, item := range mix.Contents {
		if item.PlaylistPanelVideoRenderer == nil || item.PlaylistPanelVideoRenderer.VideoID == "" {
			continue
		}
		out.Entries = append(out.Entries, PlaylistEntry{
			VideoID: item.PlaylistPanelVideoRenderer.VideoID,
			Title:   item.PlaylistPanelVideoRenderer.Title.String(),
		})
		if len(out.Entries) >= MaxPlaylistItems {
			break
		}
	}
	if len(out.Entries) == 0 {
		return PlaylistResult{}, ErrPlaylistEmpty
	}
	return out, nil
}

const (
	webClientName = "WEB"
	// webClientVersion is stale on purpose and known to work; see fetchMix.
	webClientVersion = "2.20220801.00.00"
	webUserAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
)

// httpError carries the status so callers can map it to a sentinel. A 4xx from
// /browse is not a transport problem — it is YouTube saying the list id is bad,
// gone, or private, which the caller reports as ErrPlaylistUnavailable.
type httpError struct {
	Endpoint string
	Status   string
	Code     int
	Body     string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("youtube: %s: %s: %s", e.Endpoint, e.Status, e.Body)
}

func (p *PlaylistFetcher) post(path string, body map[string]any, userAgent string, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, p.BaseURL+path+"?prettyPrint=false", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// The body names the actual reason, e.g. a retired client context.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return &httpError{
			Endpoint: strings.TrimPrefix(path, "/youtubei/v1/"),
			Status:   resp.Status,
			Code:     resp.StatusCode,
			Body:     strings.Join(strings.Fields(string(snippet)), " "),
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("youtube: decode %s response: %w", strings.TrimPrefix(path, "/youtubei/v1/"), err)
	}
	return nil
}

// seedFirst makes the video the link named the one that plays first, which is
// what "play this video, then continue the list" means.
//
// Found in the list, everything before it is dropped: linking track 40 of 50
// asks for 40 onwards, not the whole thing. Not found — the list was capped
// before reaching it, or the video simply is not a member — it is prepended, so
// the video the user actually pointed at never goes missing.
func seedFirst(entries []PlaylistEntry, seedVideoID string) []PlaylistEntry {
	if seedVideoID == "" || len(entries) == 0 {
		return entries
	}
	for i, e := range entries {
		if e.VideoID == seedVideoID {
			return entries[i:]
		}
	}
	return append([]PlaylistEntry{{VideoID: seedVideoID}}, entries...)
}
