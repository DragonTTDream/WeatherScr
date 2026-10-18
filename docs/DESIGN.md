# Design notes

Engineering notes for the parts of this project where the obvious implementation was
measurably wrong. Each entry states the mechanism, not the story.

## Layer order is fixed, and it is load-bearing

`ui/render.go`, `Screen.Layout`:

```
skyBlit → moonBlit → PrevField.Draw → Field.Draw → clockBlock → weatherBlock → alert
```

Consequences:

- Clouds (bands and particles) are drawn **after** the moon layer and **before** the
  text blocks. Clouds can cover the sun and the moon; they can never cover the clock,
  the weather panel or the alert. That is structural, not a tuning parameter.
- The moon cannot live inside the sky texture. Anything baked into the sky texture is
  behind every cloud and can never be occluded. That is why the moon is a separate
  layer (`moonLayerGet` / `moonBlit`).
- Stars are baked into the sky texture (`bakeStars`, 150 deterministic stars, seeded),
  for every night class, at zero per-frame cost. Being in the sky layer is what makes
  clouds pass in front of them.

## Supersampling and the moon's resolution

The sky texture is baked at half resolution (`skyCacheScale = 2`) and blitted 1:1.
Anything baked at that scale is magnified 2×, so a 32 px moon radius lands on the
screen as an integer staircase — measured as 7 px row jumps. The moon layer is built
at **full** resolution for exactly this reason.

## Blitting to an absolute offset in gioui

`clip.Rect` restricts the paint region; it does **not** move the image.
`paint.ImageOp.Add(ops)` draws at the current transform origin. A 1:1 blit to an
absolute offset therefore needs an explicit translation:

```go
cl := clip.Rect(image.Rect(off.X, off.Y, off.X+size.X, off.Y+size.Y)).Push(gtx.Ops)
tr := op.Affine(f32.NewAffine2D(1, 0, float32(off.X), 0, 1, float32(off.Y))).Push(gtx.Ops)
e.ops.Add(gtx.Ops)
paint.PaintOp{}.Add(gtx.Ops)
tr.Pop(); cl.Pop()
```

With an identity transform the image lands at (0,0), falls outside the clip rect, and
**nothing is drawn, silently**. No error, no warning.

## Premultiplied PNGs

Several source PNGs are premultiplied (`glow.png` RGB/α ratio 1.31, `fog1.png` 1.35,
`cloud1.png` 1.854). Go's `image/png` decodes them as NRGBA and treats the stored RGB
as straight, which darkens every semi-transparent pixel. `ui/assets.go` un-premultiplies
on load (`readPNG` → `unpremultiply(toNRGBA(im))`).

## Moon phase geometry

Validated across all seven phases. With phase `φ ∈ [0,1)`, `θ = 2πφ`:

```
cosT = cos θ          k = (1 - cosT) / 2        a = |cosT|
waxing = φ < 0.5      sX = ±X (negated when waning)
half = smoothstep(-halfFeather, halfFeather, sX)
e    = (sX²/a²) + Y²
inE  = 1 - smoothstep(1-termFeather, 1+termFeather, e)

cosT > 0  →  lit = half * (1 - inE)        crescent
cosT < 0  →  lit = half + (1-half) * inE   gibbous
a < 1e-6  →  lit = half
k < 0.02  →  draw nothing                  new moon
```

Two traps recorded the hard way:

- `smoothstep(a, b, x)` with `b <= a` is a **hard step**, not a ramp. A descending ramp
  must be written `1.0 - smoothstep(lo, hi, x)`. Writing the arguments in descending
  order silently produced a binary mask and rendered every phase as a half moon.
- The upstream limb term is `smoothstep(0.015, 0.10, facing)` with
  `facing = √(1-(r/R)²)`, i.e. the lit→dark transition band is 0.49 % of the radius.
  A feather of 0.10 is 20× wider, which is what reads as mush.

Only the lit shape is drawn. No outline, no dashed terminator.

## Glow is a bloom of the lit region

The halo is not a ring around the disc. The lit region is discretised into a 16×16
source grid with weight `litAt × limb × (1 - 0.12·rr)`; every pixel sums
`Σ w · exp(-decay · dist/rc)` over those sources within `haloSpread`, normalised so the
full-moon rim sums to 1, scaled by `0.35 + 0.65k`.

Because the glow is emitted by the lit shape and drawn **inside** the disc as well as
outside it, the dark side is transparent and filled by the halo. A uniform radial ring
centred on the disc instead produces a glowing pit on the dark side.

## Cloud bands and cloud particles

Two mechanisms, both needed:

- **Bands** (`drawCloudBands`) are the large clouds. Four of them, geometry in
  `bandPlace`.
- **Particles** (`pkCloud`) are the smaller drifting clouds.

### Band alpha must be index-descending

Composite arithmetic on the disc: covering the disc with a cloud over a dark sky
changes the pixel by `(246 - 90) · (1 - α) = 156 · (1 - α)` levels. At α = 0.588 that
is 64 levels — the moon reads straight through. The ramp is therefore
`(240 - 25·frac)/255` = 0.941 / 0.908 / 0.876 / 0.843, and only bands 0 and 1 intersect
the disc (bands span y 38–229 and 192–413; the disc is y 117–247).

### Band evenness is an identity, not a tuning

All bands share one span `bandSpan(w) = w + 2·avgDW` and one speed, and each band's
position is

```
x = mod(speed + span·i/N, span) - dw
```

The phase is recorded at the band's **left edge**, not its centre. With a centre-based
phase, at the start of the cycle the band spans `[-dw/2, +dw/2]`, i.e. half of it is
already on screen — a cloud pops into existence at the left edge. With the left-edge
phase the band is fully off-screen at `mod = 0` and fully off-screen right as
`mod → span`.

`avgDW` is the **average** half-width, not the maximum: using the maximum lengthens the
off-screen stretch of the narrow bands and drops mean coverage from 11.4 % to 10.3 %.
The average restores it to 11.9 %. The cost is that band-to-band parallax speed
difference is gone.

### Particle spacing controller

Per-particle speed is `vxBase` plus a spacing term:

```
vx_i = vxBase_i + cloudCoupleK · (gapAhead_i - gapBehind_i) / target
target = width / m
```

clamped to `[0.004, 0.030]`. This is traffic-flow smoothing: a cloud with a gap ahead
speeds up, one that is closing on its neighbour slows down. Entry positions are **not**
moved — the mandate is that nothing appears on screen.

The original spread `vx = 0.006 + 0.022·rand` is a 4.7× speed spread, so fast clouds
overtake slow ones, which produces clustering and emptiness at the same time.

### Instance jitter must not defeat the occlusion floor

`Draw` applies `alpha *= brightnessMul ∈ [0.65,1.35]` then `alpha *= alphaMul ∈
[0.50,1.00]`, so an α of 1.0 becomes 0.33–1.0. The occlusion floor is applied after
that chain, for cloud particles only, and only when the class is not `clear`.

## Class changes crossfade

A class change used to rebuild the field, and a fresh spawn spreads clouds across the
whole screen — so the new weather appeared instantly, in front of you. Now:

- `NewFieldEntering` spawns with a staggered off-screen entry `x = -0.35 - 0.10·idx`;
- the outgoing field is kept as `Frame.PrevField` and fades out over 9 s while the
  incoming field fades in.

Two bugs in this path are worth naming, because both were silent:

- The `entering` flag must be set **before** the spawn loop. Setting it after calling
  the constructor means the staggered branch never runs and the particle list is
  byte-identical to the old one.
- `clampAirY` delegates to `spreadAir`, which is free to rewrite `x`. The staggered
  `x` must be protected by passing `allowX = false`. **`spreadAir` is the true decider
  of a cloud's x and y** — any change to cloud placement that is not visible in the
  output must be traced there, not to the spawn `switch`.

## Day/night must be deterministic

`lastGood` used to return the first entry from a Go map. Go map iteration order is
random, the cache held three entries with conflicting `is_day`, and every failed
request picked one at random — the sky flipped between day and night on the same
afternoon. Selection is now newest-by-`CachedAt`.

The API was not at fault: the same request repeated returned the same `is_day`.

## Adaptive frame rate must not delete the weather

`adaptFps` reduces the active particle count below 12 fps. Its floor
`minActive = (n+3)/4` cut 8 clouds to 2 on the target host, which produced long
stretches of empty sky. For the `cloudy` class the floor is the full count.

## Fill rate is the CPU cost

One full-screen fill at 15 fps costs 4.4 % of one core. The frame is: sky blit 1.0,
cloud bands 3.7, text 2.0 → 6.7 screen-equivalents. That is why CPU tracks fill rate
and why cutting frame rate without cutting drawn area buys nothing.

## Measurement discipline

- Real-machine screen capture of a gioui fullscreen window does not work:
  `CopyFromScreen` captures the desktop, `PrintWindow` returns all black. Pixel
  measurements taken that way are void.
- Luminance thresholds cannot separate a bright cloud from a bright moon (cloud body
  median 216, max 239; moon core 215–237). The discriminator is colour temperature:
  the moon is warm white (R > B), the cloud body is cool grey-blue (B > R).
- A dynamic property must never be judged from frames that do not provably contain the
  event. Measure the timeline first (`CelestialCover`, `CelestialCoverAll`,
  `CoverageProfile`), then render the frames the measurement names. `cmd/fxshot` prints
  the covered percentage per frame for exactly this purpose.
- `copy /Y` preserves the **source** modification time, so a directory timestamp is
  not proof that a deploy happened. Only a hash comparison is.
