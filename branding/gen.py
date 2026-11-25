"""행성 포탑 방어전(celestial-cannon) 아이콘 생성기 — python3 gen.py 로 icon.svg / icon-small.svg 를 다시 만든다.
래스터·ico·syso 는 README 의 "아이콘" 명령으로."""
import math
import os

GRAD = '''  <defs>
    <linearGradient id="g" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%"   stop-color="#6366f1"/>
      <stop offset="50%"  stop-color="#8b5cf6"/>
      <stop offset="100%" stop-color="#06b6d4"/>
    </linearGradient>
    <clipPath id="sq"><rect x="112" y="112" width="800" height="800" rx="184"/></clipPath>
  </defs>

  <rect x="112" y="112" width="800" height="800" rx="184" fill="url(#g)"/>
'''


def surface(P, x):
    """행성(원) P=(cx, cy, r) 윗면에서 x 위치의 높이."""
    cx, cy, r = P
    return cy - math.sqrt(r * r - (x - cx) ** 2)


def star4(x, y, r):
    """반짝이는 네 갈래 별(북극성)."""
    k = r * 0.28
    pts = [(x, y - r), (x + k, y - k), (x + r, y), (x + k, y + k),
           (x, y + r), (x - k, y + k), (x - r, y), (x - k, y - k)]
    return "M" + " L".join(f"{a:.1f} {b:.1f}" for a, b in pts) + " Z"


def arc(cx, cy, r, a0, a1, w, op):
    """북극성 중심 별 자취. 각도는 도(화면 좌표: 0°=오른쪽, 시계 방향으로 증가)."""
    x0, y0 = cx + r * math.cos(math.radians(a0)), cy + r * math.sin(math.radians(a0))
    x1, y1 = cx + r * math.cos(math.radians(a1)), cy + r * math.sin(math.radians(a1))
    large = 1 if (a1 - a0) % 360 > 180 else 0
    return (f'    <path d="M {x0:.1f} {y0:.1f} A {r} {r} 0 {large} 1 {x1:.1f} {y1:.1f}" '
            f'stroke-width="{w}" opacity="{op}"/>\n')


def cannon(bx, by, tx, ty, w):
    """포신(둥근 끝 굵은 선) + 받침(원). b = 받침 중심, t = 포구 끝."""
    return (f'  <line x1="{bx:.0f}" y1="{by:.0f}" x2="{tx:.0f}" y2="{ty:.0f}" stroke="#ffffff" '
            f'stroke-width="{w}" stroke-linecap="round"/>\n'
            f'  <circle cx="{bx:.0f}" cy="{by:.0f}" r="{w * 0.95:.0f}" fill="#ffffff"/>\n')


def aim(bx, by, px, py, length):
    a = math.atan2(py - by, px - bx)
    return bx + math.cos(a) * length, by + math.sin(a) * length


def big():
    P = (512, 1250, 620)         # 행성 — 윗부분만 보이는 넓은 언덕(지평선)
    px, py = 700, 318            # 북극성
    ex, er = 318, 72             # 죽방(행성 왼쪽 위에 선다)
    # 몸통 밑변의 두 모서리가 모두 땅속에 묻히게(행성이 둥글어 낮은 쪽 모서리가 뜨면 예각 틈이 생긴다)
    base = max(surface(P, ex - 86), surface(P, ex + 86)) + 12
    ey = base - 204              # 공용 비율: 머리 중심 → 밑변 204
    bx = 548
    by = surface(P, bx) - 6      # 대포 받침
    tx, ty = aim(bx, by, px, py, 170)
    trails = (arc(px, py, 84, 160, 330, 16, 0.9)
              + arc(px, py, 134, 180, 315, 14, 0.7)
              + arc(px, py, 184, 198, 300, 12, 0.5))
    return f'''<svg width="1024" height="1024" viewBox="0 0 1024 1024" xmlns="http://www.w3.org/2000/svg">
  <!--
    행성 포탑 방어전(celestial-cannon) 아이콘 — 큰 크기용(256px 이상). gen.py 로 생성.

    teaveloper 공용 규격: 800x800 라운드 사각형(rx 184) · 공식 그라데이션 · 흰색 죽방
    (원 r72 · 밑변 172 · 높이 156).

    이 앱만의 것: 죽방이 도는 행성 위에 서 있고, 옆의 대포가 북극성을 겨눈다.
    하늘에는 북극성을 중심으로 한 별 자취(일주운동). 작은 크기(16·32·48px)는 icon-small.svg.
  -->
{GRAD}
  <g clip-path="url(#sq)">
  <g fill="none" stroke="#ffffff" stroke-linecap="round">
{trails}  </g>
  <path d="{star4(px, py, 38)}" fill="#ffffff"/>
{cannon(bx, by, tx, ty, 54)}
  <circle cx="{P[0]}" cy="{P[1]}" r="{P[2]}" fill="#ffffff"/>

  <!-- 죽방 — 원 r{er} · 밑변 172 · 높이 156 -->
  <g fill="#ffffff">
    <circle cx="{ex}" cy="{ey:.0f}" r="{er}"/>
    <path d="M{ex} {ey + 48:.0f} L{ex - 86} {base:.0f} L{ex + 86} {base:.0f} Z"/>
  </g>
  </g>
</svg>
'''


def small():
    """16px 에서도 보이게: 행성 · 대포 · 북극성만, 굵게."""
    P = (512, 1250, 620)
    px, py = 690, 330
    bx = 470
    by = surface(P, bx) - 10
    tx, ty = aim(bx, by, px, py, 190)
    return f'''<svg width="1024" height="1024" viewBox="0 0 1024 1024" xmlns="http://www.w3.org/2000/svg">
  <!-- 행성 포탑 방어전 아이콘 — 작은 크기용(16·32·48px). gen.py 로 생성. 죽방·별 자취를 빼고 행성·대포·북극성만. -->
{GRAD}
  <g clip-path="url(#sq)">
  <path d="{star4(px, py, 92)}" fill="#ffffff"/>
{cannon(bx, by, tx, ty, 96)}
  <circle cx="{P[0]}" cy="{P[1]}" r="{P[2]}" fill="#ffffff"/>
  </g>
</svg>
'''


if __name__ == "__main__":
    here = os.path.dirname(os.path.abspath(__file__))
    for name, svg in (("icon.svg", big()), ("icon-small.svg", small())):
        with open(os.path.join(here, name), "w", encoding="utf-8", newline="\n") as f:
            f.write(svg)
        print("wrote", name)
