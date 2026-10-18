# Configuration reference

Config file: `C:\Program Files\WeatherScr\config.json`
(falls back to the directory next to `WeatherScr.scr` when that is not writable).
Every key below is editable from the settings panel (`WeatherScr.scr /c`).
Unknown keys are ignored; missing keys fall back to the default shown here.

## Location

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `location` | string | `"Hangzhou, China"` | Place name to geocode. Used when no coordinates are set. |
| `location_lat` | float | `0` | Latitude. With `location_lon` both non-zero, they are used directly and no geocoding request is made. |
| `location_lon` | float | `0` | Longitude. |
| `location_name_zh` | string | `""` | Cached Chinese display name, filled by the place search. |
| `location_name_en` | string | `""` | Cached English display name, filled by the place search. |
| `display_language` | string | `"en"` | `en` shows the English name and English labels; `zh` shows Chinese. Coordinates are stored, not a name, so a search result survives a language change. |

Coordinates are the storage format. A name is only a label.

## Weather

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `units` | string | `"c"` | `c` metric, `i` imperial. |
| `refresh_minutes` | int | `1` | Refresh interval, floored at 1. Geocoding is cached, so a refresh is one request of about 1.1 kB. |
| `fetch_timeout_seconds` | int | `5` | HTTP timeout for the forecast request. |
| `weather_base_url` | string | `""` | Optional proxy base for the forecast API. Empty means `https://api.open-meteo.com/v1/forecast`. |

When the forecast request fails, the newest cached sample is used, selected
deterministically by cache timestamp. A stale sample keeps the previous sky; it does
not flip day and night.

## Visual classes

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `effect.enabled` | string array | all seven | Which classes may be selected: `clear`, `cloudy`, `fog`, `rain`, `snow`, `thunder`, `dust`. A class not listed falls back to `clear`. |
| `font_family` | string | built-in | Text family. |

## Panel

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `show_background` | bool | `false` | Panel background. `false` = fully transparent, no border. |
| `bg_color` | int[4] | `[35,35,35,255]` | Kept for settings-shape parity; not drawn while `show_background` is false. |
| `bg_opacity` | float | `0.0` | Fully transparent. |
| `clock_bg_opacity` | float | `0.0` | Fully transparent. |
| `border_color` | int[4] | `[255,255,255,255]` | Kept for parity; not drawn while `border_opacity` is 0. |
| `border_opacity` | float | `0.0` | No border. |
| `color` | int[4] | `[255,255,255,230]` | Text colour. |
| `font_size` | int | `25` | Weather panel text size. The clock uses its own fixed scale. |
| `icon_size` | int | `96` | Detail-row icon size. |
| `margin` | int | `30` | Panel margin. |
| `show_condition_icon` | bool | `true` | **Currently inert for the 192 px condition icon.** The renderer gates that icon on the package constant `ui.WeatherShowIcon`, which ships `false` (the icon was dropped from the layout; the implementation and assets are kept). The key is read, stored and round-tripped through the panel for upstream settings-shape parity. |
| `show_details_row` | bool | `true` | Second row: wind, humidity, precipitation probability, and so on. |
| `show_forecast` | bool | `true` | Forecast line. |
| `show_five_day_forecast` | bool | `false` | Five-day line. |
| `clock_y_offset` | int | `0` | Vertical nudge of the clock block, pixels. |
| `weather_y_offset` | int | `0` | Vertical nudge of the weather block, pixels. |
| `interaction_mode` | bool | `false` | Mouse/keyboard interaction. |

## Rain-risk alert

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `alert_enabled` | bool | `true` | Show the alert. |
| `alert_threshold_pct` | int | `30` | Minimum precipitation probability that triggers it. |
| `alert_window_hours` | int | `3` | Rolling window the probability is read from. Legal values `3` and `6`; anything else falls back to `3`. |
| `alert_y_offset` | int | `-60` | Vertical placement, pixels. |
| `alert_pulse` | bool | `true` | 1.2 s breathing on the alert. |

Tiers (Chinese national rainstorm warning thresholds):

| Probability | Tier | Colour |
| --- | --- | --- |
| ≥ 30 % | Blue | `rgb(30, 111, 217)` |
| ≥ 50 % | Yellow | `rgb(242, 187, 5)` |
| ≥ 70 % | Orange | `rgb(240, 120, 24)` |
| ≥ 90 % | Red | `rgb(217, 38, 38)` |

The probability is read over the rolling window, not as the daily maximum, so the
alert tracks the near-term forecast rather than the worst point of the day.

## Wallpaper keys

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `wallpaper_dir` | string | `C:\Users\Public\Pictures` | **Read and preserved, not rendered.** Kept for settings-shape parity. |
| `wallpaper_interval_s` | int | `40` | Same. |

## Files written at runtime

| File | Location | Contents |
| --- | --- | --- |
| `config.json` | `C:\Program Files\WeatherScr\` | Your settings, including your location. |
| `weather-cache.json` | `C:\Program Files\WeatherScr\` | Last fetched samples, keyed by location. |
| `scr.log` | next to `WeatherScr.scr` | Frame, class, fetch and budget log lines. |

`config.json` and `weather-cache.json` are the only files that contain your location.
Redact them before posting a bug report.
