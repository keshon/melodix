package kkdai

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/keshon/melodix/internal/music/parsers"

	_ "github.com/bdandy/go-socks4"
	youtube "github.com/kkdai/youtube/v2"
	"golang.org/x/net/proxy"
)

const (
	channels   = 2
	sampleRate = 48000
	frameSize  = 960 // 20ms at 48kHz
)

type KKDAIStreamer struct{}

func (s *KKDAIStreamer) GetLinkStream(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	return kkdaiLink(track, seekSec)
}
func (s *KKDAIStreamer) GetPipeStream(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	return kkdaiPipe(track, seekSec)
}
func (s *KKDAIStreamer) SupportsPipe() bool {
	return true
}

// not used
func NewKkdaiClient(proxyStr string) (*youtube.Client, string) {
	if proxyStr == "" {
		fmt.Println("[kkdai] no proxy selected, going raw")
		return &youtube.Client{
			HTTPClient: &http.Client{
				Timeout: 15 * time.Second,
			},
		}, ""
	}

	proxyURL, err := url.Parse(proxyStr)
	if err != nil {
		fmt.Printf("[kkdai] invalid proxy format: %v\n", err)
		return &youtube.Client{}, ""
	}

	var transport *http.Transport

	switch proxyURL.Scheme {
	case "http", "https":
		fmt.Printf("[kkdai] using HTTP proxy: %s\n", proxyStr)
		transport = &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
	case "socks5":
		fmt.Printf("[kkdai] using SOCKS5 proxy: %s\n", proxyStr)
		auth := &proxy.Auth{}
		if proxyURL.User != nil {
			auth.User = proxyURL.User.Username()
			if pass, ok := proxyURL.User.Password(); ok {
				auth.Password = pass
			}
		}
		dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, auth, &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 10 * time.Second,
		})
		if err != nil {
			fmt.Printf("[kkdai] SOCKS5 dialer error: %v\n", err)
			break
		}
		transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		}
	case "socks4":
		fmt.Printf("[kkdai] using SOCKS4 proxy: %s\n", proxyStr)
		dialer, err := proxy.FromURL(proxyURL, &net.Dialer{
			Timeout: 10 * time.Second,
		})
		if err != nil {
			fmt.Printf("[kkdai] SOCKS4 dialer error: %v\n", err)
			break
		}
		transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		}
	default:
		fmt.Printf("[kkdai] unsupported proxy scheme: %s\n", proxyURL.Scheme)
	}

	if transport == nil {
		fmt.Println("[kkdai] falling back to default client—no proxy for this one, poor thing 😢")
		return &youtube.Client{
			HTTPClient: &http.Client{
				Timeout: 15 * time.Second,
			},
		}, ""
	}

	return &youtube.Client{
		HTTPClient: &http.Client{
			Timeout:   15 * time.Second,
			Transport: transport,
		},
	}, proxyStr
}
