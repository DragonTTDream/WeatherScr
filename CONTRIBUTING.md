# Contributing

Thanks for taking a look. Small rules that keep this project checkable.

## Ground rules

1. **`make gate` must be green before you push.** It runs build, vet, tests and the
   Windows cross-compile in one shot. That is the project's single external check;
   a claim that something works is only accepted with its exit code 0.
2. **Do not commit binaries.** `*.exe`, `*.scr` (except the tracked placeholder, see
   below), `*.log`, `*.png` previews. `make payload` writes a real 12 MB `.scr` into
   `cmd/setup/payload/` — that file is tracked as a placeholder, so git will show it
   as modified after you run `make payload`. **Do not commit that change.**
3. **`cmd/setup/payload/WeatherScr.scr` is a placeholder in git.** It exists so
   `//go:embed payload/WeatherScr.scr` compiles in a fresh clone. `make payload`
   replaces it with the real screensaver.
4. **`cmd/scr` is Windows-only.** gioui needs a window system on Linux. The library
   packages build and test on Linux with `-tags=novulkan`; `cmd/scr` and `cmd/setup`
   are checked with `GOOS=windows go vet ./cmd/...`.
5. **Comments carry the reasoning.** When you change a coefficient, an alpha ramp, a
   placement range, say *why* in the comment and give the number you measured. A
   comment that only restates the code is noise.
6. **No personal data in the tree.** No home-directory paths, no host names, no
   machine serials, no private endpoints, no real coordinates of a real person.
   Example location is `Hangzhou, China`. The gate greps for these patterns and fails
   the build when it finds them.

## Layout

```
cmd/scr        the screensaver (Windows-only)
cmd/setup      installer
cmd/fxshot     headless renderer, iterates visuals with no display
cmd/wfetch     forecast API probe
cmd/winlist    window-state probe
ui/            layout, sky bake, layers, particles, cloud bands, alert, settings panel
config/        config file, defaults, validation
weather/       Open-Meteo client, schema, cache, forecast window
effect/        WMO code -> visual class
geo/           geocoding and place search
assets/        Kenney sprites (CC0) + LICENSE-kenney.txt
icons/         icon master and .ico
docs/          design, config reference, upstream notes
```

## Visual work

Do not iterate by staring at a screen. Render headless and measure:

```bash
make fxshot
FX_OUT=/tmp/shot FX_MOON=0.174 FX_SHOTS=111,185,296 ./fxshot cloudy
```

Each printed frame reports how many clouds are on the sun/moon disc and the covered
percentage. Pick frames to judge only from those numbers. `ui.Field` exposes
`CelestialCover`, `CelestialCoverAll` and `CoverageProfile` for exactly this.

## Tests

Tests are in the packages they cover (`ui/*_test.go`, `weather/*_test.go`, ...).
They are geometry and behaviour probes, not decoration: they assert measured
quantities, and they print the numbers they measured. If you change a quantity,
update the assertion in the same commit and say in the message what moved.

## Reporting a problem

Give: OS build, screen resolution and orientation, GPU, `WeatherScr.scr` SHA256, the
`config.json` keys you changed (with the location redacted), and the log lines from
`scr.log` next to the `.scr`.
