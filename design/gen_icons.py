#!/usr/bin/env python3
"""perfmode 图标终稿
风格依据：
  - Windows 11 图标规范：满铺圆角块、白色简洁字形、渐变克制(1~2 档)、不含文字
  - Fluent 系统图标：线宽统一、全圆角端点、断口 1px、24px 网格 2px 内边距
  - 托盘图标惯例：单色，深色任务栏用白 / 浅色用黑
"""
import math, os
from PIL import Image, ImageDraw, ImageFont

OUT = os.path.join(os.path.dirname(__file__), "out")
FONT = "/usr/share/fonts/truetype/wqy/wqy-microhei.ttc"

TILE = {
    "app":     ((60, 65, 74), (41, 45, 52)),
    "turbo":   ((158, 62, 45), (118, 43, 31)),
    "balance": ((47, 93, 140), (30, 60, 96)),
    "silence": ((34, 107, 87),  (20, 68, 56)),
}
FILL = {"app": 1.00, "turbo": 1.00, "balance": 0.55, "silence": 0.18}
A0, A1 = 135, 405          # 270° 弧，底部开口
GRID = 48                   # 设计网格


def lerp(a, b, t):
    return tuple(int(round(a[i] + (b[i] - a[i]) * t)) for i in range(3))


def grad(S, top, bot):
    im = Image.new("RGB", (S, S)); d = ImageDraw.Draw(im)
    for y in range(S):
        d.line([(0, y), (S, y)], fill=lerp(top, bot, y / max(1, S - 1)))
    return im


def rmask(S, r):
    m = Image.new("L", (S, S), 0)
    ImageDraw.Draw(m).rounded_rectangle([0, 0, S - 1, S - 1], radius=r, fill=255)
    return m


def pts(cx, cy, r, a0, a1, step=0.2):
    out, a = [], a0
    while a <= a1:
        out.append((cx + r * math.cos(math.radians(a)), cy + r * math.sin(math.radians(a))))
        a += step
    return out


def stroke(img, cx, cy, r, a0, a1, w, col):
    """分层绘制再 composite —— ImageDraw 直接画 RGBA 不会做 alpha 混合"""
    layer = Image.new("RGBA", img.size, (0, 0, 0, 0))
    d = ImageDraw.Draw(layer)
    for p in pts(cx, cy, r, a0, a1):
        d.ellipse([p[0]-w/2, p[1]-w/2, p[0]+w/2, p[1]+w/2], fill=col)
    return Image.alpha_composite(img, layer)


def badge(mode, S=1024):
    img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    img.paste(grad(S, *TILE[mode]).convert("RGBA"), (0, 0), rmask(S, int(S * 0.215)))
    cx = cy = S / 2
    R, w = S * 0.285, S * 0.072
    img = stroke(img, cx, cy, R, A0, A1, w, (255, 255, 255, 56))
    img = stroke(img, cx, cy, R, A0, A0 + (A1 - A0) * FILL[mode], w, (255, 255, 255, 255))
    if mode == "app":
        # 主图标加一个轴心点，强化“仪表”语义，同时与三档区分
        layer = Image.new("RGBA", (S, S), (0, 0, 0, 0))
        dd = ImageDraw.Draw(layer)
        dr = w * 0.62
        dd.ellipse([cx - dr, cy - dr, cx + dr, cy + dr], fill=(255, 255, 255, 255))
        img = Image.alpha_composite(img, layer)
    return img


def tray(mode, S=1024, dark=True):
    img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    cx = cy = S / 2
    R, w = S * 0.320, S * 0.128
    fg = (255, 255, 255) if dark else (24, 26, 30)
    img = stroke(img, cx, cy, R, A0, A1, w, fg + (66,))
    img = stroke(img, cx, cy, R, A0, A0 + (A1 - A0) * FILL[mode], w, fg + (255,))
    return img


def save(img, name, sizes):
    for s in sizes:
        img.resize((s, s), Image.LANCZOS).save(os.path.join(OUT, f"{name}_{s}.png"))


def main():
    os.makedirs(OUT, exist_ok=True)
    for f in os.listdir(OUT):
        os.remove(os.path.join(OUT, f))

    names = ["app", "turbo", "balance", "silence"]
    badges = {n: badge(n) for n in names}
    td = {n: tray(n, dark=True) for n in names}
    tl = {n: tray(n, dark=False) for n in names}

    # 注意：运行时用的图标资源由 build_assets.py 生成到 osd/assets，
    # 这里只产出设计稿预览。
    F = ImageFont.truetype(FONT, 15)
    FT = ImageFont.truetype(FONT, 19)
    W, H = 1060, 1240
    sheet = Image.new("RGB", (W, H), (250, 251, 252))
    d = ImageDraw.Draw(sheet)

    d.text((36, 18), "perfmode 图标终稿", font=FT, fill=(38, 42, 50))
    d.text((36, 46), "满铺色块 + 白色字形；环填充比例表示档位：静音 18% · 均衡 55% · 性能 100%", font=F, fill=(125, 131, 143))

    x = 36
    for n in names:
        im = badges[n].resize((170, 170), Image.LANCZOS)
        sheet.paste(im, (x, 82), im)
        d.text((x + 2, 258), n, font=F, fill=(120, 126, 138))
        x += 246

    d.text((36, 300), "实际尺寸", font=FT, fill=(38, 42, 50))
    y = 334
    for n in names:
        xx = 36
        for s in [64, 48, 32, 24, 16]:
            im = badges[n].resize((s, s), Image.LANCZOS)
            sheet.paste(im, (xx, y + (64 - s)), im)
            xx += s + 13
        z = badges[n].resize((16, 16), Image.LANCZOS).resize((64, 64), Image.NEAREST)
        sheet.paste(z, (xx + 20, y), z)
        d.text((xx + 22, y + 68), "16px ×4", font=F, fill=(160, 166, 178))
        d.text((470, y + 22), n, font=F, fill=(140, 146, 158))
        y += 96

    for bg, label, st in [((32, 32, 32), "托盘 · 深色任务栏（白色）", td),
                          ((243, 243, 243), "托盘 · 浅色任务栏（黑色）", tl)]:
        strip = Image.new("RGB", (W - 72, 82), bg)
        sd = ImageDraw.Draw(strip)
        sd.text((14, 6), label, font=F, fill=(165, 170, 180) if bg[0] < 128 else (110, 116, 128))
        xx = 14
        for n in names:
            for s in [32, 24, 20, 16]:
                im = st[n].resize((s, s), Image.LANCZOS)
                strip.paste(im, (xx, 40 + (32 - s)), im)
                xx += s + 9
            xx += 24
        sheet.paste(strip, (36, y))
        y += 90

    d.text((36, y + 6), "OSD 效果", font=FT, fill=(38, 42, 50))
    y += 40
    for n, cn in [("turbo", "性能模式"), ("balance", "均衡模式"), ("silence", "静音模式")]:
        box = Image.new("RGBA", (392, 88), (0, 0, 0, 0))
        bd = ImageDraw.Draw(box)
        bd.rounded_rectangle([0, 0, 391, 87], radius=11, fill=(28, 29, 33, 250))
        ic = badges[n].resize((54, 54), Image.LANCZOS)
        box.alpha_composite(ic, (17, 17))
        bd.text((88, 20), cn, font=ImageFont.truetype(FONT, 21), fill=(255, 255, 255))
        sub = {"turbo": "TURBO · 65W", "balance": "BALANCE · 54W", "silence": "SILENCE · 45W"}[n]
        bd.text((90, 51), sub, font=F, fill=(150, 155, 165))
        sheet.paste(box, (36, y), box)
        y += 98

    sheet.save(os.path.join(OUT, "preview.png"))
    print("ok")


if __name__ == "__main__":
    main()


# ============================================================
# OSD 专用字形：透明底，不带色块（色块在深色弹窗里太重）
# ============================================================
OSD_ACCENT = {                      # 深色底上用的提亮版强调色
    "turbo":   (240, 122, 94),
    "balance": (107, 165, 231),
    "silence": (72, 195, 155),
    "app":     (232, 234, 238),
}


def osd_glyph(mode, S=1024, colored=True):
    img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    cx = cy = S / 2
    R, w = S * 0.300, S * 0.105
    img = stroke(img, cx, cy, R, A0, A1, w, (255, 255, 255, 42))
    col = (OSD_ACCENT[mode] if colored else (255, 255, 255)) + (255,)
    img = stroke(img, cx, cy, R, A0, A0 + (A1 - A0) * FILL[mode], w, col)
    return img


def osd_mockup(path, colored=True):
    F = ImageFont.truetype(FONT, 15)
    W, H = 780, 400
    sheet = Image.new("RGB", (W, H), (238, 240, 244))
    d = ImageDraw.Draw(sheet)
    d.text((30, 18), "OSD 方案 " + ("① 强调色字形" if colored else "② 纯白单色字形"),
           font=ImageFont.truetype(FONT, 18), fill=(40, 44, 52))
    y = 60
    for n, cn in [("turbo", "性能模式"), ("balance", "均衡模式"), ("silence", "静音模式")]:
        box = Image.new("RGBA", (392, 88), (0, 0, 0, 0))
        bd = ImageDraw.Draw(box)
        bd.rounded_rectangle([0, 0, 391, 87], radius=11, fill=(28, 29, 33, 250))
        ic = osd_glyph(n, colored=colored).resize((56, 56), Image.LANCZOS)
        box.alpha_composite(ic, (16, 16))
        bd.text((88, 20), cn, font=ImageFont.truetype(FONT, 21), fill=(255, 255, 255))
        sub = {"turbo": "TURBO · 65W", "balance": "BALANCE · 54W", "silence": "SILENCE · 45W"}[n]
        bd.text((90, 51), sub, font=F, fill=(150, 155, 165))
        sheet.paste(box, (30, y), box)
        y += 100
    sheet.save(path)
