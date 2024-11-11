package song

import (
	"bufio"
	"bytes"
	"fmt"
	"hash/crc32"
	"net"
	"net/http"
	urlstd "net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/keshon/melodix/iradio"
	"github.com/keshon/melodix/youtube"

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

func (s *Song) FetchSong(url string) (*Song, error) {
	youtubeutil := *youtube.New()

	var song *Song
	var err error

	switch {
	case isYoutubeURL(url):
		song, err = s.fetchYoutubeSong(url)
	case isInternetRadioURL(url):
		song, err = s.fetchInternetRadioSong(url)
	default:
		var videoURL string
		videoURL, err = youtubeutil.GetVideoURLByTitle(url)
		if err == nil {
			song, err = s.fetchYoutubeSong(videoURL)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("error fetching song for %s: %w", url, err)
	}

	return song, nil
}
func isYoutubeURL(url string) bool {
	youtubeRegex := regexp.MustCompile(`^(https?://)?(www\.)?(m\.)?(music\.)?(youtube\.com|youtu\.be)(/|$)`)
	return youtubeRegex.MatchString(url)
}

func isInternetRadioURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") && !isYoutubeURL(url)
}

func (s *Song) fetchYoutubeSong(url string) (*Song, error) {
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

func (s *Song) fetchInternetRadioSong(url string) (*Song, error) {
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

func (s *Song) GetInfo(song *Song) (string, string, string, error) {
	switch song.Source {
	case SourceYouTube:
		return song.Title, song.Source.String(), song.PublicLink, nil
	case SourceInternetRadio:
		return s.getInternetRadioSongMetadata(song.StreamURL)
	default:
		return "", "", "", fmt.Errorf("unknown source: %v", song.Source)
	}
}

func (s *Song) getInternetRadioSongMetadata(url string) (string, string, string, error) {
	cmd := exec.Command("ffmpeg", "-i", url, "-f", "ffmetadata", "-")

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err != nil {
		return "", "", "", fmt.Errorf("error running ffmpeg command: %v", err)
	}

	var streamTitle string
	var icyName string
	var icyURL string
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Println(line)
		if strings.HasPrefix(line, "StreamTitle=") {
			streamTitle = strings.TrimPrefix(line, "StreamTitle=")
		}
		if strings.HasPrefix(line, "icy-name=") {
			icyName = strings.TrimPrefix(line, "icy-name=")
		}
		if strings.HasPrefix(line, "icy-url=") {
			icyURL = strings.TrimPrefix(line, "icy-url=")
		}
	}

	if err := scanner.Err(); err != nil {
		return "", "", "", fmt.Errorf("error reading metadata: %v", err)
	}

	return streamTitle, icyName, icyURL, nil
}
