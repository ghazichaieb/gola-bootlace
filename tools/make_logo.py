"""Draws the Gola Bootlace logo files from one description of the drawing.

    python tools/make_logo.py

The mark is a boot's criss-cross lacing in Gola red, threaded through six eyelets on a dark tile (a lace is also a link between machines).
The wordmark is set in Geist Bold (SIL Open Font Licence 1.1, see NOTICE) and turned into outlines, so the SVG files look the same everywhere.

Writes into branding/:
  bootlace-mark.svg            the square mark
  bootlace-logo.svg            mark and wordmark, for light backgrounds
  bootlace-logo-dark.svg       mark and wordmark, for dark backgrounds
  bootlace-mark-NNN.png        the mark at several sizes
  bootlace-logo.png / -dark.png  the full logo (transparent background)
  bootlace.ico                 the mark as a Windows icon / favicon

PNG files are drawn with Microsoft Edge in headless mode (part of Windows); nothing else has to be installed except `pip install fonttools brotli pillow`.
The font file is not stored in this repository: pass --font <path to Geist-Variable.woff2>."""
from __future__ import annotations

import argparse
import math
import re
import subprocess
import tempfile
from pathlib import Path

from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen
from fontTools.ttLib import TTFont
from fontTools.varLib import instancer
from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'branding'
SOURCE = OUT / 'source' / 'GolaLogo.svg'          # the Gola logo as Gola Services supplied it: the GOLA lettering is taken from it untouched
EDGE = Path(r'C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe')

RED, OUTLINE = '#bf2126', '#231f20'          # the Gola red and its dark outline
TILE, TILE_EDGE = '#1f2124', '#33373c'
METAL, HOLE = '#e8eaed', '#14161a'
INK = '#1f2124'                              # text on light backgrounds

# ---- the mark, on a 256 x 256 tile --------------------------------------------------------------------------------------------------
LEFT, RIGHT, ROWS = 88, 168, (86, 128, 170)
Z1 = [(62, 46), (LEFT, ROWS[0]), (RIGHT, ROWS[1]), (LEFT, ROWS[2]), (62, 212)]       # one lace: zig-zags down the boot
Z2 = [(194, 46), (RIGHT, ROWS[0]), (LEFT, ROWS[1]), (RIGHT, ROWS[2]), (194, 212)]    # the other lace, the mirror image
CROSS_1 = (128, 107)                         # where Z1 passes over Z2 (everywhere else Z2 is on top)


def points(pts) -> str:
    return ' '.join(f'{x:g},{y:g}' for x, y in pts)


def strand(pts, clip: str = '') -> str:
    attrs = f' clip-path="url(#{clip})"' if clip else ''
    return (f'<g{attrs} fill="none" stroke-linecap="round" stroke-linejoin="round">'
            f'<polyline points="{points(pts)}" stroke="{OUTLINE}" stroke-width="21"/>'
            f'<polyline points="{points(pts)}" stroke="{RED}" stroke-width="14"/></g>')


def tip(end, toward, length: float = 17) -> str:
    """The metal tip of a lace end: the last `length` units of the strand, pale."""
    dx, dy = toward[0] - end[0], toward[1] - end[1]
    n = math.hypot(dx, dy)
    ex, ey = end[0] + dx / n * length, end[1] + dy / n * length
    return (f'<g fill="none"><line x1="{end[0]:g}" y1="{end[1]:g}" x2="{ex:.1f}" y2="{ey:.1f}" stroke="{OUTLINE}" stroke-width="21" stroke-linecap="round"/>'
            f'<line x1="{end[0]:g}" y1="{end[1]:g}" x2="{ex:.1f}" y2="{ey:.1f}" stroke="{METAL}" stroke-width="13" stroke-linecap="butt"/></g>')


def mark_body() -> str:
    # the rings go over the lace, so the lace is seen passing through them
    eyelets = ''.join(f'<circle cx="{x}" cy="{y}" r="17" fill="none" stroke="{METAL}" stroke-width="5"/>' for x in (LEFT, RIGHT) for y in ROWS)
    tips = tip(Z1[0], Z1[1]) + tip(Z1[-1], Z1[-2]) + tip(Z2[0], Z2[1]) + tip(Z2[-1], Z2[-2])
    clip = f'<clipPath id="cross1"><circle cx="{CROSS_1[0]}" cy="{CROSS_1[1]}" r="21"/></clipPath>'
    return f'<defs>{clip}</defs>{strand(Z1)}{strand(Z2)}{strand(Z1, "cross1")}{eyelets}{tips}'


def mark_svg(size: int | None = None) -> str:
    dim = f' width="{size}" height="{size}"' if size else ''
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256"{dim}>'
            f'<rect x="4" y="4" width="248" height="248" rx="58" fill="{TILE}" stroke="{TILE_EDGE}" stroke-width="4"/>{mark_body()}</svg>')


# ---- the wordmark -------------------------------------------------------------------------------------------------------------------
class Font:
    def __init__(self, path: Path, weight: int) -> None:
        font = TTFont(path)
        self.font = instancer.instantiateVariableFont(font, {'wght': weight})
        self.glyphs, self.cmap, self.upem = self.font.getGlyphSet(), self.font.getBestCmap(), self.font['head'].unitsPerEm

    def text(self, text: str, size: float, x: float, baseline: float, tracking: float = 0) -> tuple[str, float]:
        """The outlines of `text` as one SVG path, and its width."""
        scale, cursor, parts = size / self.upem, 0.0, []
        for ch in text:
            name = self.cmap[ord(ch)]
            pen = SVGPathPen(self.glyphs)
            self.glyphs[name].draw(TransformPen(pen, (scale, 0, 0, -scale, x + cursor, baseline)))
            parts.append(pen.getCommands())
            cursor += self.font['hmtx'][name][0] * scale + tracking * size
        return ' '.join(p for p in parts if p), cursor - tracking * size


def gola_lettering(height: float) -> tuple[str, float]:
    """The letters G O L A from the Gola logo file, as one group scaled to `height`, with its width. Their own red fill and dark outline are kept."""
    svg = SOURCE.read_text(encoding='utf8')
    letters = [m.group(0) for key in 'GOLA' for m in [re.search(rf'<path id="{key}" [^>]*/>', svg)] if m]
    if len(letters) != 4:
        raise SystemExit(f'the letters G O L A were not found in {SOURCE}')
    top, bottom, right = 760.0, 859.0, 262.0                 # the letters' extent in the logo file (outline included)
    scale = height / (bottom - top)
    return f'<g transform="scale({scale:.4f}) translate(0 {-top:g})">{"".join(letters)}</g>', right * scale


def logo_svg(font: Font, light: Font, dark: bool) -> str:
    """Mark on the left, the stacked wordmark on the right."""
    ink = '#f2f3f5' if dark else INK
    mark = 224                                               # the mark's height in the logo
    gola_art, w1 = gola_lettering(60)
    word, w2 = font.text('Bootlace', 112, 0, 0, tracking=-0.01)
    tag, w3 = light.text('network boot for ISO files', 31, 0, 0, tracking=0.01)
    x0 = mark + 36
    height, width = mark + 16, x0 + max(w1, w2, w3) + 16
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {width:.0f} {height:.0f}" width="{width:.0f}" height="{height:.0f}">'
            f'<g transform="translate(8 8) scale({mark / 256:.5f})"><rect x="4" y="4" width="248" height="248" rx="58" fill="{TILE}" stroke="{TILE_EDGE}" stroke-width="4"/>'
            f'{mark_body()}</g>'
            f'<g transform="translate({x0:.0f} 0)">'
            f'<g transform="translate(2 4)">{gola_art}</g>'
            f'<path transform="translate(0 168)" d="{word}" fill="{ink}"/>'
            f'<path transform="translate(3 218)" d="{tag}" fill="{"#9aa0a8" if dark else "#5f6670"}"/></g></svg>')


# ---- files --------------------------------------------------------------------------------------------------------------------------
def render(svg: str, width: int, height: int, background: str = 'transparent') -> Image.Image:
    with tempfile.TemporaryDirectory() as tmp:
        work = Path(tmp)
        page, out = work / 'page.html', work / 'out.png'
        page.write_text(f'<html><body style="margin:0;background:{background};overflow:hidden">'
                        f'<div style="width:{width}px;height:{height}px">{svg}</div></body></html>', encoding='utf8')
        subprocess.run([str(EDGE), '--headless=new', '--disable-gpu', '--hide-scrollbars', '--default-background-color=00000000',
                        f'--window-size={width},{height}', f'--screenshot={out}', page.as_uri()], check=True, capture_output=True, timeout=60)
        return Image.open(out).convert('RGBA')


def sized(svg: str, width: int, height: int) -> str:
    """The SVG with its width and height replaced, so a browser draws it at that size."""
    import re
    return re.sub(r'(<svg[^>]*?) width="\d+" height="\d+"', r'\1', svg, count=1).replace('<svg ', f'<svg width="{width}" height="{height}" ', 1)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument('--font', required=True, help='Geist-Variable.woff2 (SIL OFL)')
    args = parser.parse_args()
    OUT.mkdir(exist_ok=True)
    font, light = Font(Path(args.font), 700), Font(Path(args.font), 500)
    mark = mark_svg()
    logo, dark = logo_svg(font, light, False), logo_svg(font, light, True)
    (OUT / 'bootlace-mark.svg').write_text(mark, encoding='utf8')
    (OUT / 'bootlace-logo.svg').write_text(logo, encoding='utf8')
    (OUT / 'bootlace-logo-dark.svg').write_text(dark, encoding='utf8')
    for size in (512, 256, 128, 64, 32, 16):
        render(sized(mark, size, size), size, size).save(OUT / f'bootlace-mark-{size}.png')
    for name, svg in (('bootlace-logo', logo), ('bootlace-logo-dark', dark)):
        import re
        w, h = (int(v) for v in re.search(r'width="(\d+)" height="(\d+)"', svg).groups())
        render(sized(svg, w * 2, h * 2), w * 2, h * 2).save(OUT / f'{name}.png')
    sizes = [16, 32, 48, 64, 128, 256]
    base = render(sized(mark, 256, 256), 256, 256)
    base.save(OUT / 'bootlace.ico', sizes=[(s, s) for s in sizes])
    print('wrote', ', '.join(sorted(p.name for p in OUT.iterdir())))


if __name__ == '__main__':
    main()
