# Changelog

Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-10-18

First public release. A Go reimplementation of a subset of
ShittyRandomPhotoScreenSaver, targeting a low-power x86 mini host.

### Added

- **Screensaver (`cmd/scr`)** — pure Go, no cgo, Direct3D 11 via gioui. Full-screen
  clock, current-conditions panel, details row, forecast line.
- **Weather-driven sky** — seven classes (`clear`, `cloudy`, `fog`, `rain`, `snow`,
  `thunder`, `dust`) mapped from WMO weather codes, with cloud cover able to promote a
  clear code into `cloudy`.
- **Day and night from the API's `is_day`**, with a star field baked into the night sky
  at zero per-frame cost.
- **Real moon phases** from the forecast API's `daily.moon_phase`: lit shape only, no
  outline, glow built as a bloom of the lit region so the dark side is transparent and
  filled by the halo. New moon draws nothing.
- **Cloud occlusion of the sun and the moon**, including the large cloud bands, in both
  day and night.
- **Rain-risk alert** — rolling precipitation probability over a 3 h window (configurable
  to 6 h), tiered Blue/Yellow/Orange/Red on the Chinese national rainstorm thresholds,
  with an optional 1.2 s breathing pulse.
- **Class-change crossfade** — 9 s crossfade, incoming clouds entering from off-screen.
- **Cloud drift speed control** — per-cloud spacing controller plus a shared band span
  and phase, so the sky does not bunch up and then go empty.
- **Settings panel** (`WeatherScr.scr /c`) editing every config key, with a place search
  that stores coordinates.
- **Installer (`cmd/setup`)** — installs to `C:\Windows\System32\WeatherScr.scr`, records
  the previous screensaver for uninstall, creates desktop shortcuts, registers an
  *Installed apps* entry, elevates once for the system-directory copy and rolls the
  registry back if that copy fails.
- **Application icon** — 9-frame `.ico` (16 to 256 px) embedded in both the `.scr` and
  the installer.
- **Headless renderer (`cmd/fxshot`)** — renders the production layout path to PNG with
  no display, and reports per frame how much of the sun/moon disc the clouds cover.
- **Cache fallback** — when the forecast request fails, the newest cached sample is used,
  selected deterministically by cache timestamp.

### Notes

- Panel background is fully transparent with no border, which differs from the upstream
  default.
- `wallpaper_dir` and `wallpaper_interval_s` are read and preserved for settings-shape
  compatibility; this build draws no wallpaper.
- Resident memory is 121–142 MB, above the 120 MB budget during the class crossfade.
  Stated rather than smoothed.

[1.0.0]: https://github.com/DragonTTDream/WeatherScr/releases/tag/v1.0.0
