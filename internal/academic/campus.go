package academic

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Location は教務ポータルの教室表記（工4-211、共2-104 など）から引いた建物と地図の位置。
//
// 座標は OpenStreetMap の建物データ（2026-11 取得）と Google マップで確認したもの。
// 略称から建物を特定できない教室（国セ・研セ・体 など）は返さない。推測で違う建物を指すより、地図を出さない方を選ぶ。
type Location struct {
	Building string  `json:"building"`         // 例: 工学部4号館
	Room     string  `json:"room,omitempty"`   // 例: 211
	Floor    int     `json:"floor,omitempty"`  // 部屋番号の百の位（211 → 2階）。分からなければ 0
	Detail   string  `json:"detail,omitempty"` // 例: 1階（共創技術ラボ）
	Campus   string  `json:"campus,omitempty"` // 例: 千原キャンパス
	Lat      float64 `json:"lat,omitempty"`    // 座標が無い施設は MapQuery で地図を出す
	Lng      float64 `json:"lng,omitempty"`
	MapQuery string  `json:"map_query"`        // 地図の埋め込み・検索に使う語（座標があれば "緯度,経度"）
	MapURL   string  `json:"map_url"`          // Google マップで開く URL
	Online   bool    `json:"online,omitempty"` // WebClass・遠隔など、建物が無い授業
}

type building struct {
	name     string
	lat, lng float64
	query    string // 座標が無い（学外の施設など）ときの検索語
	campus   string
}

const senbaru = "千原キャンパス"

func (b building) at(room string) *Location {
	l := &Location{Building: b.name, Room: room, Campus: b.campus, Lat: b.lat, Lng: b.lng}
	if l.Campus == "" && b.lat != 0 {
		l.Campus = senbaru
	}
	if b.lat != 0 {
		l.MapQuery = fmt.Sprintf("%.6f,%.6f", b.lat, b.lng)
	} else {
		l.MapQuery = b.query
	}
	l.MapURL = "https://www.google.com/maps/search/?api=1&query=" + url.QueryEscape(l.MapQuery)
	if n, err := strconv.Atoi(strings.TrimLeft(digitsPrefix(room), "0")); err == nil && n >= 100 && n < 1000 {
		l.Floor = n / 100
	}
	return l
}

func digitsPrefix(s string) string {
	for i, r := range s {
		if r < '0' || r > '9' {
			return s[:i]
		}
	}
	return s
}

var (
	engineering = map[string]building{
		"1": {name: "工学部1号館", lat: 26.253354, lng: 127.766432},
		"2": {name: "工学部2号館", lat: 26.252694, lng: 127.766447},
		"3": {name: "工学部3号館", lat: 26.252084, lng: 127.766882},
		"4": {name: "工学部4号館", lat: 26.252500, lng: 127.767506},
	}
	generalEducation = map[string]building{
		"1": {name: "共通教育棟1号館", lat: 26.248468, lng: 127.767372},
		"2": {name: "共通教育棟2号館", lat: 26.248185, lng: 127.767817},
		"3": {name: "共通教育棟3号館", lat: 26.247854, lng: 127.768108},
		"4": {name: "共通教育棟4号館", lat: 26.247994, lng: 127.767309},
	}
	scienceMain = map[string]building{
		"A": {name: "理学部本館 A棟", lat: 26.249083, lng: 127.767047},
		"B": {name: "理学部本館 B棟", lat: 26.249644, lng: 127.767344},
		"C": {name: "理学部本館 C棟", lat: 26.249318, lng: 127.767286},
	}
	agriculture = map[string]string{"西": "西棟", "中": "中棟", "東": "東棟"}

	scienceComplex  = building{name: "理学部複合棟", lat: 26.250035, lng: 127.767257}
	scienceAnnex    = building{name: "理学部別館", lat: 26.249459, lng: 127.766787}
	educationMain   = building{name: "教育学部本部棟", lat: 26.246806, lng: 127.765404}
	educationPract  = building{name: "教育実践棟", lat: 26.246210, lng: 127.765726}
	technology      = building{name: "技術教育棟", lat: 26.246354, lng: 127.764961}
	music           = building{name: "音楽棟", lat: 26.246170, lng: 127.765155}
	sculpture       = building{name: "彫刻工芸棟", lat: 26.246613, lng: 127.764742}
	humanitiesLect  = building{name: "文系講義棟", lat: 26.246830, lng: 127.767000}
	agricultureMain = building{name: "農学部本館", lat: 26.251167, lng: 127.764624}
	regionalStudies = building{name: "地域創生総合研究棟", lat: 26.253319, lng: 127.765864}
	infoCenter      = building{name: "情報基盤統括センター", lat: 26.251814, lng: 127.766935}
	workshop        = building{name: "工作工場棟", lat: 26.253086, lng: 127.767333}
	gym1            = building{name: "第一体育館", lat: 26.251596, lng: 127.768432}
	gym2            = building{name: "第二体育館", lat: 26.250552, lng: 127.768170}
	track           = building{name: "陸上競技場", lat: 26.250753, lng: 127.769597}
	library         = building{name: "附属図書館", lat: 26.248124, lng: 127.766496}
	medicine        = building{name: "医学部（西普天間キャンパス）", query: "琉球大学医学部 西普天間キャンパス", campus: "西普天間キャンパス"}
)

type roomRule struct {
	re   *regexp.Regexp
	find func(m []string) *Location
}

func fixed(b building) func([]string) *Location {
	return func(m []string) *Location {
		room := ""
		if len(m) > 1 {
			room = m[1]
		}
		return b.at(room)
	}
}

var roomRules = []roomRule{
	{regexp.MustCompile(`^工(?:学部)?([1-4])-?(\d{3}[A-Z]?)`), func(m []string) *Location { return engineering[m[1]].at(m[2]) }},
	{regexp.MustCompile(`^工(?:学部)?([1-4])号館`), func(m []string) *Location { return engineering[m[1]].at("") }},
	{regexp.MustCompile(`^共創技術ラボ`), func(m []string) *Location {
		l := engineering["1"].at("")
		l.Floor, l.Detail = 1, "1階（共創技術ラボ）"
		return l
	}},
	{regexp.MustCompile(`^共(?:通|通教育棟)?([1-4])-?(\d{3}[A-Z]?)`), func(m []string) *Location { return generalEducation[m[1]].at(m[2]) }},
	{regexp.MustCompile(`^共通教育棟([1-4])号館`), func(m []string) *Location { return generalEducation[m[1]].at("") }},
	{regexp.MustCompile(`^理?複(\d{3}[A-Z]?)`), fixed(scienceComplex)},
	{regexp.MustCompile(`^理学部複合棟`), fixed(scienceComplex)},
	{regexp.MustCompile(`^理?([ABC])(\d{3})`), func(m []string) *Location { return scienceMain[m[1]].at(m[2]) }},
	{regexp.MustCompile(`^別(\d{3})`), fixed(scienceAnnex)},
	{regexp.MustCompile(`^教育実践棟`), fixed(educationPract)},
	{regexp.MustCompile(`^教(?:育)?(\d{3})`), fixed(educationMain)},
	{regexp.MustCompile(`^技(\d{3})`), fixed(technology)},
	{regexp.MustCompile(`^音(\d{3})`), fixed(music)},
	{regexp.MustCompile(`^彫工?(\d{3})`), fixed(sculpture)},
	{regexp.MustCompile(`^文講(\d{3})`), fixed(humanitiesLect)},
	{regexp.MustCompile(`^文系講義棟`), fixed(humanitiesLect)},
	{regexp.MustCompile(`^農(西|中|東)(?:\([WCE]\))?-?(\d{3}[A-Z]?)`), func(m []string) *Location {
		l := agricultureMain.at(m[2])
		l.Building += " " + agriculture[m[1]]
		return l
	}},
	{regexp.MustCompile(`^地創棟|^地域創生総合研究棟`), fixed(regionalStudies)},
	{regexp.MustCompile(`^情報基盤統括`), fixed(infoCenter)},
	{regexp.MustCompile(`^工作工場`), fixed(workshop)},
	{regexp.MustCompile(`^第(?:1|一)体育館`), fixed(gym1)},
	{regexp.MustCompile(`^第(?:2|二)体育館`), fixed(gym2)},
	{regexp.MustCompile(`^陸上競技場`), fixed(track)},
	{regexp.MustCompile(`^(?:附属)?図書館`), fixed(library)},
	{regexp.MustCompile(`^医学部`), fixed(medicine)},
	{regexp.MustCompile(`^保(\d{3})`), fixed(medicine)},
	{regexp.MustCompile(`^千原フィールド`), fixed(building{name: "千原フィールド", query: "琉球大学 亜熱帯フィールド科学教育研究センター 千原フィールド", campus: senbaru})},
	{regexp.MustCompile(`^与那フィールド`), fixed(building{name: "与那フィールド（国頭村）", query: "琉球大学 与那フィールド", campus: "学外施設"})},
	{regexp.MustCompile(`^瀬底`), fixed(building{name: "瀬底研究施設（本部町）", query: "琉球大学 熱帯生物圏研究センター 瀬底研究施設", campus: "学外施設"})},
	{regexp.MustCompile(`^西表`), fixed(building{name: "西表研究施設（竹富町）", query: "琉球大学 熱帯生物圏研究センター 西表研究施設", campus: "学外施設"})},
	{regexp.MustCompile(`附属小学校`), fixed(building{name: "教育学部附属小学校", query: "琉球大学教育学部附属小学校", campus: "附属学校"})},
	{regexp.MustCompile(`附属中学校`), fixed(building{name: "教育学部附属中学校", query: "琉球大学教育学部附属中学校", campus: "附属学校"})},
}

var onlineRoom = regexp.MustCompile(`(?i)^(webclass|遠隔|オンライン|zoom|teams)`)
var roomSplit = regexp.MustCompile(`[,、，/／・\s]+`)

// NormalizeRoom は全角・半角カナ・ハイフンの揺れをそろえる（工1－322 → 工1-322、ﾗﾎﾞ → ラボ）。
func NormalizeRoom(s string) string {
	s = norm.NFKC.String(s)
	s = strings.NewReplacer("−", "-", "‐", "-", "–", "-", "—", "-").Replace(s)
	return strings.TrimSpace(s)
}

// LocateRoom は教室表記から建物を引く。複数の教室が並ぶ場合は最初に分かったものを返す。分からなければ nil。
func LocateRoom(room string) *Location {
	room = NormalizeRoom(room)
	if room == "" {
		return nil
	}
	compact := strings.ReplaceAll(room, " ", "")
	if onlineRoom.MatchString(compact) {
		return &Location{Building: "オンライン", Online: true}
	}
	for _, part := range append([]string{compact}, roomSplit.Split(room, -1)...) {
		for _, r := range roomRules {
			if m := r.re.FindStringSubmatch(part); m != nil {
				return r.find(m)
			}
		}
	}
	return nil
}

// Label は「工学部4号館 211（2階）」のような1行の表記。
func (l *Location) Label() string {
	if l == nil {
		return ""
	}
	s := l.Building
	if l.Room != "" {
		s += " " + l.Room
	}
	switch {
	case l.Detail != "":
		s += " " + l.Detail
	case l.Floor > 0:
		s += fmt.Sprintf("（%d階）", l.Floor)
	}
	return s
}
