![# Header](https://raw.githubusercontent.com/keshon/melodix/master/assets/readme-banner.webp)

# 🎵 Melodix — Self-hosted Discord music bot

Melodix is my pet project written in Go that plays audio from YouTube and audio streaming links to Discord voice channels. It's a continuation of my original buggy [prototype](https://github.com/keshon/melodix-player).

## 🌟 Features Overview

### 🎧 Playback Support
- 🎶 Single track added by song name or YouTube link.
- 🎶 Multiple tracks added via multiple YouTube links (space-separated).
- 🎶 Tracks from public user YouTube playlists.
- 🎶 Tracks from YouTube "MIX" playlists.
- 📻 Internet radio streaming links (24/7 playback).

### ⚙️ Additional Features
- 🌐 Operation across multiple Discord servers.
- 📜 Access to recent track statistics and history of `play` commands.
- 🔄 Playback auto-resume support for connection interruptions.

### ⚠️ Current Limitations
- 🚫 The bot cannot play YouTube live streams or region-locked videos.
- ⏸️ Playback auto-resume support may cause noticeable pauses at times.
- ⏩ Playback speed may sometimes slightly vary.
- 🐞 It's not bug-free.

## 🚀 Try Melodix

You can test out Melodix in two ways:
- 🖥️ Download [compiled binaries](https://github.com/keshon/melodix/releases) (available only for Windows). Ensure [FFMPEG](https://www.ffmpeg.org/) is installed on your system and added to the global PATH variable. Follow the "Create bot in Discord Developer Portal" section to set up the bot in Discord.

- 🎙️ Join the [Official Discord server](https://discord.gg/NVtdTka8ZT) and use the voice and `#bot-spam` channels.

## 📝 Available Discord Commands

### ▶️ Playback Commands
- `!play [title|url]` — Parameters: song name, YouTube URL, audio streaming URL.
- `!skip` — Skip to the next track in the queue.
- `!stop` — Stop playback, clear the queue, and leave the voice channel.

### 📋 Advanced Playback Commands
- `!list` — Show the current song queue.
- `!pause`, `!resume` — Pause/resume current playback.

### 📊 Information Commands
- `!now` — Show the currently playing song. Convenient for radio streaming.
- `!stats` — Show track statistics with total playback duration and count.
- `!log` — Show recent `play` commands by users.

### ⚙️ Utility Commands
- `!set-prefx [new_prefix]` — Set a custom prefix for a guild to avoid collisions with other bots.
- `melodix-reset-prefix` — Revert to the default prefix `!`.

### ℹ️ General Commands
- `!about` — Show bot information.
- `!help` — Show a help cheatsheet.

### 💡 Usage Examples
To use the `play` command, provide a YouTube video title or URL:
```
!play Never Gonna Give You Up
!play https://www.youtube.com/watch?v=dQw4w9WgXcQ
!play http://stream-uk1.radioparadise.com/aac-320
```
Play multiple tracks (the second link will be added to the queue):
```
!play https://www.youtube.com/watch?v=dQw4w9WgXcQ https://www.youtube.com/watch?v=OorZcOzNcgE
```

## 🔧 How to Set Up the Bot

### 🔗 Create a Bot in the Discord Developer Portal
To add Melodix to a Discord server, follow these steps:

1. Create an application in the [Discord Developer Portal](https://discord.com/developers/applications) and obtain the `APPLICATION_ID` (in the General section).
2. In the Bot section, enable `PRESENCE INTENT`, `SERVER MEMBERS INTENT`, and `MESSAGE CONTENT INTENT`.
3. Use the following link to authorize the bot: `discord.com/oauth2/authorize?client_id=YOUR_APPLICATION_ID&scope=bot&permissions=36727824`
   - Replace `YOUR_APPLICATION_ID` with your Bot's Application ID from step 1.
4. Select a server and click "Authorize".
5. Grant the necessary permissions for Melodix to function correctly (access to text and voice channels).

After adding the bot, build it from source or download [compiled binaries](https://github.com/keshon/melodix-player/releases). Docker deployment instructions are available in `docker/README.md`.

### 🛠️ Building Melodix from Sources
This project is written in Go, so ensure your environment is ready. Use the provided scripts to build Melodix from source:
- `bash-and-run.bat` (or `.sh` for Linux): Build the debug version and execute.
- `build-release.bat` (or `.sh` for Linux): Build the release version.
- `build-dist-assemble`: Build the release version and assemble it as a distribution package (Windows only).

Rename `.env.example` to `.env` and store your Discord Bot Token in the `DISCORD_TOKEN` variable. Install [FFMPEG](https://ffmpeg.org/) (only recent versions are supported).

### 🐳 Docker Deployment
For Docker deployment, refer to `docker/README.md` for specific instructions.

## 🆘 Support
For any questions, get support in the [Official Discord server](https://discord.gg/NVtdTka8ZT).

## 📜 License
Melodix is licensed under the [MIT License](https://opensource.org/licenses/MIT).