package stream

import (
	"fmt"
	"net/http"
)

var allowedTypes = []string{
	"application/flv",
	"application/vnd.ms-wpl",
	"audio/aac",
	"audio/basic",
	"audio/flac",
	"audio/mpeg",
	"audio/ogg",
	"audio/vnd.audible",
	"audio/vnd.dece.audio",
	"audio/vnd.dts",
	"audio/vnd.rn-realaudio",
	"audio/vnd.wave",
	"audio/webm",
	"audio/x-aiff",
	"audio/x-m4a",
	"audio/x-matroska",
	"audio/x-ms-wax",
	"audio/x-ms-wma",
	"audio/x-mpegurl",
	"audio/x-pn-realaudio",
	"audio/x-scpls",
	"audio/x-wav",
	"video/3gpp",
	"video/mp4",
	"video/quicktime",
	"video/webm",
	"video/x-flv",
	"video/x-ms-video",
	"video/x-ms-wmv",
	"video/x-ms-asf",
}

type AudioStreamChecker struct{}

func New() *AudioStreamChecker {
	return &AudioStreamChecker{}
}

func (asc *AudioStreamChecker) GetContentType(url string) (string, error) {
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return "", fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	return resp.Header.Get("Content-Type"), nil
}

func (asc *AudioStreamChecker) IsValidStreamType(contentType string) bool {
	for _, validType := range allowedTypes {
		if contentType == validType {
			return true
		}
	}

	return false
}
