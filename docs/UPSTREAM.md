# Upstream reference

## The project referenced

> **ShittyRandomPhotoScreenSaver (SRPSS)**
> by Basjohn — https://github.com/Basjohn/ShittyRandomPhotoScreenSaver
> Copyright (c) 2026 Jayde Ver Elst / Basjohn
> MIT License — https://github.com/Basjohn/ShittyRandomPhotoScreenSaver/blob/main/LICENSE

WeatherScr is **not** a fork, not a port and not a derivative build of SRPSS. It is an
independent implementation in Go of a subset of its behaviour, written to run on a
low-power x86 mini host.

## What was referenced

| Referenced | How |
| --- | --- |
| Panel layout | The clock block and the current-conditions block, their relative placement and content structure. |
| Panel transparency semantics | The upstream `show_background` switch is the mechanism used for the transparent panel; no key was invented that upstream does not have. |
| Settings keys | Key names and shapes were mirrored so an existing SRPSS settings file reads sensibly: `location`, `units`, `refresh_minutes`, `bg_color`, `bg_opacity`, `border_color`, `border_opacity`, `show_background`, `color`, `font_size`, `icon_size`, `margin`, `show_condition_icon`, `show_details_row`, `show_forecast`, `show_five_day_forecast`, `interaction_mode`, `wallpaper_dir`, `wallpaper_interval_s`. |
| Behaviour | Weather-driven visuals, day/night handling, the screensaver registration model (`.scr` in `System32`, `SCRNSAVE.EXE`). |

## What was not taken

- **No source code.** SRPSS is Python 3 + PySide6 (Qt Quick), built with Nuitka into a
  `.scr`. WeatherScr is Go + gioui, no cgo, Direct3D 11. There is no shared code, no
  shared build step, no shared asset.
- **No upstream artwork is shipped.** Cloud and fog sprites come from Kenney (CC0),
  documented in `assets/LICENSE-kenney.txt`; the three detail-row icons are recorded in
  `ui/embed/ATTRIBUTION.md`. Upstream's condition-icon set and its moon-phase artwork
  were consulted during asset development and are **not used**: the condition icon was
  dropped from the layout and the moon is drawn by this project, so neither set is in
  this repository and no code path loads it.
- **No upstream files in this repository.** The upstream repository is not vendored, not
  mirrored, not included as a submodule.

## Upstream line references in comments

Comments cite upstream source lines, for example `default_settings.py:1523-1540`.
These record **which upstream default a value mirrors** — provenance for a reader who
wants to compare behaviour. They are citations of a public repository, not copied code.

## Where the two diverge, deliberately

| | SRPSS | WeatherScr |
| --- | --- | --- |
| Scope | Wallpaper slideshow, multi-monitor, widget dashboard, media control, mail and feed cards, RSS, audio visualizer, 25 transitions, 8 visualizer modes | Clock, current-conditions panel, rain-risk alert, weather-driven sky |
| Runtime | Python 3 + PySide6, Nuitka-compiled | Go, no cgo |
| Install footprint | 156 MB `.scr` | 12.4 MB `.scr` |
| Panel background | `bg_opacity 0.3`, border `1.0` | fully transparent, no border |
| Condition icon | 96 px condition icon | dropped from the layout; the icon's assets and drawing path were removed, `show_condition_icon` is still accepted as a key |
| Weather source | its own providers | Open-Meteo, keyless |

## License position

The upstream MIT license requires its copyright and permission notices to be preserved
in copies of **that** software. This repository contains none of that software, so the
obligation is not triggered. The attribution in `NOTICE` and in the README's **Thanks**
section is given as credit, which is the convention this project follows and the reason
this file exists.

WeatherScr's own code is MIT, Copyright (c) 2026 DragonTTDream.
