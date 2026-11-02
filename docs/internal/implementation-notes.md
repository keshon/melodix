# Implementation notes

Historical evidence retained during the documentation revision on 2026-11-02.
These observations describe the investigated versions and network conditions;
use the public guides for current behavior.

## Tried and rejected

A codebase shows what is, never what was tried and undone. Each of these cost
a debugging session at least once, and two of them were still described as
current behaviour in this document long after they had been reverted — which
is the failure this section exists to prevent.

- **Gating live broadcasts on `hlsManifestUrl`.** Exactly backwards:
  Apple-platform clients are served an HLS manifest for ordinary VOD as well,
  so the gate rejected every playable video for a whole release and caught no
  live one. Live is refused upstream instead, by playability status.
- **The `ANDROID_VR` InnerTube client** — kkdai's default. Its URLs answer
  403 to any open-ended request, so every YouTube parser died on its first
  read and the chain walked down to yt-dlp. Neither the User-Agent nor an `n`
  parameter was involved; both were measured against the live CDN and ruled
  out.
- **Putting the anti-skip buffer below recovery.** A reopen tore the buffer
  down along with the stream it wrapped, so the consumer blocked for the
  whole reconnect and the gap was fully audible — the opposite of what the
  buffer exists for.
- **Answering YouTube stutter with a deeper buffer.** googlevideo paces an
  open-ended response at roughly 1.9x the format's own bitrate, so a lead
  accrues only as fast as bytes arrive and no depth outlasts a link running
  below real time. Fixed by changing the fetch shape instead.
- **Treating a mid-track connection reset as a clean end.** With ffmpeg in
  front, a dropped connection arrives as a closed pipe and reads as EOF;
  passthrough removed that layer and the reset arrives raw, so the track died
  instead of resuming.
