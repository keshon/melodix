package song

import (
	"bufio"
	"bytes"
	"fmt"
	"hash/crc32"
	urlstd "net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/keshon/melodix/sources_util"
	"github.com/keshon/melodix/ytdlp"
)

type Platform string

const (
	YouTube      Platform = "YouTube"
	Soundcloud   Platform = "Soundcloud"
	Bandcamp     Platform = "Bandcamp"
	FiftySixCom  Platform = "56.com"
	DailyMotion  Platform = "DailyMotion"
	Vimeo        Platform = "Vimeo"
	TikTok       Platform = "TikTok"
	Facebook     Platform = "Facebook"
	Instagram    Platform = "Instagram"
	Vevo         Platform = "Vevo"
	AudioMack    Platform = "AudioMack"
	Chaturbate   Platform = "Chaturbate"
	Pornhub      Platform = "Pornhub"
	ReverbNation Platform = "ReverbNation"
)

var platformURLs = map[Platform][]string{
	YouTube:      {"youtube.com", "youtu.be"},
	Soundcloud:   {"soundcloud.com"},
	Bandcamp:     {"bandcamp.com"},
	FiftySixCom:  {"56.com"},
	DailyMotion:  {"dailymotion.com"},
	Vimeo:        {"vimeo.com"},
	TikTok:       {"tiktok.com"},
	Facebook:     {"facebook.com"},
	Instagram:    {"instagram.com"},
	Vevo:         {"vevo.com"},
	AudioMack:    {"audiomack.com"},
	Chaturbate:   {"chaturbate.com"},
	Pornhub:      {"pornhub.com"},
	ReverbNation: {"reverbnation.com"},
}

var (
	youtubeUtil  = *sources_util.NewYouTubeUtil()
	interentUtil = *sources_util.New()
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
	Platform       string        // platform of the song
}

type Thumbnail struct {
	URL    string
	Width  uint
	Height uint
}

type SongSource int32

const (
	SourcePlatform SongSource = iota
	SourceInternet
	SourceFile
)

func (source SongSource) String() string {
	sources := map[SongSource]string{
		SourcePlatform: "Platform",
		SourceInternet: "Radio",
		SourceFile:     "File",
	}

	return sources[source]
}

func New() *Song {
	return &Song{}
}

func (s *Song) FetchSongs(input string) ([]*Song, error) {
	if isURL(input) {
		return s.fetchSongsByURLs(input)
	}
	return s.fetchSongsByTitle(input)
}

func isURL(input string) bool {
	return strings.Contains(input, "http://") || strings.Contains(input, "https://")
}

func (s *Song) fetchSongsByURLs(urlsInput string) ([]*Song, error) {
	urls := strings.Fields(urlsInput)
	var platformURLs, internetURLs []string

	for _, url := range urls {
		if platform := s.FindPlatformByURL(url); platform != "" {
			platformURLs = append(platformURLs, url)
		} else {
			internetURLs = append(internetURLs, url)
		}
	}

	var songs []*Song
	results := make(chan *Song, len(urls)) // Buffered channel to collect songs
	errs := make(chan error, len(urls))    // Buffered channel to collect errors
	var wg sync.WaitGroup                  // WaitGroup for managing concurrency
	ytdlp := ytdlp.New()

	for _, url := range platformURLs {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			song, err := s.fetchPlatformSong(ytdlp, url)
			if err != nil {
				errs <- fmt.Errorf("error fetching song from yt-dlp URL %q: %w", url, err)
				return
			}
			results <- song
		}(url)
	}

	for _, url := range internetURLs {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			song, err := s.fetchInternetSong(url)
			if err != nil {
				errs <- fmt.Errorf("error fetching song from internet radio URL %q: %w", url, err)
				return
			}
			results <- song
		}(url)
	}

	go func() {
		wg.Wait()
		close(results)
		close(errs)
	}()

	// Collect results and errors
	for song := range results {
		songs = append(songs, song)
	}
	if len(errs) > 0 {
		return nil, <-errs // Return the first error encountered
	}

	return songs, nil
}

func (s *Song) fetchSongsByTitle(title string) ([]*Song, error) {
	url, err := youtubeUtil.FetchVideoURLByTitle(title)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch video URL for title %q: %w", title, err)
	}
	return s.FetchSongs(url)
}

func (s *Song) fetchPlatformSong(ytdlp *ytdlp.YtdlpWrapper, url string) (*Song, error) {
	meta, err := ytdlp.GetMetaInfo(url)
	if err != nil {
		return nil, fmt.Errorf("error getting metadata from yt-dlp: %w", err)
	}

	streamURL, err := ytdlp.GetStreamURL(url)
	if err != nil {
		return nil, fmt.Errorf("error getting stream URL from yt-dlp: %w", err)
	}

	fmt.Println(streamURL)
	fmt.Println(meta.Title)

	return &Song{
		Title:      meta.Title,
		PublicLink: meta.WebPageURL,
		StreamURL:  streamURL,
		Thumbnail:  Thumbnail{},
		Duration:   time.Duration(meta.Duration) * time.Second,
		SongID:     meta.ID,
		Source:     SourcePlatform,
	}, nil
}

func (s *Song) fetchInternetSong(url string) (*Song, error) {
	u, err := urlstd.Parse(url)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", url, err)
	}

	hash := crc32.ChecksumIEEE([]byte(u.Host))

	contentType, err := interentUtil.GetContentType(u.String())
	if err != nil {
		return nil, fmt.Errorf("error determining content type for URL %q: %w", url, err)
	}

	if !interentUtil.IsValidStreamType(contentType) {
		return nil, fmt.Errorf("invalid stream type for URL %q: %s", url, contentType)
	}

	return &Song{
		Title:      u.Host,
		PublicLink: url,
		StreamURL:  u.String(),
		Thumbnail:  Thumbnail{},
		Duration:   -1,
		SongID:     fmt.Sprintf("%d", hash),
		Source:     SourceInternet,
	}, nil
}

func (s *Song) GetSongInfo(song *Song) (string, string, string, error) {
	switch song.Source {
	case SourcePlatform:
		return song.Title, song.Source.String(), song.PublicLink, nil
	case SourceInternet:
		return s.getInternetSongMetadata(song.StreamURL)
	default:
		return "", "", "", fmt.Errorf("unknown source: %v", song.Source)
	}
}

func (s *Song) getInternetSongMetadata(url string) (string, string, string, error) {
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

func (s *Song) FindPlatformByURL(url string) Platform {
	for platform, domains := range platformURLs {
		for _, domain := range domains {
			if strings.Contains(url, domain) {
				return platform
			}
		}
	}
	return ""
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
// 		Source:     SourcePlatform,
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
