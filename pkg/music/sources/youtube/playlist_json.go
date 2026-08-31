package youtube

import "strings"

// InnerTube response shapes for playlist expansion. They are typed rather than
// walked dynamically so a schema change surfaces as an empty list at one known
// place instead of a panic somewhere in the middle of a traversal.

// runsText covers InnerTube's two interchangeable text encodings.
type runsText struct {
	SimpleText string `json:"simpleText"`
	Runs       []struct {
		Text string `json:"text"`
	} `json:"runs"`
}

func (r runsText) String() string {
	if r.SimpleText != "" {
		return r.SimpleText
	}
	var b strings.Builder
	for _, run := range r.Runs {
		b.WriteString(run.Text)
	}
	return b.String()
}

// playlistVideoList is the container /browse returns, both on the first page
// and inside a continuation.
type playlistVideoList struct {
	Contents []struct {
		PlaylistVideoRenderer *struct {
			VideoID string   `json:"videoId"`
			Title   runsText `json:"title"`
		} `json:"playlistVideoRenderer"`
	} `json:"contents"`
	// Continuations is the legacy token shape, which is what the app clients
	// still get; the web client moved to continuationItemRenderer.
	Continuations []struct {
		NextContinuationData struct {
			Continuation string `json:"continuation"`
		} `json:"nextContinuationData"`
	} `json:"continuations"`
}

func (l *playlistVideoList) continuation() string {
	if l == nil || len(l.Continuations) == 0 {
		return ""
	}
	return l.Continuations[0].NextContinuationData.Continuation
}

type browseResponse struct {
	Contents struct {
		SingleColumn struct {
			Tabs []struct {
				TabRenderer struct {
					Content struct {
						SectionList struct {
							Contents []struct {
								PlaylistVideoList *playlistVideoList `json:"playlistVideoListRenderer"`
							} `json:"contents"`
						} `json:"sectionListRenderer"`
					} `json:"content"`
				} `json:"tabRenderer"`
			} `json:"tabs"`
		} `json:"singleColumnBrowseResultsRenderer"`
	} `json:"contents"`
	ContinuationContents struct {
		PlaylistVideoListContinuation *playlistVideoList `json:"playlistVideoListContinuation"`
	} `json:"continuationContents"`
	Header struct {
		PlaylistHeader struct {
			Title runsText `json:"title"`
		} `json:"playlistHeaderRenderer"`
	} `json:"header"`
	Alerts []struct {
		AlertRenderer struct {
			Type string   `json:"type"`
			Text runsText `json:"text"`
		} `json:"alertRenderer"`
	} `json:"alerts"`
}

// videoList returns the page's list, from either the first-page path or the
// continuation path, or nil when the response carries neither.
func (r *browseResponse) videoList() *playlistVideoList {
	if l := r.ContinuationContents.PlaylistVideoListContinuation; l != nil {
		return l
	}
	for _, tab := range r.Contents.SingleColumn.Tabs {
		for _, c := range tab.TabRenderer.Content.SectionList.Contents {
			if c.PlaylistVideoList != nil {
				return c.PlaylistVideoList
			}
		}
	}
	return nil
}

// errorAlert returns the text of an ERROR alert, which is how YouTube reports a
// private, deleted or unviewable list while still answering 200.
func (r *browseResponse) errorAlert() string {
	for _, a := range r.Alerts {
		if a.AlertRenderer.Type == "ERROR" {
			return a.AlertRenderer.Text.String()
		}
	}
	return ""
}

type nextResponse struct {
	Contents struct {
		TwoColumn struct {
			Playlist struct {
				Playlist struct {
					Title    string `json:"title"`
					Contents []struct {
						PlaylistPanelVideoRenderer *struct {
							VideoID string   `json:"videoId"`
							Title   runsText `json:"title"`
						} `json:"playlistPanelVideoRenderer"`
					} `json:"contents"`
				} `json:"playlist"`
			} `json:"playlist"`
		} `json:"twoColumnWatchNextResults"`
	} `json:"contents"`
}
