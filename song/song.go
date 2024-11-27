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
	"sort"
	"strings"
	"sync"
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
	Title          string               // title of the song
	PublicLink     string               // link to the song page
	StreamURL      string               // URL for streaming the song
	StreamFilepath string               // path for streaming the song
	Thumbnail      Thumbnail            // thumbnail image for the song
	Duration       time.Duration        // duration of the song
	SongID         string               // unique ID for the song
	Source         SongSource           // source type of the song
	YTVideo        *kkdai_youtube.Video // YouTube video data from `kkdai_youtube`
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
			for _, URL := range ytDlpURLs {
				song, err := s.fetchYoutubeSong(URL) // TODO: write new method for retrieveing song from yt-dlp
				if err != nil {
					return nil, err
				}
				songs = append(songs, song)
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
		song, err := s.FetchSongs(URLsOrTitle)
		if err != nil {
			return nil, err
		}
		songs = append(songs, song...)
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

func (s *Song) fetchYoutubeSong(url string) (*Song, error) {

	id, err := s.extractYoutubeID(url)
	if err != nil {
		return nil, err
	}

	song, err := KkdaiClient.GetVideo(id)
	if err != nil {
		if strings.Contains(err.Error(), "UNPLAYABLE") {
			return nil, fmt.Errorf("YouTube video is unplayable, possibly due to `region restrictions` or other issues.\n\n¯\\_(ツ)_/¯")
		}
		return nil, err
	}

	songStreamURL, err := yt_dlp.New().GetStreamURL(url) // a fix due to kkdai is broken atm
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
		StreamURL:  songStreamURL, //song.Formats.WithAudioChannels()[0].URL,
		Duration:   song.Duration,
		Thumbnail:  thumbnail,
		SongID:     song.ID,
		Source:     SourceYouTube,
		YTVideo:    song,
	}, nil
}

func (s *Song) fetchYoutubePlaylist(url string) ([]*Song, error) {
	var songs []*Song
	playlist, err := KkdaiClient.GetPlaylist(url)

	// Check if it's a YouTube Mix Playlist that `kkdai_youtube` doesn't natively support
	if err != nil && err.Error() == "extractPlaylistID failed: no playlist detected or invalid playlist ID" {
		urls, mixErr := youtube.New().FetchMixPlaylistVideoURLs(url)
		if mixErr != nil {
			return nil, mixErr
		}
		// Create a synthetic playlist from the fetched URLs
		playlist = &kkdai_youtube.Playlist{
			Videos: make([]*kkdai_youtube.PlaylistEntry, len(urls)),
		}
		for i, id := range urls {
			playlist.Videos[i] = &kkdai_youtube.PlaylistEntry{ID: id}
		}
	} else if err != nil {
		return nil, err
	}

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		index = make(map[string]int, len(playlist.Videos))
	)

	for i, video := range playlist.Videos {
		index[video.ID] = i
		wg.Add(1)
		go func(videoID string) {
			defer wg.Done()
			videoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)
			song, fetchErr := s.fetchYoutubeSong(videoURL)
			if fetchErr != nil {
				fmt.Printf("Error fetching song for video ID %s: %v\n", videoID, fetchErr)
				return
			}
			mu.Lock()
			songs = append(songs, song)
			mu.Unlock()
		}(video.ID)
	}

	wg.Wait()

	// Stable sort to maintain the order based on the original playlist
	sort.SliceStable(songs, func(i, j int) bool {
		return index[songs[i].SongID] < index[songs[j].SongID]
	})

	return songs, nil
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
	"17live",
	"1news.co.nz",
	"1tv",
	"20min",
	"23video",
	"247sports",
	"24tv.ua",
	"3qsdn",
	"3sat",
	"4tube",
	"56.com",
	"6play",
	"7plus",
	"8tracks",
	"9c9media",
	"9gag",
	"9news",
	"9now.com.au",
	"abc.net.au",
	"abcnews",
	"abcotvs",
	"abematv",
	"academicearth",
	"acast",
	"acfunbangumi",
	"acfunvideo",
	"animationdigitalnetwork",
	"adobeconnect",
	"adobetv",
	"adultswim",
	"aenetworks",
	"aeonco",
	"airtv",
	"aitubekzvideo",
	"aliexpresslive",
	"aljazeera",
	"allocine",
	"allstar",
	"allstarprofile",
	"alphaporno",
	"alsace20tv",
	"alsace20tvembed",
	"altcensored",
	"alura",
	"aluracourse",
	"amadeustv",
	"amara",
	"amazonminitv",
	"amazonreviews",
	"amazonstore",
	"amcnetworks",
	"americastestkitchen",
	"americastestkitchenseason",
	"amhistorychannel",
	"anchorfm",
	"anderetijden",
	"angel",
	"animalplanet",
	"ant1news.gr",
	"antenna",
	"anvato",
	"aol.com",
	"apa",
	"aparat",
	"appleconnect",
	"appledaily",
	"applepodcasts",
	"appletrailers",
	"archive.org",
	"arcpublishing",
	"ard",
	"ardmediathek",
	"arkena",
	"art19",
	"arte.sky.it",
	"artetv",
	"asobichannel",
	"asobistage",
	"atresplayer",
	"atscaleconfevent",
	"atvat",
	"audimedia",
	"audioboom",
	"audiomack",
	"audius",
	"awaan",
	"axs.tv",
	"azmedien",
	"baiduvideo",
	"banbye",
	"banbyechannel",
	"bandaichannel",
	"bandcamp",
	"bandlab",
	"bannedvideo",
	"bbc.co.uk",
	"bbvtv",
	"beacontv",
	"beatport",
	"beeg",
	"behindkink",
	"bellator",
	"bellmedia",
	"berufetv",
	"bet",
	"bfi",
	"bfmtv",
	"bibeltv",
	"bigflix",
	"bigo",
	"bild",
	"bilibili",
	"biliintl",
	"bililive",
	"biobiochiletv",
	"biography",
	"bitchute",
	"bitchute",
	"blackboardcollaborate",
	"bleacherreport",
	"blerp",
	"blogger.com",
	"bloomberg",
	"bluesky",
	"bokecc",
	"bongacams",
	"boosty",
	"bostonglobe",
	"box",
	"boxcast",
	"bpb.de",                    // Bundeszentrale für politische Bildung
	"br.de",                     // Bayerischer Rundfunk
	"brainpop.com",              // BrainPOP
	"bravotv.com",               // BravoTV
	"breitbart.com",             // BreitBart
	"brightcove.com",            // Brightcove
	"classes.brilliantpala.org", // Brilliantpala Classes
	"elearn.brilliantpala.org",  // Brilliantpala Elearn
	"bt.no",                     // Bergens Tidende Articles
	"bundesliga.com",            // Bundesliga
	"bundestag.de",              // Bundestag
	"businessinsider.com",       // Business Insider
	"buzzfeed.com",              // BuzzFeed
	"byutv.org",                 // BYUtv
	"caffeine.tv",               // CaffeineTV
	"callin.com",                // Callin
	"caltrans.com",              // Caltrans
	"cam4.com",                  // CAM4
	"camdemy.com",               // Camdemy
	"cammodels.com",             // CamModels
	"camsoda.com",               // Camsoda
	"camtasia.com",              // CamtasiaEmbed
	"canal1.com.co",             // Canal1
	"canalalpha.ch",             // CanalAlpha
	"canalc2.tv",                // canalc2.tv
	"mycanal.fr",                // Canalplus
	"piwiplus.fr",               // Canalplus
	"caracoltv.com",             // CaracolTvPlay
	"cartoonnetwork.com",        // Cartoon Network
	"cbc.ca",                    // CBC
	"cbssports.com",             // CBS Sports
	"cbsnews.com",               // CBS News
	"ccma.cat",                  // CCMA
	"cctv.com",                  // CCTV
	"cdapl.com",                 // CDA
	"ceskatelevize.cz",          // Ceska Televize
	"cgtn.com",                  // CGTN
	"chaturbate.com",            // Chaturbate
	"chilloutzone.net",          // Chilloutzone
	"charlierose.com",           // Charlie Rose
	"cielotv.it",                // cielotv.it
	"cinetecamilano.it",         // CinetecaMilano
	"cineverse.com",             // Cineverse
	"ciscolive.com",             // CiscoLive
	"clubic.com",                // Clubic
	"cloudflarestream.com",      // CloudflareStream
	"comedycentral.com",         // Comedy Central
	"cnn.com",                   // CNN
	"condenast.com",             // Conde Nast
	"cp24.com",                  // CP24
	"cpac.ca",                   // CPAC
	"crack.com",                 // Cracked
	"crackle.com",               // Crackle
	"craftsy.com",               // Craftsy
	"crunchyroll.com",           // Crunchyroll
	"cspan.org",                 // C-SPAN
	"ctv.ca",                    // CTV
	"cu.ntv.co.jp",              // Nippon Television Network
	"cultureunplugged.com",      // Culture Unplugged
	"curiositystream.com",       // CuriosityStream
	"cw.com",                    // CWTV
	"cybrary.com",               // Cybrary
	"dailymail.co.uk",           // Daily Mail
	"dailymotion.com",           // Dailymotion
	"dailywire.com",             // DailyWire
	"dangplay.com",              // Dangalplay
	"daum.net",                  // Daum
	"dbtv.com",                  // DBTV
	"dctptv.com",                // DctpTv
	"deezer.com",                // Deezer
	"democracynow.org",          // Democracy Now
	"detik.com",                 // Detik
	"disney.com",                // Disney
	"dlive.tv",                  // DLive
	"douyin.com",                // Douyin
	"douyu.com",                 // DouyuTV
	"dplay.com",                 // DPlay
	"drtv.dk",                   // drtv
	"duboku.io",                 // duboku.io
	"dumpert.nl",                // Dumpert
	"dw.com",                    // DW
	"ebaumsworld.com",           // EbaumsWorld
	"elonet.fi",                 // Elonet
	"elpais.com",                // El País
	"eltrecetv.com.ar",          // El Trece TV
	"embedly.com",               // Embedly
	"epoch.com",                 // Epoch
	"eporner.com",               // Eporner
	"ertflix.gr",                // ERTFLIX
	"espn.com",                  // ESPN
	"eurosport.com",             // Eurosport
	"expressen.se",
	"facebook.com",
	"fathom.com",
	"faz.net",
	"fc2.com",
	"fczenit.com",
	"fifa.com",
	"filmon.com",
	"filmweb.pl",
	"fivethirtyeight.com",
	"fivetv.com",
	"flextv.com",
	"flickr.com",
	"floatplane.com",
	"folketinget.dk",
	"foodnetwork.com",
	"footyroom.com",
	"formula1.com",
	"fox.com",
	"fox9.com",
	"fox9news.com",
	"foxnews.com",
	"foxnewsvideo.com",
	"foxsports.com",
	"fptplay.vn",
	"franceculture.fr",
	"franceinter.fr",
	"francetv.fr",
	"francetvinfo.fr",
	"francetvsite.fr",
	"freesound.org",
	"freespeech.org",
	"freetv.com",
	"freetvmovies.com",
	"frontendmasters.com",
	"fujitvfodplus7.com",
	"funimation.com",
	"funk.com",
	"funker530.com",
	"fux.com",
	"fuyintv.com",
	"gab.com",
	"gabtv.com",
	"gaia.com",
	"gamedevtv.com",
	"gamejolt.com",
	"gamespot.com",
	"gamestar.de",
	"gaskrank.tv",
	"gazeta.com",
	"gbnews.uk",
	"gdcvault.com",
	"gedidigital.it",
	"gem.cbc.ca",
	"genius.com",
	"germanupa.de",
	"getcourse.ru",
	"gettr.com",
	"giantbomb.com",
	"glattvisiontv.ch",
	"glide.me",
	"globalplayer.com",
	"globo.com",
	"glomex.com",
	"gmanetwork.com",
	"go.com",
	"godiscovery.com",
	"godresource.com",
	"gofile.io",
	"golem.de",
	"goodgame.ru",
	"google.com",
	"googledrive.com",
	"goplay.be",
	"gopro.com",
	"goshgay.com",
	"gotostage.com",
	"gpu-techconf.com",
	"graspop.be",
	"gronkh.tv",
	"groupon.com",
	"harpodeon.com",
	"hbo.com",
	"hearthis.at",
	"heise.de",
	"hellporno.com",
	"hetklokhuis.nl",
	"hgtv.com",
	"hgtv.de",
	"hidive.com",
	"historicfilms.com",
	"history.com",
	"hitrecord.org",
	"hketv.com",
	"hollywoodreporter.com",
	"holodex.net",
	"hotnewhiphop.com",
	"hotstar.com",
	"hrfernsehen.de",
	"hrti.hr",
	"hse.com",
	"huajiao.com",
	"huffpost.com",
	"hungama.com",
	"huya.com",
	"hypem.com",
	"hytale.com",
}
