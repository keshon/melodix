# Brand

There is one mark and two colours. That is the whole system, and it should
stay that small — this is a music bot, not a company.

## The mark

A speaker driver seen head on: lit rim, basket, cone, dome. Four concentric
circles on a 24-unit grid, drawn from the Discord avatar the project has
always used. The avatar is a rendered image and cannot survive being shrunk
to a tab icon; the mark is what is left when you keep only the structure that
still reads at 20 pixels.

`assets/brand/logo.svg` is the source of truth. It carries no colours of its
own: inlined in a page it inherits `currentColor` and `--accent`, so it
follows light and dark mode with no second file. Standalone it falls back to
the Melodix palette.

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

Two tones, both already defined as CSS custom properties in `index.html`:

| role   | light     | dark      | token      |
| ------ | --------- | --------- | ---------- |
| mark   | `#241a21` | `#f2e9ee` | `--text`   |
| rim    | `#b01e66` | `#e13a86` | `--accent` |
| ground | `#fbf6f9` | `#171118` | `--ink`    |

The accent carries the rim and nothing else. If a third colour ever seems
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
