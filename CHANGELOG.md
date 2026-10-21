# Changelog

Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Shape mask for the lightning flash: the flash glow is now built by Gaussian-blurring
  the bolt sprite that the flash is drawing (`bolt1..3.png`, sigma = 0.03 x screen
  width, normalised to its peak), so the lit area follows the bolt's own outline
  instead of a circle. Cached per (bolt index, screen size); build cost measured
  7-8 ms per bolt at 1080x1920. Measured anisotropy at flash = 0.30, 0.15w from the
  anchor: along the bolt 5.6 / 3.7 / 8.4 levels vs perpendicular 0.2 / 0.7 / 0.0
  levels (ratio 25 / 5.0 / inf for the three bolts); increment at 0.6w = 0.0 levels.
- Six self-checks in `ui/sunedge_test.go`: sun profile has no edge, flash shape is the
  bolt, per-frame cloud speed change, cloud screen-motion smoothness, per-band wrap
  fully offscreen, and the particle-cloud recycle regression. All six print measured
  numbers; the measured values are listed in `out/notes-sunedge.md`.

### Changed

- Installer modernization (B3): `cmd/setup` now carries an application manifest
  (`cmd/setup/WeatherScr_setup.exe.manifest`, embedded via `rsrc -manifest` in the
  `icon` target): Common-Controls 6.0.0.0 (themed controls), `asInvoker` (elevation
  still goes through the existing `relaunchElevated` self-bootstrap), DPI awareness
  (`dpiAware=true` + `dpiAwareness=PerMonitorV2, PerMonitor`). The single-page GUI
  became a 4-page wizard (welcome -> intro -> install log -> finish) with
  Back/Next/Install/Finish/Cancel; window size is computed from content and clamped
  to 90% of the work area (`SystemParametersInfoW(SPI_GETWORKAREA)`), minimum
  480x420, centered, so no button needs a manual window resize to be reachable.
  `cmd/scr` keeps no manifest (gioui handles DPI itself; a manifest there risks
  changing the rendering path). Install/uninstall logic untouched; the uninstall
  rollback key `PreviousScr` (saved pre-install, restored on uninstall with a
  `WM_SETTINGCHANGE` broadcast) is verified in place.

- Windows builds of `cmd/scr` and `cmd/setup` link with `-ldflags="-s -w -H=windowsgui"`,
  so the PE Subsystem is 2 (GUI) instead of 3 (console): starting the screensaver no
  longer opens a console window. Measured on the two products built by the check
  command: `/tmp/b1_scr.exe` Subsystem = 2, `/tmp/b1_setup.exe` Subsystem = 2
  (the shipped pair measured 3 / 3 before). Logging path is unchanged
  (exe-dir `scr_run.log`), it never needed a console.
- The settings dialog is a topmost window now: `normalizeDialog` sets `WS_EX_TOPMOST`
  (0x00000008) and its sized `SetWindowPos` uses `HWND_TOPMOST` without `SWP_NOZORDER`,
  followed by a `SWP_NOMOVE|SWP_NOSIZE` topmost pass after `ShowWindow(SW_RESTORE)`.
  Only the settings window goes through this; the fullscreen screensaver path
  (`app.Fullscreen.Option()` + `app.Decorated(false)`) is untouched.
- The desktop settings shortcut no longer points at the `.scr`. The shell association
  `HKCR\scrfile\shell\open\command = "%1" /S` rewrites every shell-launched start of a
  `.scr`, so the `/c` argument never survives a double click and the shortcut opened
  the fullscreen saver. The shortcut now targets `<install dir>\WeatherScr_setup.exe`
  with `--settings`, and that mode starts `"C:\Windows\System32\WeatherScr.scr" /c`
  through a direct `CreateProcess` (no ShellExecute, no association). Working directory
  and icon location are unchanged; the start shortcut keeps `WeatherScr.scr /s`.

- Sunlight now covers the whole sky. The sun's sky contribution gains a second, very
  slow Lorentz term on top of the near field spot: `A_sky = 90 x dim`, `L_sky = 0.9 x`
  bake width, added outside the 1.5w cos^2 window so the four canvas corners get lifted
  too (measured net lift at the corners vs a no-sun baseline: 41.7 / 69.0 / 20.3 / 21.3
  levels; profile steepness stays under 3 levels per pixel, so no ring edge appears).
- Cloud shadow layer: every particle cloud and every cloud band casts a soft shadow on
  the sky *before* the cloud body is drawn (same sprite, colour (0,0,0), alpha =
  cloud alpha x 0.45, size x 1.15 as penumbra). Day only - at night there is no
  sunlight to block and the night sky would be crushed. Measured: at the cloud-covered
  point the sky reads 64.7 levels below a cloud-free sky point in the same frame.
- Three Kenney CC0 textures for the sun's shape per class: `sunrays.png` (Smoke
  Particles `flash05`, 512 px wide) drawn additively at the sun for the disc classes,
  `sunglow.png` (Light Masks `circle_a`, 1024 px wide) for the classes that draw no
  disc, and `flashglow.png` (Light Masks `circle_a`, 512 px wide) as the soft mask of
  the lightning flash.

### Changed

- Additive sunlight is now the sum of two analytic long-tail decays only: near field
  `A_n = 150 x dim`, `L_n = 0.55 x bake width` (was `A = 200`, `L = 0.18w`) plus the
  sky-wide `A_s = 90 x dim`, `L_s = 0.9w`. Neither term is truncated to zero, and the
  wide glow texture is no longer the mask of the additive light: `sunglow.png` is a
  soft shape base drawn 1.6 x bake width (rim at 0.8w, off screen) and `sunrays.png`
  is drawn 1.4 x bake width (was 0.57) so its rim is off screen too. Measured on the
  540x960 bake: net lift at 0.45w = 117.7 of 192.0 peak (61%) clear, 33.0 of 55.0 (60%)
  overcast; adjacent-pixel peak 1.67 / 1.00 levels; at the old texture rim (0.465w) the
  20 px inside vs 20 px outside averages differ by 1.60 levels (overcast).
- `coupleClouds` is a low-pass filter now: `vx += clamp(target - vx, +/- cloudVxSlew)`
  with `cloudVxSlew = 0.0004` per frame, gain `cloudCoupleK` 0.05 -> 0.012 and a dead
  zone `cloudGapDead = 0.15` (gap error within +/-15% is not corrected). The clamp
  range 0.002-0.015 and the parallax base-speed spread are unchanged; the no-overtake
  clamp now acts on the target so it cannot jump the speed in one frame either.
  Measured over 600 frames at 15 fps: max |delta vx| = 0.000400 (= the slew cap),
  max per-frame screen motion 1.080 px, max acceleration 0.0288 px/frame^2.
- Cloud band wrap uses a per-band period `span_i = w + 2 x dw_i` (was one shared
  `w + 2 x average dw`), so a band is fully off screen at the moment it teleports:
  measured left edge 1848-2257 px at the wrap instant on a 1080 px screen (band widths
  768-1178 px), right edge 0.0235 px at entry. The shared speed and the left-edge
  phase convention are unchanged.
- The round flash mask `flashglow.png` is retired: the file stays in `ui/embed/` and
  no code references it any more.
- Two existing flash self-checks (`TestFlashLocalized`, `TestFlashAnchorIsBolt`) now
  measure the peak increment of the lit layer inside the bolt rect instead of the
  increment at the anchor pixel. Reason: the anchor is the sprite centre, which is
  empty in the bolt art (measured 0 pixels above alpha 128 in the 21x21 centre of
  bolt3), so with a bolt-shaped mask that single pixel is legitimately not lit.
  The locality ratio criterion (>= 8x / > 5x at 0.5w away) is unchanged.

- The lightning flash's glow is now the Kenney soft light mask centred on the bolt's
  landing point (0.70 x screen width), replacing the procedurally generated cos^2 disc;
  amplitude cap 0.30 and the cloud alpha boost 0.60 are unchanged. The bolt body is
  drawn from flash > 0.12 instead of > 0.25, so a low-amplitude flash shows lightning
  and not only light (measured 12095 bolt pixels at flash = 0.20, previously 0).

- The clock and weather text blocks are now drawn from a recording of their own draw
  commands. When every visible input is unchanged — text, placement, font metrics, ink,
  shadow — the layout, glyph shaping and path construction are skipped and the recording is
  replayed; any change rebuilds that block wholesale. The cache holds two entries (one per
  block). Output is pixel-identical: six fixed-input frames rendered from the pre-change
  tree and byte-compared against the current one, all identical.
- The process sets a 110 MB memory limit and a 200 % GC trigger (the runtime default is
  100 %), keeping the heap peak under the 120 MB budget with fewer collections per second.
- The sun's disc is gated by cloud amount per weather class. `sunDiscVisibility(class, cloudPct)`
  is the single place that decides it: 1.0 for clear / partly-cloudy / cloudy at cloud <= 50,
  linear 1.0 -> 0.0 across 50 -> 75, 0.0 above; 0.0 for overcast, fog and dust at any cloud
  amount; 1.0 for rain / snow / thunder at cloud <= 50 (the sun shower) and 0.0 above. The disc
  body, the small core, the near field term, the ray texture and the bloom scale by it; the
  sky-wide lift and the wide glow texture do not, so a cloudy sky keeps its grey wide light.
  The near field term is back at `A_n = 200`, `L_n = 0.18 x bake width` (was 150 / 0.55w): the
  middle core shrinks while the 0.9w lift and the 1.6w glow texture keep the overall spread.
  Measured on the 540x960 bake: middle-core half-width 96 px = 0.178w, net lift at 0.45w = 69.3
  of the 192.0 peak (36.1 %), disc pixels 1861 at cloud 40 and 0 at cloud 90 (rain).

### Removed

- The rain-alert layout variants that never shipped: seven drawing functions
  (`drawStrip`, `drawCard`, `drawLeftBar`, `drawWatermark`, `drawFusion`, `drawFusion2`,
  `drawFusion3`), the `switch a.Style` block and the `Alert.Style` field. The shipped
  `panelrow` behaviour is unchanged, pixel for pixel.

### Fixed

- The settings panel now lists its rows vertically. The list widget's zero-value main axis
  is horizontal, so all rows were laid out in one row and clipped away: the window showed
  nothing but its title.
- The settings window opened from `WeatherScr.scr /c` is restored to a plain dialog with
  a title bar, a resize frame and a taskbar button, sized and centred on the primary
  monitor's work area; the `.scr` launch path used to hand it a full-screen, borderless
  frame.
- The installer wizard window is sized to the primary monitor's work area, so its buttons
  are reachable without resizing the window by hand.
- Uninstall now stops the running screensaver process before deleting
  `C:\Windows\System32\WeatherScr.scr`, then polls until the file is deletable
  (200 ms x 50 = 10 s) before requesting elevation. Deleting the file while the
  screensaver was running used to fail with `Access is denied` (the running image
  locks the file; the lock also lingers briefly after the process is terminated).
  Measured on the target machine with the screensaver running
  (`已停止 1 个运行中的屏保进程` -> `已删除 C:\Windows\System32\WeatherScr.scr` ->
  `已恢复 SCRNSAVE.EXE = "C:\Windows\System32\SRPSS.scr"`, uninstall page log).
- Uninstall no longer reports a failure when the Add/Remove Programs key does not
  exist: `RegDeleteKey` returning `ERROR_FILE_NOT_FOUND` is now treated as success
  (`已删除 Add/Remove Programs 项`), matching the idempotent intent of the step.

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

[Unreleased]: https://github.com/DragonTTDream/WeatherScr/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/DragonTTDream/WeatherScr/releases/tag/v1.0.0
