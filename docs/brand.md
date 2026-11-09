# Brand

The mark uses a foreground colour and an accent for the rim. Keep its small-size
constraints when changing or exporting assets.

## The mark

A speaker driver seen head on: lit rim, basket, cone, dome. Four concentric
circles on a 24-unit grid, drawn from the Discord avatar the project has
always used. The avatar is a rendered image and cannot survive being shrunk
to a tab icon; the mark is what is left when you keep only the structure that
still reads at 20 pixels.

`assets/brand/logo.svg` is the source of truth. It uses `currentColor` for the
driver and `--accent` for the rim, with a built-in fallback palette. A page can
override those values when inlining the mark. The source includes light and
dark foreground treatments.

`assets/brand/logo-specimen.svg` shows it at real sizes against both
backgrounds. Look at that before changing any number in it — the strokes are
tuned so the bands stay separate at small sizes, and thickening one closes
the gap either side of it.

The basket ring (`r=8.1`, half opacity) is the fragile one. It is what makes
the mark read as a driver rather than a bullseye, and it is also the first
thing to disappear when the icon gets small. Below about 16 pixels it is
gone whatever you do; do not compensate by thickening it, because at 48
pixels and up that reads as a mistake.

## Colours

The standalone mark uses these fallback colours. They are distinct from the
landing page palette; an inline mark can inherit the page's `color` and `--accent`.

| role   | light     | dark      | token      |
| ------ | --------- | --------- | ---------- |
| mark   | `#241a21` | `#f2e9ee` | `--text`   |
| rim    | `#b01e66` | `#e13a86` | `--accent` |
| ground | `#fbf6f9` | `#171118` | `--ink`    |

The mark uses its accent for the rim. The landing page defines its own palette
in `index.html`: dark `--ink: #0b0a0c`, `--text: #f4eff2`, `--accent: #ff2d7e`;
light `--ink: #f4f1ea`, `--text: #16120f`, `--accent: #c00040`.

The mark's accent carries the rim and nothing else. If a third colour ever seems
necessary, the answer is almost always that the thing being coloured should
not be in the mark.

## Favicons

`assets/favicon/` is generated from `assets/brand/favicon-source.svg`, which
is a separate file on purpose: browser chrome cannot resolve `currentColor`,
and a tab strip may be any shade, so the favicon carries its own colours and
sits on the dark disc the avatar always had.

Three treatments, and they are not interchangeable:

- **Rounded, transparent corners** — `favicon.ico` (16/32/48) and
  `favicon-96x96.png`. Shown as-is in tabs and bookmarks.
- **Full bleed** — `apple-touch-icon.png`. iOS applies its own rounded mask,
  so shipping our own corners would round it twice.
- **Full bleed, mark at 82%** — the two `web-app-manifest-*` files. The
  manifest declares `purpose: "maskable"`, which lets Android crop to any
  shape it likes; anything outside the central 80% can be cut. At full size
  the rim would lose its edge.
