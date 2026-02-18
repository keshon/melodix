![# Header](https://raw.githubusercontent.com/keshon/melodix/master/assets/readme-banner.webp)

[![GoDoc](https://godoc.org/github.com/keshon/melodix?status.svg)](https://godoc.org/github.com/keshon/melodix) [![Go report](https://goreportcard.com/badge/keshon/melodix)](https://goreportcard.com/report/github.com/keshon/melodix)

# 🎵 Melodix — Self-hosted Discord music bot

Melodix is my pet project written in Go that plays audio from YouTube and audio streaming links to Discord voice channels. It's a continuation of my original buggy [prototype](https://github.com/keshon/melodix-player).

## 🌟 Features Overview

### 🎧 Playback Support
- 🎶 Track added by song name or YouTube/Soundcloud link.
- 📻 Internet radio streaming links (24/7 playback).

### ⚙️ Additional Features
- 🌐 Operation across multiple Discord servers.
- 🔄 Playback auto-resume support for connection interruptions.

### ⚠️ Current Limitations
- ⏸️ Playback auto-resume feature may cause noticeable pauses at times.
- ⏩ Playback speed may sometimes slightly vary.
- 🚫 The bot cannot play YouTube live streams or region-locked videos.
- 🚫 Not every radio streams are supported.
- 🐞 Can hang or become unresponsive. It's not bug-free.

## 🚀 Try Melodix

You can test out Melodix in two ways:
- 🖥️ Download [compiled binaries](https://github.com/keshon/melodix/releases) (available only for Windows). Ensure [FFMPEG](https://www.ffmpeg.org/) is installed on your system and added to the global PATH variable. Follow the "Create bot in Discord Developer Portal" section to set up the bot in Discord.

- 🎙️ Join the [Official Discord server](https://discord.gg/NVtdTka8ZT) and use the voice and `#bot-spam` channels.

## 📝 Available Discord Commands

### 🕯️ Information

- **/about** — Discover the origin of this bot
- **/help** — Get a list of available commands

### 🎵 Music

- **/music** — Control music playback

### ⚙️ Settings

- **/commands** — Manage or inspect commands
- **/maintenance** — Bot maintenance commands


### 💡 Usage Examples
To use the `play` command, provide a YouTube video title or URL:
```
/music play Never Gonna Give You Up
/music play https://www.youtube.com/watch?v=dQw4w9WgXcQ
/music play http://stream-uk1.radioparadise.com/aac-320
```

## 🔧 How to Set Up the Bot

### 🔗 Create a Bot in the Discord Developer Portal
To add Melodix to a Discord server, follow these steps:

1. Create an application in the [Discord Developer Portal](https://discord.com/developers/applications) and obtain the `APPLICATION_ID` (in the General section).
2. In the Bot section, enable `PRESENCE INTENT`, `SERVER MEMBERS INTENT`, and `MESSAGE CONTENT INTENT`.
3. Use the following link to authorize the bot: `discord.com/oauth2/authorize?client_id=YOUR_APPLICATION_ID&scope=bot&permissions=2150714368`
   - Replace `YOUR_APPLICATION_ID` with your Bot's Application ID from step 1.
4. Select a server and click "Authorize".
5. Grant the necessary permissions for Melodix to function correctly (access to text and voice channels).

After adding the bot, build it from source or download [compiled binaries](https://github.com/keshon/melodix-player/releases). Docker deployment instructions are available in `docker/README.md`.

### 🛠️ Building Melodix from Sources
This project is written in Go, so ensure your environment is ready. Use the provided scripts to build Melodix from source:
- `bash-and-run.bat`: Build the debug version and execute.

Rename `.env.example` to `.env` and store your Discord Bot Token in the `DISCORD_TOKEN` variable. 
Install [FFMPEG](https://ffmpeg.org/) and add it to global PATH variable.
Install yt-dlp and add it to global PATH variable.

### 🐳 Docker Deployment
For Docker deployment, refer to `docker/README.md` for specific instructions.

## 📝 Environment Variables

You can configure Melodix using environment variables by creating a `.env` file in your project root directory. The following variables should be set in your `.env` file:

```env
# Discord Bot Token (Required)
DISCORD_TOKEN=your-discord-bot-token
```

## 🆘 Support
For any questions, get support in the [Official Discord server](https://discord.gg/NVtdTka8ZT).

## 📜 License
Melodix is licensed under the [MIT License](https://opensource.org/licenses/MIT).