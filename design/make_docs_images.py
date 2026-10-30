#!/usr/bin/env python3
"""按程序里的实际几何/配色渲染文档配图（不是截图，可任意放大）。
几何取自 osd/osd_windows.go：
  窗口 420 宽；上下留白 18；图标 52；图标与文字间距 14
  标题 28px；正文 15px；行高 22；圆角 11
"""
import os, sys
from PIL import Image, ImageDraw, ImageFont

sys.path.insert(0, os.path.dirname(__file__))
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
A = os.path.join(ROOT, "osd", "assets")
OUT = os.path.join(ROOT, "docs")

S = 3                                   # 渲染倍率
WINW, PAD, ICON, GAP = 420, 18, 52, 14
TITLE, BODY, LINEH, RADIUS = 28, 15, 22, 11
BG = (30, 31, 34)
FG = (255, 255, 255)
DIM = (168, 172, 180)
FONT = "/usr/share/fonts/truetype/wqy/wqy-microhei.ttc"

MODES = [
    ("turbo",   "性能模式", "TURBO · 功耗墙 65W",   "65°C · 3075 rpm"),
    ("balance", "均衡模式", "BALANCE · 功耗墙 54W", "62°C · 2980 rpm"),
    ("silence", "静音模式", "SILENCE · 功耗墙 45W", "58°C · 2760 rpm"),
]


def osd(mode, title, sub, sensor, n_lines=2):
    h = PAD * 2 + max(ICON, TITLE + n_lines * LINEH)
    img = Image.new("RGB", (WINW * S, h * S), BG)
    d = ImageDraw.Draw(img)
    # 圆角
    mask = Image.new("L", img.size, 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, img.size[0]-1, img.size[1]-1],
                                           radius=RADIUS*S, fill=255)
    out = Image.new("RGB", img.size, (255, 255, 255))
    out.paste(img, (0, 0), mask)

    ic = Image.open(os.path.join(A, f"glyph_{mode}.png")).convert("RGBA")
    ic = ic.resize((ICON*S, ICON*S), Image.LANCZOS)
    out.paste(ic, (PAD*S, (h - ICON)//2 * S), ic)

    tx = (PAD + ICON + GAP) * S
    TGAP = 5
    textH = TITLE + TGAP + n_lines * LINEH
    top = (h - textH) // 2

    f_title = ImageFont.truetype(FONT, TITLE*S)
    f_body = ImageFont.truetype(FONT, BODY*S)
    d = ImageDraw.Draw(out)
    d.text((tx, top*S), title, font=f_title, fill=FG)
    d.text((tx, (top+TITLE+TGAP)*S), sub, font=f_body, fill=DIM)
    if n_lines > 1:
        d.text((tx, (top+TITLE+TGAP+LINEH)*S), sensor, font=f_body, fill=DIM)
    return out


def main():
    os.makedirs(OUT, exist_ok=True)
    shots = [osd(*m) for m in MODES]

    gap = 26 * S
    W = shots[0].width + gap
    H = sum(s.height for s in shots) + gap * (len(shots) + 1)
    sheet = Image.new("RGB", (W, H), (238, 240, 244))
    y = gap
    for s in shots:
        sheet.paste(s, (gap // 2, y))
        y += s.height + gap
    sheet.save(os.path.join(OUT, "osd-modes.png"))
    print("docs/osd-modes.png", sheet.size)


if __name__ == "__main__":
    main()
