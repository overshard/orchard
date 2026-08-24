#!/usr/bin/env python3
"""Generate the responsive size ladder for the acrylic pours.

Go has no next/image, so the widths a browser can choose between have to exist
as real files. This makes them.

Run it when a pour is added or replaced:

    make images SRC=/path/to/originals

The originals are 3900x3000 archival scans at up to 16MB each. They are
deliberately NOT in this repo: a website repository is the wrong home for
archival masters, and serving one unresized put 87MB on the art page once
already. Point SRC at wherever they live.

Widths come from where the images actually appear on the page:

    480   cards at 1x (they render about 389px wide in the two-column grid)
    960   cards at 2x, and the menu panel
    1600  the lightbox (90vw/90vh, object-fit: contain) and the menu panel at 2x
    2400  the hero only, which is the one full-bleed 100vw image

Keep this in step with the srcset lists in handlers.go. They are the two halves
of the same decision, and nothing checks that they agree.
"""
import pathlib
import sys

try:
    from PIL import Image
except ImportError:
    sys.exit("Pillow is required: uv pip install pillow")

HERO = "005"
CARD_AND_LIGHTBOX = [480, 960, 1600]
HERO_EXTRA = [2400]
QUALITY = {480: 80, 960: 80, 1600: 82, 2400: 82}

DST = pathlib.Path(__file__).resolve().parent.parent / "public/images/art/acrylic-pours"


def main(src_dir):
    src = pathlib.Path(src_dir)
    if not src.is_dir():
        sys.exit(f"no such directory: {src}")

    originals = sorted(src.glob("*.webp"))
    if not originals:
        sys.exit(f"no .webp originals found in {src}")

    DST.mkdir(parents=True, exist_ok=True)
    for stale in DST.glob("*.webp"):
        stale.unlink()

    total = 0
    for path in originals:
        im = Image.open(path).convert("RGB")
        widths = CARD_AND_LIGHTBOX + (HERO_EXTRA if path.stem == HERO else [])
        line = f"{path.name:10}"
        for w in widths:
            out = im if im.width <= w else im.resize(
                (w, round(im.height * w / im.width)), Image.Resampling.LANCZOS)
            dest = DST / f"{path.stem}-{w}.webp"
            out.save(dest, "WEBP", quality=QUALITY[w], method=6)
            size = dest.stat().st_size
            total += size
            line += f"  {w}w {size / 1000:6.0f}kB"
        print(line)

    print(f"\n{len(originals)} originals -> {total / 1e6:.1f}MB in {DST}")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    main(sys.argv[1])
