module github.com/keshon/melodix

go 1.26

require (
	github.com/bwmarrin/discordgo v0.29.0
	github.com/caarlos0/env/v11 v11.4.1
	github.com/ebitengine/oto/v3 v3.4.0
	github.com/godeps/opus v1.0.3
	github.com/joho/godotenv v1.5.1
	github.com/keshon/buildinfo v0.1.0
	github.com/keshon/command v0.1.0
	github.com/keshon/datastore v0.1.1
	github.com/kkdai/youtube/v2 v2.10.6
	github.com/rs/zerolog v1.35.1
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
)

require (
	github.com/bitly/go-simplejson v0.5.1 // indirect
	github.com/cloudflare/circl v1.6.4 // indirect
	github.com/dlclark/regexp2/v2 v2.2.2 // indirect
	github.com/dop251/goja v0.0.0-20260618133527-c9b2ea77db59 // indirect
	github.com/ebitengine/purego v0.10.1 // indirect
	github.com/go-sourcemap/sourcemap v2.1.4+incompatible // indirect
	github.com/google/pprof v0.0.0-20260604005048-7023385849c0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/tetratelabs/wazero v1.12.0 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
)

replace github.com/bwmarrin/discordgo => ./pkg/discordgo-fork-dev
