"""Generate synthetic tables for the PDF parser. Requires ReportLab and a CJK TrueType font."""
from pathlib import Path
import sys

from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.pdfgen.canvas import Canvas

HERE = Path(__file__).resolve().parent
FONT = "FixtureFont"


def canvas(name):
    return Canvas(str(HERE / name), pagesize=(960, 720), invariant=True)


def vertical_lines(pdf, edges, bottom, top):
    for x in edges:
        pdf.rect(x, bottom, 0.8, top - bottom, stroke=0, fill=1)


def text(pdf, x, y, value):
    pdf.setFont(FONT, 9)
    pdf.drawString(x, y, value)


def timetable(name, pages, separate_characters=False):
    pdf = canvas(name)
    edges = (24, 98, 172, 304, 350, 400, 466, 534, 570, 616, 680, 776, 936)
    headers = ("講義コード", "科目番号", "科目名", "必修", "単位",
               "登録人員", "受講年次", "曜日", "時限", "教室", "期間", "備考")
    for rows in pages:
        top, height = 616, 44
        bottom = top - height * len(rows)
        vertical_lines(pdf, edges, bottom, 650)
        for col, value in enumerate(headers):
            text(pdf, edges[col] + 4, 633, value)
        for i in range(len(rows) + 1):
            pdf.rect(edges[0], top - height * i, edges[-1] - edges[0], 0.8,
                     stroke=0, fill=1)
        for i, row in enumerate(rows):
            code, number, title, capacity, day, period, quarter, notes = row
            y = top - height * i - 16
            values = (code, number, title, "選択", "2", capacity, "2", day, period,
                      "試験棟101", quarter, notes)
            for col, value in enumerate(values):
                x = edges[col] + 4
                for j, line in enumerate(value.splitlines()):
                    if separate_characters and col == 0:
                        for char in line:
                            text(pdf, x, y, char)
                            x += pdfmetrics.stringWidth(char, FONT, 9)
                    else:
                        text(pdf, x, y - j * 12, line)
        text(pdf, 24, 670, "試験用時間割（架空）")
        pdf.showPage()
    pdf.save()


def handbook(name, prefix, required, selective, examples):
    pdf = canvas(f"required2024_{name}.pdf")
    edges = (24, 88, 164, 330, 383, 476, 540, 616, 782, 835, 936)
    vertical_lines(pdf, edges, 50, 650)
    text(pdf, 24, 675, "試験用 授業科目分類表")
    headers = ("分類", "科目番号", "科目名", "単位", "必修")
    for start in (0, 5):
        for col, value in enumerate(headers):
            text(pdf, edges[start + col] + 4, 630, value)

    rows = list(examples)
    for i in range(required - len(examples)):
        rows.append((f"{prefix}{501 + i}", f"架空の必修演習{i + 1}", "2", "○"))
    for i in range(selective):
        rows.append((f"{prefix}{801 + i}", f"架空の選択必修演習{i + 1}", "2", "選必"))
    rows.append((f"{prefix}950", "隣接する選択科目", "1", ""))
    half = (len(rows) + 1) // 2
    for i, row in enumerate(rows):
        start = 5 if i >= half else 0
        for col, value in enumerate(("専門",) + row):
            text(pdf, edges[start + col] + 4, 606 - (i % half) * 24, value)
    pdf.showPage()
    pdf.save()


def main(font_path):
    pdfmetrics.registerFont(TTFont(FONT, font_path))
    first = ("990100001", "機械983", "試験流体演習", "20", "火", "4", "第1クォータ",
             "基礎演習を受講済み\n未受講者は履修不可")
    neighbor = ("990100002", "機械984", "試験設計演習", "24", "火", "5", "第1クォータ", "")
    next_page = ("990100003", "機械985", "試験応用演習", "25", "火", "4～5", "第2クォータ", "")
    timetable("fusion2026.pdf", [[first, neighbor], [next_page]])
    timetable("course2026.pdf", [[("990100004", "知能977", "試験コース演習", "75",
                                  "水", "3", "前学期", "対象年度の学生のみ")]])
    timetable("course_autumn2026.pdf", [[("990100005", "工共973", "試験設計実習", "90",
                                         "木", "4", "後学期", "別の演習と同時に履修")]], True)
    timetable("fusion_intro_autumn2026.pdf", [[next_page]], True)
    for name, prefix, required, selective, examples in (
        ("kikai", "機械", 33, 0, ()),
        ("energy", "エネ", 28, 8, ()),
        ("denki", "電気", 25, 8, ()),
        ("denshi-", "電情", 25, 8, ()),
        ("shakai", "社基", 28, 2, (("社基350", "試験構造演習", "2", "○"),)),
        ("kenchiku", "建築", 27, 0, (("建築492", "試験建築倫理", "1", "○"),)),
        ("chinot", "知能", 31, 0, (
            ("知能210", "試験データベース", "2", "○"),
            ("知能301", "試験総合実験", "2", "○"),
            ("工共302", "試験デザイン演習", "2", "○"),
            ("工共405", "試験セミナー", "1", "○"),
        )),
    ):
        handbook(name, prefix, required, selective, examples)


if __name__ == "__main__":
    main(sys.argv[1])
