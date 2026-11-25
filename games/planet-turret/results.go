package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math/bits"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// 판 결과는 데이터 폴더의 planet-turret-results/<날짜>/<시각>-<판>.json 에 한 판씩 저장한다.
// 교사 화면에서 판마다 엑셀(CSV)로 내려받는다.

type Row struct {
	Rank int    `json:"rank"`
	ID   string `json:"-"`
	SID  string `json:"sid"`
	Name string `json:"name"`
	Progress
}

type Result struct {
	Round   int       `json:"round"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended"`
	Minutes int       `json:"minutes"`
	Rows    []Row     `json:"rows"`
}

func resultsDir() string { return filepath.Join(dataDir(), "planet-turret-results") }

func saveResult(r *Result) (string, error) {
	date := r.Started.Format("2006-01-02")
	file := fmt.Sprintf("%s-%d판.json", r.Started.Format("150405"), r.Round)
	dir := filepath.Join(resultsDir(), date)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, file), b, 0644); err != nil {
		return "", err
	}
	return date + "/" + file, nil
}

var (
	validDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	validFile = regexp.MustCompile(`^\d{6}-\d+판\.json$`)
)

func loadResult(ref string) (*Result, error) {
	date, file := filepath.Split(filepath.ToSlash(ref))
	date = filepath.Clean(date)
	if !validDate.MatchString(date) || !validFile.MatchString(file) {
		return nil, errors.New("잘못된 파일 이름")
	}
	b, err := os.ReadFile(filepath.Join(resultsDir(), date, file))
	if err != nil {
		return nil, err
	}
	var r Result
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

type resultSummary struct {
	Ref     string    `json:"ref"`
	Round   int       `json:"round"`
	Started time.Time `json:"started"`
	Minutes int       `json:"minutes"`
	Players int       `json:"players"`
	Top     string    `json:"top"`
	TopScr  int       `json:"topScore"`
}

func listResults() ([]resultSummary, error) {
	dates, err := os.ReadDir(resultsDir())
	if errors.Is(err, os.ErrNotExist) {
		return []resultSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []resultSummary{}
	for _, d := range dates {
		if !d.IsDir() || !validDate.MatchString(d.Name()) {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(resultsDir(), d.Name()))
		for _, f := range files {
			if !validFile.MatchString(f.Name()) {
				continue
			}
			ref := d.Name() + "/" + f.Name()
			r, err := loadResult(ref)
			if err != nil {
				continue
			}
			s := resultSummary{Ref: ref, Round: r.Round, Started: r.Started, Minutes: r.Minutes, Players: len(r.Rows)}
			if len(r.Rows) > 0 {
				s.Top, s.TopScr = r.Rows[0].Name, r.Rows[0].Score
			}
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out, nil
}

// LevelNames 는 게임 index.html 의 LEVELS 순서와 같다.
var LevelNames = []string{"북극", "적도·동쪽", "서울·동쪽", "서울·남쪽", "서울·서쪽", "서울·북쪽"}

func resultCSV(r *Result) []byte {
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf") // 엑셀이 한글을 바로 읽도록 BOM
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"순위", "학번", "이름", "점수", "최고 레벨", "마지막 장소", "가 본 장소(6곳)", "빌런 격파", "악당 격파", "포탑 부서짐", "발사", "명중", "명중률(%)"})
	for _, x := range r.Rows {
		acc := ""
		if x.Shots > 0 {
			acc = strconv.Itoa(x.Hits * 100 / x.Shots)
		}
		place := ""
		if x.Lvl >= 0 && x.Lvl < len(LevelNames) {
			place = LevelNames[x.Lvl]
		}
		_ = w.Write([]string{
			strconv.Itoa(x.Rank), x.SID, x.Name, strconv.Itoa(x.Score),
			strconv.Itoa(x.Loop*len(LevelNames) + x.Lvl + 1), place,
			strconv.Itoa(bits.OnesCount(uint(x.Seen))),
			strconv.Itoa(x.Bosses), strconv.Itoa(x.Kills), strconv.Itoa(x.Breaks),
			strconv.Itoa(x.Shots), strconv.Itoa(x.Hits), acc,
		})
	}
	w.Flush()
	return buf.Bytes()
}

// ── 교사 전용 API ───────────────────────────────────────────
// 학생도 같은 AP 에서 서버에 닿으므로 결과 열람은 교사 PC 본인(localhost)에서만 허용한다.

func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func localOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLoopback(r) {
			http.Error(w, "forbidden — 교사 화면은 교사 PC 에서만 열 수 있습니다.", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func handleListResults(w http.ResponseWriter, r *http.Request) {
	list, err := listResults()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"results": list})
}

func handleResultCSV(w http.ResponseWriter, r *http.Request) {
	res, err := loadResult(r.URL.Query().Get("f"))
	if err != nil {
		http.Error(w, "결과 파일을 찾을 수 없습니다", http.StatusNotFound)
		return
	}
	name := fmt.Sprintf("행성포탑-%s-%d판.csv", res.Started.Format("2006-01-02-1504"), res.Round)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+urlPathEscape(name))
	_, _ = w.Write(resultCSV(res))
}

func urlPathEscape(s string) string {
	const hexd = "0123456789ABCDEF"
	out := make([]byte, 0, len(s)*3)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_' {
			out = append(out, c)
		} else {
			out = append(out, '%', hexd[c>>4], hexd[c&15])
		}
	}
	return string(out)
}
