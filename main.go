// celestial-cannon — 같은 교내망의 학생들이 QR 로 접속해 함께 하는 "행성 포탑 방어전" 교실 서버.
// 중학교 1학년 과학 '일주운동' 단원. 구조는 형제 프로젝트 vector-soccer · Teavel_SubMarine 과 같다.
//
// 교사 PC 에서 이 exe 를 더블클릭하면:
//
//	① LAN IP 를 탐지하고 0.0.0.0 에 바인딩(학생이 붙을 수 있게).
//	② 기본 브라우저로 교사 화면(/host)을 연다 — 접속 QR · 판 시작/멈춤/끝 · 실시간 순위 · 결과.
//	③ 학생은 QR 을 찍어 / 로 들어와 학번·이름을 넣는다. 기다리는 동안은 연습, 교사가 시작하면 모두 함께 시작.
//
// 게임은 1인용이라 물리(행성 회전·포탄)는 학생 태블릿에서 계산하고, 서버는 판 시간·멈춤·점수·순위만 맡는다.
// 게임 파일(index.html)은 혼자 열어도(파일로 더블클릭) 동작하며, 서버가 내보낼 때만 접속 모드가 켜진다.
//
// 빌드:  ./build.sh  (또는 build.ps1)
package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

//go:embed games/planet-turret/index.html assets
var assetsFS embed.FS

const (
	preferredPort = 8091 // classroom-quiz(8080) · vector-soccer(8090) 와 동시에 켜도 겹치지 않게
	portTries     = 12
)

// 게임 파일의 이 표시를 바꿔 "서버 접속 모드"를 켠다(파일로 열면 혼자 하기 모드).
const (
	serverFlagOff = `<meta name="pt-server" content="0">`
	serverFlagOn  = `<meta name="pt-server" content="1">`
)

func main() {
	setupLogging()

	gamePage, err := fs.ReadFile(assetsFS, "games/planet-turret/index.html")
	if err != nil {
		log.Fatalf("게임 파일 읽기 실패: %v", err)
	}
	if !bytes.Contains(gamePage, []byte(serverFlagOff)) {
		log.Printf("경고: index.html 에 %s 표시가 없어 접속 모드가 켜지지 않습니다", serverFlagOff)
	}
	gamePage = bytes.Replace(gamePage, []byte(serverFlagOff), []byte(serverFlagOn), 1)

	primaryIP, candidates := detectLANIP()
	bindHost := "0.0.0.0"
	if os.Getenv("PT_LOCAL") != "" { // 교사 혼자 시험: 이 PC 에서만 접속(학생 태블릿은 못 들어옴)
		bindHost, primaryIP, candidates = "127.0.0.1", "127.0.0.1", []string{"127.0.0.1"}
		log.Printf("혼자 시험 모드: 이 PC 에서만 접속됩니다")
	}
	ln, port, err := listenLAN(bindHost, preferredPort)
	if err != nil {
		log.Fatalf("포트 열기 실패: %v", err)
	}
	defer ln.Close()

	hub := newHub()
	resumeFromEnv(hub)
	go hub.run()

	joinURL := fmt.Sprintf("http://%s:%d/", primaryIP, port)
	media := networkMedia(primaryIP)
	info := map[string]any{
		"joinURL": joinURL, "port": port, "primaryIP": primaryIP,
		"candidates": candidates, "media": media,
	}

	mux := http.NewServeMux()
	assetServer := http.FileServer(http.FS(assetsFS))
	mux.Handle("/assets/", assetServer)
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/assets/favicon.svg", http.StatusFound)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.NotFound(w, r)
			return
		}
		writeHTML(w, gamePage)
	})

	// 교사 화면 + 결과 API — 교사 PC(localhost) 전용.
	mux.HandleFunc("/host", localOnly(func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(assetsFS, "assets/host.html")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeHTML(w, b)
	}))
	mux.HandleFunc("GET /api/results", localOnly(handleListResults))
	mux.HandleFunc("GET /api/result.csv", localOnly(handleResultCSV))

	mux.HandleFunc("/qr.png", func(w http.ResponseWriter, r *http.Request) {
		size := 320
		if s := r.URL.Query().Get("size"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n >= 128 && n <= 1280 {
				size = n
			}
		}
		png, err := qrcode.Encode(joinURL, qrcode.Medium, size)
		if err != nil {
			http.Error(w, "qr error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(png)
	})

	mux.HandleFunc("/info", localOnly(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(info)
	}))

	// WebSocket: ?role=host(교사 PC 전용)  또는  ?name=..&sid=..&token=..
	mux.HandleFunc("/ws", hub.serveWS)

	log.Printf("학생접속 %s  교사화면 http://127.0.0.1:%d/host  (연결: %s)", joinURL, port, media)
	if len(candidates) > 1 {
		log.Printf("LAN IP 후보 %v (QR 가 %s 로 안 되면 다른 후보로 시도)", candidates, primaryIP)
	}

	if os.Getenv("PT_NO_BROWSER") == "" {
		go openBrowser(fmt.Sprintf("http://127.0.0.1:%d/host", port))
	}

	srv := &http.Server{Handler: mux}
	if err := srv.Serve(ln); err != nil {
		log.Printf("서버 종료: %v", err)
	}
}

func writeHTML(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}

// listenLAN 은 0.0.0.0(전체 인터페이스)에 바인딩한다 — 학생이 LAN 으로 붙으려면 필수.
func listenLAN(host string, start int) (net.Listener, int, error) {
	var lastErr error
	for p := start; p < start+portTries; p++ {
		ln, err := net.Listen("tcp", host+":"+strconv.Itoa(p))
		if err == nil {
			return ln, p, nil
		}
		lastErr = err
	}
	return nil, 0, lastErr
}

// detectLANIP 은 사설망 IPv4 를 골라 반환한다(우선순위 192.168 > 10 > 172.16-31).
func detectLANIP() (string, []string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1", []string{"127.0.0.1"}
	}
	var cands []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || !ip.IsPrivate() {
				continue
			}
			cands = append(cands, ip.String())
		}
	}
	if len(cands) == 0 {
		return "127.0.0.1", []string{"127.0.0.1"}
	}
	return pickPrimary(cands), cands
}

func pickPrimary(cands []string) string {
	rank := func(ip string) int {
		switch {
		case strings.HasPrefix(ip, "192.168."):
			return 0
		case strings.HasPrefix(ip, "10."):
			return 1
		default:
			return 2
		}
	}
	best := cands[0]
	for _, c := range cands[1:] {
		if rank(c) < rank(best) {
			best = c
		}
	}
	return best
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("브라우저 자동 열기 실패(수동으로 %s 접속): %v", url, err)
	}
}

// setupLogging 은 데이터 폴더에 celestial-cannon.log 를 남긴다 + 콘솔이 있으면 화면에도 출력.
func setupLogging() {
	log.SetFlags(log.LstdFlags)
	f, err := os.OpenFile(filepath.Join(dataDir(), "celestial-cannon.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
}

// dataDir 은 기록을 남길 폴더 — exe 옆. `go run` 처럼 임시 폴더에서 돌면 현재 폴더.
func dataDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	dir := filepath.Dir(exe)
	if tmp := os.TempDir(); tmp != "" && strings.HasPrefix(strings.ToLower(dir), strings.ToLower(tmp)) {
		return "."
	}
	return dir
}

// resumeFromEnv — 서버가 판 도중에 꺼졌을 때 같은 판을 이어 간다.
// PT_RESUME="판번호,분,끝나는시각(15:04:05)"  예: PT_RESUME="1,12,11:31:49"
// 학생 태블릿은 점수·남은 시간을 각자 들고 재접속하므로, 같은 판 번호와 원래 끝나는 시각으로 열면 점수가 다시 모인다.
func resumeFromEnv(h *Hub) {
	v := os.Getenv("PT_RESUME")
	if v == "" {
		return
	}
	parts := strings.Split(v, ",")
	if len(parts) != 3 {
		log.Printf("PT_RESUME 형식 오류: %q", v)
		return
	}
	round, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	hm, err3 := time.ParseInLocation("15:04:05", parts[2], time.Local)
	if err1 != nil || err2 != nil || err3 != nil || round < 1 || min < 1 {
		log.Printf("PT_RESUME 형식 오류: %q", v)
		return
	}
	now := time.Now()
	end := time.Date(now.Year(), now.Month(), now.Day(), hm.Hour(), hm.Minute(), hm.Second(), 0, time.Local)
	dur := time.Duration(min) * time.Minute
	h.sess = Session{State: "run", Round: round, Dur: dur, EndAt: end, StartAt: end.Add(-dur), Started: end.Add(-dur - countdown)}
	log.Printf("%d판 이어 가기: %s 까지 (남은 %s)", round, end.Format("15:04:05"), end.Sub(now).Round(time.Second))
}
