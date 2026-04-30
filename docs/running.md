
# Running Melodix

This guide covers both modes:
- Discord bot
- CLI player

---

## Requirements

- FFmpeg in PATH
- yt-dlp (recommended)

---

## Discord bot

### Step 1: Create bot

1. Open https://discord.com/developers/applications
2. Create application
3. Go to "Bot"
4. Create bot and copy token

Enable intents:
- Presence
- Server Members
- Message Content

---

### Step 2: Invite bot

Replace YOUR_APPLICATION_ID:

https://discord.com/oauth2/authorize?client_id=YOUR_APPLICATION_ID&scope=bot&permissions=3238912

---

### Step 3: Configure

Create `.env` or export variables:

```env
DISCORD_TOKEN=your-token
````

Optional variables:

| Variable                  | Description                                                | Default                 |
| ------------------------- | ---------------------------------------------------------- | ----------------------- |
| `STORAGE_PATH`            | Path for bot data (e.g. command state).                    | `./data/datastore.json` |
| `INIT_SLASH_COMMANDS`     | Set to `true` to register slash commands on every startup. | `false`                 |
| `DEVELOPER_ID`            | Your Discord user ID for developer-only commands.          | (none)                  |
| `DISCORD_GUILD_BLACKLIST` | Comma-separated guild IDs the bot will leave.              | (none)                  |
| `VOICE_READY_DELAY_MS`    | Delay after joining VC before sending Opus (avoids OP4 race). | `500`                 |
| `WS_SILENCE_TIMEOUT`      | Treat gateway as unhealthy after this long without messages. | `2m`                  |
| `DISCORD_UNHEALTHY_MODE`  | Action on unhealthy: `restart-session`, `restart-voice`, `ignore`. | `restart-session` |
| `DISCORD_UNHEALTHY_GRACE` | In `restart-session`: ignore first N unhealthy signals within window (still invalidates sinks). | `0` |
| `DISCORD_UNHEALTHY_WINDOW`| Window for `DISCORD_UNHEALTHY_GRACE` counting.             | `1m`                    |
| `PLAYER_TRANSPORT_RECOVERY_MODE` | On voice transport failure: `hard` (rejoin VC) or `soft` (reopen stream first, then hard fallback). | `hard` |
| `PLAYER_TRANSPORT_SOFT_ATTEMPTS` | In `soft` mode: how many soft retries before hard fallback. | `1` |
| `COMMAND_TIMEOUT`         | Hard timeout for a single command execution.               | `30s`                   |
| `COMMAND_PARALLELISM`     | Max number of concurrently running command handlers.       | `16`                    |


---

### Step 4: Run

```bash
go build -o melodix-discord ./cmd/discord
./melodix-discord
```

---

## CLI player

No Discord setup required.

```bash
go build -o melodix-cli ./cmd/cli
./melodix-cli
```

Commands:

* play <url or query>
* next
* stop
* queue
* status
* quit

---

## Docker

See `docker/README.md`