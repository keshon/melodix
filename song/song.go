package song

import (
	"fmt"
	"hash/crc32"
	"net"
	"net/http"
	urlstd "net/url"
	"time"

	"github.com/keshon/melodix/iradio"

	kkdai_youtube "github.com/kkdai/youtube/v2"
)

type Song struct {
	Title          string        // Title of the song
	PublicLink     string        // Link to the song page
	StreamURL      string        // URL for streaming the song
	StreamFilepath string        // Path for streaming the song
	Thumbnail      Thumbnail     // Thumbnail image for the song
	Duration       time.Duration // Duration of the song
	SongID         string        // Unique ID for the song
	Source         SongSource    // Source type of the song
}

type Thumbnail struct {
	URL    string
	Width  uint
	Height uint
}

type SongSource int32

const (
	SourceYouTube SongSource = iota
	SourceInternetRadio
	SourceLocalFile
)

func (source SongSource) String() string {
	sources := map[SongSource]string{
		SourceYouTube:       "YouTube",
		SourceInternetRadio: "InternetRadio",
		SourceLocalFile:     "LocalFile",
	}

	return sources[source]
}

func New() *Song {
	return &Song{}
}

func (s *Song) GetYoutubeSong(url string) (*Song, error) {
	client := &kkdai_youtube.Client{}
	client.HTTPClient = &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		Timeout: 30 * time.Second,
	}

	id, err := s.extractYoutubeID(url)
	if err != nil {
		return nil, err
	}

	song, err := client.GetVideo(id)
	if err != nil {
		return nil, err
	}

	var thumbnail Thumbnail
	if len(song.Thumbnails) > 0 {
		thumbnail = Thumbnail(song.Thumbnails[0])
	}

	return &Song{
		Title:      song.Title,
		PublicLink: url,
		StreamURL:  song.Formats.WithAudioChannels()[0].URL,
		Duration:   song.Duration,
		Thumbnail:  thumbnail,
		SongID:     song.ID,
		Source:     SourceYouTube,
	}, nil
}

func (s *Song) extractYoutubeID(url string) (string, error) {
	parsedURL, err := urlstd.Parse(url)
	if err != nil {
		fmt.Println("Error parsing URL:", err)
		return "", err
	}
	queryParams := parsedURL.Query()

	videoID := queryParams.Get("v")
	if videoID != "" {
		fmt.Println("The video ID is:", videoID)
		return videoID, nil
	}

	fmt.Println("Video ID not found.")
	return "", nil
}

func (s *Song) GetInternetRadioSong(url string) (*Song, error) {
	u, err := urlstd.Parse(url)
	if err != nil {
		return nil, fmt.Errorf("error parsing url: %v", err)
	}

	hash := crc32.ChecksumIEEE([]byte(u.Host))

	ir := iradio.New()

	contentType, err := ir.GetContentTypeFromURL(u.String())
	if err != nil {
		return nil, fmt.Errorf("error getting content-type: %v", err)
	}

	if ir.IsValidAudioStream(contentType) {
		return &Song{
			Title:      u.Host,
			PublicLink: url,
			StreamURL:  u.String(),
			Thumbnail:  Thumbnail{},
			Duration:   -1,
			SongID:     fmt.Sprintf("%d", hash),
			Source:     SourceInternetRadio,
		}, nil
	} else {
		return nil, fmt.Errorf("not a valid stream due to invalid content-type: %v", contentType)
	}
}

// NOT IMPLEMENTED
func (s *Song) GetLocalFileSong(url string) (*Song, error) {
	return nil, nil
}
