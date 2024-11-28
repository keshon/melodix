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

	"github.com/keshon/melodix/stream"
	"github.com/keshon/melodix/youtube"
	"github.com/keshon/melodix/yt_dlp"
	kkdai_youtube "github.com/kkdai/youtube/v2"
)

var (
	KkdaiClient = &kkdai_youtube.Client{HTTPClient: &http.Client{
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
	}}
	youtubeClient = *youtube.New()
)

type Song struct {
	Title          string        // title of the song
	PublicLink     string        // link to the song page
	StreamURL      string        // URL for streaming the song
	StreamFilepath string        // path for streaming the song
	Thumbnail      Thumbnail     // thumbnail image for the song
	Duration       time.Duration // duration of the song
	SongID         string        // unique ID for the song
	Source         SongSource    // source type of the song
}

type Thumbnail struct {
	URL    string
	Width  uint
	Height uint
}

type SongSource int32

const (
	SourceYouTube SongSource = iota
	SourceRadioStream
	SourceLocalFile // reserved, not used
)

func (source SongSource) String() string {
	sources := map[SongSource]string{
		SourceYouTube:     "YouTube",
		SourceRadioStream: "RadioStream",
		SourceLocalFile:   "LocalFile", // reserved, not used
	}

	return sources[source]
}

func New() *Song {
	return &Song{}
}

func (s *Song) FetchSongs(URLsOrTitle string) ([]*Song, error) {

	var songs []*Song
	var ytDlpURLs []string
	var intertnetRadioURLs []string

	if strings.Contains(URLsOrTitle, "http://") || strings.Contains(URLsOrTitle, "https://") {
		urls := strings.Fields(URLsOrTitle)
		for _, url := range urls {
			isYtdlpSupported := s.CheckLink(url)
			if isYtdlpSupported {
				ytDlpURLs = append(ytDlpURLs, url)
			} else {
				intertnetRadioURLs = append(intertnetRadioURLs, url)
			}
		}

		if len(ytDlpURLs) > 0 {
			for _, url := range ytDlpURLs {
				// if !isYoutubeURL(URL) {
				// 	song, err := s.fetchYoutubeSong(URL)
				// 	if err != nil {
				// 		return nil, err
				// 	}
				// 	songs = append(songs, song)
				// } else {
				song, err := s.fetchYtdlpSong(url)
				if err != nil {
					return nil, err
				}
				songs = append(songs, song)
				// }
			}
		}

		if len(intertnetRadioURLs) > 0 {
			for _, URL := range intertnetRadioURLs {
				song, err := s.fetchInternetRadioSong(URL)
				if err != nil {
					return nil, err
				}
				songs = append(songs, song)
			}
		}

	} else {
		url, err := youtubeClient.FetchVideoURLByTitle(URLsOrTitle)
		if err != nil {
			return nil, err
		}
		song, err := s.FetchSongs(url)
		if err != nil {
			return nil, err
		}
		songs = append(songs, song...)
	}
	fmt.Println(len(songs))
	for _, song := range songs {
		fmt.Println(song)
	}
	return songs, nil
}
func isYoutubeURL(url string) bool {
	youtubeRegex := regexp.MustCompile(`^(https?://)?(www\.)?(m\.)?(music\.)?(youtube\.com|youtu\.be)(/|$)`)
	return youtubeRegex.MatchString(url)
}

func isInternetRadioURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") && !isYoutubeURL(url)
}

// func (s *Song) fetchYoutubeSong(url string) (*Song, error) {
// 	yd := yt_dlp.New()
// 	id, err := s.extractYoutubeID(url)
// 	if err != nil {
// 		return nil, err
// 	}

// 	song, err := KkdaiClient.GetVideo(id)
// 	if err != nil {
// 		if strings.Contains(err.Error(), "UNPLAYABLE") {
// 			return nil, fmt.Errorf("YouTube video is unplayable, possibly due to `region restrictions` or other issues.\n\n¯\\_(ツ)_/¯")
// 		}
// 		return nil, err
// 	}
// 	songMeta, err := yd.GetMetaInfo(url)
// 	if err != nil {
// 		return nil, err
// 	}
// 	fmt.Println(songMeta)
// 	songStreamURL, err := yd.GetStreamURL(url) // a fix due to kkdai is broken atm
// 	if err != nil {
// 		return nil, err
// 	}

// 	var thumbnail Thumbnail
// 	if len(song.Thumbnails) > 0 {
// 		thumbnail = Thumbnail(song.Thumbnails[0])
// 	}

// 	return &Song{
// 		Title:      song.Title,
// 		PublicLink: url,
// 		StreamURL:  songStreamURL, //song.Formats.WithAudioChannels()[0].URL,
// 		Duration:   song.Duration,
// 		Thumbnail:  thumbnail,
// 		SongID:     song.ID,
// 		Source:     SourceYouTube,
// 		YTVideo:    song,
// 	}, nil
// }

// func (s *Song) fetchYoutubePlaylist(url string) ([]*Song, error) {
// 	var songs []*Song
// 	playlist, err := KkdaiClient.GetPlaylist(url)

// 	// Check if it's a YouTube Mix Playlist that `kkdai_youtube` doesn't natively support
// 	if err != nil && err.Error() == "extractPlaylistID failed: no playlist detected or invalid playlist ID" {
// 		urls, mixErr := youtube.New().FetchMixPlaylistVideoURLs(url)
// 		if mixErr != nil {
// 			return nil, mixErr
// 		}
// 		// Create a synthetic playlist from the fetched URLs
// 		playlist = &kkdai_youtube.Playlist{
// 			Videos: make([]*kkdai_youtube.PlaylistEntry, len(urls)),
// 		}
// 		for i, id := range urls {
// 			playlist.Videos[i] = &kkdai_youtube.PlaylistEntry{ID: id}
// 		}
// 	} else if err != nil {
// 		return nil, err
// 	}

// 	var (
// 		wg    sync.WaitGroup
// 		mu    sync.Mutex
// 		index = make(map[string]int, len(playlist.Videos))
// 	)

// 	for i, video := range playlist.Videos {
// 		index[video.ID] = i
// 		wg.Add(1)
// 		go func(videoID string) {
// 			defer wg.Done()
// 			videoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)
// 			song, fetchErr := s.fetchYoutubeSong(videoURL)
// 			if fetchErr != nil {
// 				fmt.Printf("Error fetching song for video ID %s: %v\n", videoID, fetchErr)
// 				return
// 			}
// 			mu.Lock()
// 			songs = append(songs, song)
// 			mu.Unlock()
// 		}(video.ID)
// 	}

// 	wg.Wait()

// 	// Stable sort to maintain the order based on the original playlist
// 	sort.SliceStable(songs, func(i, j int) bool {
// 		return index[songs[i].SongID] < index[songs[j].SongID]
// 	})

// 	return songs, nil
// }

func (s *Song) extractYoutubeID(url string) (string, error) {
	parsedURL, err := urlstd.Parse(url)
	if err != nil {
		fmt.Println("Error parsing URL:", err)
		return "", err
	}
	queryParams := parsedURL.Query()

	videoID := queryParams.Get("v")
	if videoID != "" {
		return videoID, nil
	}

	fmt.Println("Video ID not found.")
	return "", nil
}

func (s *Song) fetchYtdlpSong(url string) (*Song, error) {
	meta, err := yt_dlp.New().GetMetaInfo(url)
	if err != nil {
		return nil, err
	}

	streamURL, err := yt_dlp.New().GetStreamURL(url)
	if err != nil {
		return nil, err
	}

	timeDuration := time.Duration(meta.Duration) * time.Second

	return &Song{
		Title:      meta.Title,
		PublicLink: meta.WebPageURL,
		StreamURL:  streamURL,
		Thumbnail:  Thumbnail{},
		Duration:   timeDuration,
		SongID:     meta.ID,
		Source:     SourceYouTube,
	}, nil
}

func (s *Song) fetchInternetRadioSong(url string) (*Song, error) {
	u, err := urlstd.Parse(url)
	if err != nil {
		return nil, fmt.Errorf("error parsing url: %v", err)
	}

	hash := crc32.ChecksumIEEE([]byte(u.Host))

	stream := stream.New()

	contentType, err := stream.GetContentType(u.String())
	if err != nil {
		return nil, fmt.Errorf("error getting content-type: %v", err)
	}

	if stream.IsValidStreamType(contentType) {
		return &Song{
			Title:      u.Host,
			PublicLink: url,
			StreamURL:  u.String(),
			Thumbnail:  Thumbnail{},
			Duration:   -1,
			SongID:     fmt.Sprintf("%d", hash),
			Source:     SourceRadioStream,
		}, nil
	} else {
		return nil, fmt.Errorf("not a valid stream due to invalid content-type: %v", contentType)
	}
}

func (s *Song) GetInfo(song *Song) (string, string, string, error) {
	switch song.Source {
	case SourceYouTube:
		return song.Title, song.Source.String(), song.PublicLink, nil
	case SourceRadioStream:
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

func (s *Song) CheckLink(link string) bool {
	for _, service := range ytDlpSupported {
		if strings.Contains(link, service) {
			return true
		}
	}
	return false
}

var ytDlpSupported = []string{
	"soundcloud.com",  // supported
	"youtube.com",     // supported
	"56.com",          // supported
	"bandcamp.com",    // supported
	"dailymotion.com", // region locked
	"vimeo.com",
	"tiktok.com",
	"facebook.com",
	"instagram.com",
	"vevo.com",
	"hypem.com",
	"clyp.it",
	"audiomack.com", // broken
	"huya.com",
	"douyu.com",
	"chaturbate.com",
	"pornhub.com",
	"xvideos.com",
	"spankbang.com",
	"cam4.com",
	"camsoda.com",
	"datpiff.com",
	"reverbnation.com",
	"vocaroo.com",
	"glomex.com",
	"peertube.org",
	"younow.com",
	"vid.me",
	"smule.com",
}
