#!/usr/bin/env python3
"""把图标渲染成程序运行期资源（osd/assets）。改完设计后跑一次即可。"""
import os, sys
sys.path.insert(0, os.path.dirname(__file__))
import gen_icons as G
from PIL import Image

A = os.path.join(os.path.dirname(os.path.dirname(__file__)), "osd", "assets")
os.makedirs(A, exist_ok=True)

G.badge("app").save(os.path.join(A, "app.ico"),
                    sizes=[(16,16),(20,20),(24,24),(32,32),(40,40),(48,48),(64,64),(128,128),(256,256)])
for n in ["app", "turbo", "balance", "silence"]:
    G.badge(n).resize((256,256), Image.LANCZOS).save(os.path.join(A, f"badge_{n}.png"))
    G.osd_glyph(n, colored=True).resize((512,512), Image.LANCZOS).save(os.path.join(A, f"glyph_{n}.png"))
    G.tray(n, dark=True).resize((64,64), Image.LANCZOS).save(os.path.join(A, f"tray_dark_{n}.png"))
    G.tray(n, dark=False).resize((64,64), Image.LANCZOS).save(os.path.join(A, f"tray_light_{n}.png"))
print("osd/assets 已更新")
