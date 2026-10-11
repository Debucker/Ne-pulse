"""Generates the NE-PULSE logo set (SVG) and renders PNGs with headless Chrome.

    python brand/make.py

Outputs into brand/svg and brand/png. The mark is the NE-PULSE heartbeat:
a flat line, a sharp seismic blip, flat again.
"""
import os
import subprocess
import pathlib

ROOT = pathlib.Path(__file__).resolve().parent
SVG = ROOT / "svg"
PNG = ROOT / "png"
HTML = ROOT / ".render"
for d in (SVG, PNG, HTML):
    d.mkdir(exist_ok=True)

INK = "#05080b"
CYAN = "#38e1ff"
HOT = "#ff7a2f"
ICE = "#eaf1f4"
CHROME = r"C:\Program Files\Google\Chrome\Application\chrome.exe"
FONTS = '<link href="https://fonts.googleapis.com/css2?family=Archivo:wdth,wght@62..125,700..900&family=IBM+Plex+Mono:wght@500&display=block" rel="stylesheet">'

# The heartbeat on a 100-unit grid (x 14..86, centred on y 52).
PULSE = "M14 52 H37 L44.5 27 L55.5 77 L64.5 35 L70.5 52 H86"


def pulse(x, y, size, color, width, glow=None, gradient=None):
    """The heartbeat scaled to `size` px wide, top-left at (x, y)."""
    k = size / 100
    stroke = f"url(#{gradient})" if gradient else color
    g = f'<g transform="translate({x} {y}) scale({k})" fill="none" stroke-linecap="round" stroke-linejoin="round">'
    if glow:
        g += f'<path d="{PULSE}" stroke="{glow}" stroke-width="{width * 2.6}" stroke-opacity="0.35" filter="url(#blur)"/>'
    g += f'<path d="{PULSE}" stroke="{stroke}" stroke-width="{width}"/></g>'
    return g


def defs(extra=""):
    return f'''<defs>
  <filter id="blur" x="-30%" y="-30%" width="160%" height="160%"><feGaussianBlur stdDeviation="3.2"/></filter>
  <linearGradient id="wave" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="{CYAN}"/><stop offset="0.55" stop-color="{CYAN}"/><stop offset="1" stop-color="{HOT}"/></linearGradient>
  {extra}
</defs>'''


def svg(w, h, body, extra_defs=""):
    return f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="0 0 {w} {h}">{defs(extra_defs)}{body}</svg>\n'


def rings(cx, cy, color, radii, opacities, width=3):
    return "".join(f'<circle cx="{cx}" cy="{cy}" r="{r}" fill="none" stroke="{color}" stroke-opacity="{o}" stroke-width="{width}"/>' for r, o in zip(radii, opacities))


# ---------------------------------------------------------------- the files
files = {}

# 1. Icon only (transparent), colour and single-colour versions.
files["icon"] = svg(512, 512, pulse(56, 56 - 0, 400, CYAN, 8, glow=CYAN).replace("translate(56 56)", "translate(56 48)"))
files["icon-mono-white"] = svg(512, 512, pulse(56, 48, 400, "#ffffff", 8))
files["icon-mono-black"] = svg(512, 512, pulse(56, 48, 400, INK, 8))

# 2. Telegram avatars, 640x640. Telegram shows them as a circle: keep the
#    mark inside the middle ~70%.
glow_defs = f'<radialGradient id="g" cx="50%" cy="50%" r="50%"><stop offset="0" stop-color="{CYAN}" stop-opacity="0.22"/><stop offset="1" stop-color="{CYAN}" stop-opacity="0"/></radialGradient>'
files["avatar-dark"] = svg(640, 640,
    f'<rect width="640" height="640" fill="{INK}"/><circle cx="320" cy="320" r="320" fill="url(#g)"/>'
    + rings(320, 320, CYAN, [150, 215, 280], [0.16, 0.10, 0.05])
    + pulse(110, 102, 420, CYAN, 7, glow=CYAN), glow_defs)

files["avatar-cyan"] = svg(640, 640,
    f'<rect width="640" height="640" fill="{CYAN}"/>'
    + rings(320, 320, INK, [150, 215, 280], [0.14, 0.09, 0.05])
    + pulse(110, 102, 420, INK, 7.5))

hot_defs = f'<radialGradient id="g" cx="50%" cy="50%" r="50%"><stop offset="0" stop-color="{HOT}" stop-opacity="0.18"/><stop offset="1" stop-color="{HOT}" stop-opacity="0"/></radialGradient>'
files["avatar-wave"] = svg(640, 640,
    f'<rect width="640" height="640" fill="{INK}"/><circle cx="320" cy="320" r="320" fill="url(#g)"/>'
    + rings(320, 320, CYAN, [168], [0.35], 3) + rings(320, 320, HOT, [236], [0.35], 3)
    + pulse(110, 102, 420, CYAN, 7, glow=CYAN, gradient="wave"), hot_defs)

files["avatar-light"] = svg(640, 640,
    f'<rect width="640" height="640" fill="{ICE}"/>'
    + rings(320, 320, INK, [150, 215, 280], [0.08, 0.05, 0.03])
    + pulse(110, 102, 420, INK, 7.5))

files["avatar-ring"] = svg(640, 640,
    f'<rect width="640" height="640" fill="{INK}"/>'
    f'<circle cx="320" cy="320" r="250" fill="none" stroke="{CYAN}" stroke-width="10"/>'
    + pulse(135, 115, 370, ICE, 7.5))

# 3. Wordmarks (icon + NE-PULSE), dark and light, 1200x360.
def wordmark(bg, mark, text, sub):
    bgrect = f'<rect width="1200" height="360" fill="{bg}"/>' if bg else ""
    return svg(1200, 360,
        bgrect
        + pulse(70, 80, 220, mark, 8, glow=mark if bg == INK else None)
        + f'<text x="320" y="214" font-family="Archivo, Arial Black, sans-serif" font-weight="800" font-size="124" style="font-stretch:112%" fill="{text}" letter-spacing="-2">NE-PULSE</text>'
        + f'<text x="326" y="262" font-family="IBM Plex Mono, monospace" font-size="24" letter-spacing="6" fill="{sub}">A SEISMIC NETWORK MADE OF PHONES</text>')

files["wordmark-dark"] = wordmark(INK, CYAN, ICE, "#6d808b")
files["wordmark-light"] = wordmark(ICE, INK, INK, "#5a6b75")
files["wordmark-transparent"] = wordmark(None, CYAN, "#ffffff", "#a9b8c0")

# 4. Telegram bot description picture (shown above "Start"), 640x360.
files["telegram-intro"] = svg(640, 360,
    f'<rect width="640" height="360" fill="{INK}"/>'
    f'<circle cx="320" cy="150" r="230" fill="url(#g)"/>'
    + rings(320, 135, CYAN, [80, 125, 170], [0.18, 0.10, 0.05])
    + pulse(225, 40, 190, CYAN, 8, glow=CYAN)
    + f'<text x="320" y="282" text-anchor="middle" font-family="Archivo, Arial Black, sans-serif" font-weight="800" font-size="54" style="font-stretch:112%" fill="{ICE}">NE-PULSE</text>'
    + f'<text x="320" y="318" text-anchor="middle" font-family="IBM Plex Mono, monospace" font-size="15" letter-spacing="4" fill="#a9b8c0">EARTHQUAKE ALERTS · UZBEKISTAN</text>', glow_defs)

# ---------------------------------------------------------------- write + render
SIZES = {"icon": [512, 1024], "icon-mono-white": [512], "icon-mono-black": [512]}
for name, data in files.items():
    (SVG / f"{name}.svg").write_text(data, encoding="utf-8")
    w = int(data.split('width="')[1].split('"')[0])
    h = int(data.split('height="')[1].split('"')[0])
    for scale_w in SIZES.get(name, [w]):
        k = scale_w / w
        W, H = round(w * k), round(h * k)
        transparent = name.startswith("icon") or name == "wordmark-transparent"
        page = HTML / f"{name}-{W}.html"
        page.write_text(
            f'<!doctype html><html><head><meta charset="utf-8">{FONTS}<style>html,body{{margin:0;background:transparent}}svg{{display:block;width:{W}px;height:{H}px}}</style></head>'
            f'<body>{data}</body></html>', encoding="utf-8")
        out = PNG / (f"{name}.png" if W == w else f"{name}-{W}.png")
        args = [CHROME, "--headless=new", "--hide-scrollbars", f"--window-size={W},{H}", "--virtual-time-budget=4000",
                f"--screenshot={out}", page.as_uri()]
        if transparent:
            args.insert(2, "--default-background-color=00000000")
        subprocess.run(args, check=True, capture_output=True)
        print("rendered", out.name)
