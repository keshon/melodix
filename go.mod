module github.com/keshon/melodix

go 1.25.0

require (
	github.com/bwmarrin/discordgo v0.29.0
	github.com/ebitengine/oto/v3 v3.4.0
	github.com/keshon/buildinfo v0.1.0
	github.com/keshon/commandkit v0.1.0
	github.com/keshon/datastore v0.1.1
)

require (
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/ebitengine/purego v0.9.0 // indirect
	github.com/tetratelabs/wazero v1.11.0 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

require (
	github.com/bdandy/go-errors v1.2.2 // indirect
	github.com/bdandy/go-socks4 v1.2.3
	github.com/bitly/go-simplejson v0.5.1 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/dop251/goja v0.0.0-20260311135729-065cd970411c // indirect
	github.com/go-sourcemap/sourcemap v2.1.4+incompatible // indirect
	github.com/godeps/opus v1.0.3
	github.com/google/pprof v0.0.0-20260302011040-a15ffb7f9dcc // indirect
	golang.org/x/text v0.35.0 // indirect
)

require (
	github.com/caarlos0/env/v11 v11.4.0
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/joho/godotenv v1.5.1
	github.com/kkdai/youtube/v2 v2.10.5
	golang.org/x/net v0.52.0
)

replace github.com/bwmarrin/discordgo => ./pkg/discordgo-fork
