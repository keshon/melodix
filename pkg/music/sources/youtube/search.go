package youtube

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/keshon/melodix/pkg/music/innertube"
)

// ErrNoVideoMatch means the query returned no usable video.
var ErrNoVideoMatch = errors.New("youtube: no video found for the given query")

// videoOnlyParams is YouTube's "Type: Video" search filter, so channels,
// playlists and shelves never reach the caller.
const videoOnlyParams = "EgIQAQ%3D%3D"

// SearchResult is one search hit. Duration is zero when YouTube reports none,
// which is what a live stream looks like.
type SearchResult struct {
	VideoID  string
	Title    string
	Author   string
	Duration time.Duration
}

// URL returns the canonical watch URL for the hit.
func (r SearchResult) URL() string {
	return "https://www.youtube.com/watch?v=" + r.VideoID
}

// Searcher turns a text query into video results. BaseURL and Client are fields
// so tests can point it at an httptest server.
type Searcher struct {
	BaseURL string
	Client  *http.Client
}

// NewSearcher creates a Searcher with production defaults.
func NewSearcher() *Searcher {
	return &Searcher{
		BaseURL: "https://www.youtube.com",
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Search returns up to limit results for the query, in YouTube's own ranking.
//
// This goes through InnerTube rather than scraping the results page: the page
// only ever yielded video ids, and a chooser needs titles, authors and
// durations to be worth showing.
func (r *Searcher) Search(query string, limit int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("youtube: empty search query")
	}
	if limit <= 0 {
		limit = 1
	}

	body := innertube.Context()
	body["query"] = query
	body["params"] = videoOnlyParams

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, r.BaseURL+"/youtubei/v1/search?prettyPrint=false", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", innertube.UserAgent)

	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, fmt.Errorf("youtube: search: %s: %s", resp.Status, strings.Join(strings.Fields(string(snippet)), " "))
	}

	var parsed searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("youtube: decode search response: %w", err)
	}

	out := make([]SearchResult, 0, limit)
	for _, section := range parsed.Contents.SectionList.Contents {
		for _, item := range section.ItemSection.Contents {
			v := item.CompactVideoRenderer
			if v == nil || v.VideoID == "" {
				continue
			}
			out = append(out, SearchResult{
				VideoID:  v.VideoID,
				Title:    v.Title.String(),
				Author:   v.ShortByline.String(),
				Duration: parseClockDuration(v.LengthText.String()),
			})
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	if len(out) == 0 {
		return nil, ErrNoVideoMatch
	}
	return out, nil
}

// parseClockDuration reads YouTube's "4:02" / "1:00:02" length text. An
// unparsable or absent value yields 0, which callers treat as unknown.
func parseClockDuration(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	parts := strings.Split(s, ":")
	if len(parts) > 3 {
		return 0
	}
	var total int
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return time.Duration(total) * time.Second
}

type searchResponse struct {
	Contents struct {
		SectionList struct {
			Contents []struct {
				ItemSection struct {
					Contents []struct {
						CompactVideoRenderer *struct {
							VideoID     string   `json:"videoId"`
							Title       runsText `json:"title"`
							ShortByline runsText `json:"shortBylineText"`
							LengthText  runsText `json:"lengthText"`
						} `json:"compactVideoRenderer"`
					} `json:"contents"`
				} `json:"itemSectionRenderer"`
			} `json:"contents"`
		} `json:"sectionListRenderer"`
	} `json:"contents"`
}
