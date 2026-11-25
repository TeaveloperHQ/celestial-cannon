package main

import (
	"crypto/rand"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

// 연결 · 판(세션) 진행 · 순위. 모든 상태 변경은 run() 단일 고루틴 안에서만 일어나므로 락이 없다
// (vector-soccer 와 같은 액터 모델).
//
// 판의 흐름:  idle(연습) → run(카운트다운 3초 뒤 시작) ⇄ pause → end(집계 2초) → 결과 저장 → idle 또는 바로 새 판.

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10

	tickPeriod = 250 * time.Millisecond
	hostEvery  = 2  // 틱 2번(0.5초)마다 교사 화면 갱신
	rankEvery  = 8  // 2초마다 학생에게 현재 순위
	syncEvery  = 20 // 5초마다 남은 시간 다시 맞추기

	countdown  = 3 * time.Second
	finalGrace = 2 * time.Second // 끝난 뒤 마지막 점수가 도착할 여유

	minMinutes = 3
	maxMinutes = 30
	maxScore   = 99_999_999
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true }, // 로컬 교실 전용
}

type client struct {
	hub   *Hub
	conn  *websocket.Conn
	send  chan []byte
	host  bool
	token string
	sid   string
	name  string
	done  bool // send 를 이미 닫았나(두 번 닫으면 서버가 죽는다)
}

func (c *client) closeSend() {
	if !c.done {
		c.done = true
		close(c.send)
	}
}

type inMsg struct {
	c    *client
	data []byte
}

// Progress 는 학생 게임이 1초마다 보내는 진행 상황.
type Progress struct {
	Score  int `json:"score"`
	Lvl    int `json:"lvl"`    // 장소(레벨) 번호 0..5
	Loop   int `json:"loop"`   // 6곳을 몇 바퀴 돌았나
	Kills  int `json:"kills"`  // 악당 격파
	Bosses int `json:"bosses"` // 빌런 격파
	Breaks int `json:"breaks"` // 포탑이 부서진 횟수
	Shots  int `json:"shots"`
	Hits   int `json:"hits"`
	Seen   int `json:"seen"` // 가 본 장소 비트마스크
}

func (p *Progress) clamp() {
	lim := func(v *int, max int) {
		if *v < 0 {
			*v = 0
		}
		if *v > max {
			*v = max
		}
	}
	lim(&p.Score, maxScore)
	lim(&p.Lvl, 5)
	lim(&p.Loop, 99)
	lim(&p.Kills, 100000)
	lim(&p.Bosses, 10000)
	lim(&p.Breaks, 10000)
	lim(&p.Shots, 1000000)
	lim(&p.Hits, p.Shots)
	lim(&p.Seen, 63)
}

// Player 는 학생 한 명. 연결(client)과 분리되어 있어 재접속하면 conn 만 갈아낀다.
type Player struct {
	id    string
	token string
	sid   string
	name  string
	conn  *client

	prog  Progress
	round int // prog 가 속한 판 번호(0 = 이번 판에 아직 점수 없음)
}

func (p *Player) key() string { return p.sid + "|" + p.name }

func (p *Player) label() string {
	if p.sid != "" {
		return p.sid + " " + p.name
	}
	return p.name
}

type Session struct {
	State   string // idle | run | pause | end
	Round   int
	Dur     time.Duration
	StartAt time.Time     // 카운트다운이 끝나는 시각(run)
	EndAt   time.Time     // 끝나는 시각(run)
	Left    time.Duration // 남은 시간(pause)
	Started time.Time     // 시작 버튼을 누른 시각(기록용)
	Ended   time.Time
	Final   []Row // 집계가 끝난 순위(end 후 finalGrace 가 지나면)
	SavedAs string
}

type Hub struct {
	register   chan *client
	unregister chan *client
	inbound    chan inMsg

	hosts   map[*client]bool
	players map[string]*Player // 토큰 → 학생
	order   []*Player          // 들어온 순서

	sess Session
	tick int
	now  func() time.Time
	save func(*Result) (string, error) // 결과 저장(테스트에서 교체)
}

func newHub() *Hub {
	return &Hub{
		register:   make(chan *client),
		unregister: make(chan *client),
		inbound:    make(chan inMsg, 256),
		hosts:      map[*client]bool{},
		players:    map[string]*Player{},
		sess:       Session{State: "idle", Dur: 5 * time.Minute},
		now:        time.Now,
		save:       saveResult,
	}
}

func (h *Hub) run() {
	t := time.NewTicker(tickPeriod)
	defer t.Stop()
	for {
		select {
		case c := <-h.register:
			h.onRegister(c)
		case c := <-h.unregister:
			h.onUnregister(c)
		case m := <-h.inbound:
			h.onMessage(m)
		case <-t.C:
			h.onTick()
		}
	}
}

// ── 연결 ────────────────────────────────────────────────────

func (h *Hub) onRegister(c *client) {
	if c.host {
		h.hosts[c] = true
		h.sendTo(c, h.hostMsg())
		return
	}
	p := h.players[c.token]
	if p == nil {
		// 새 탭·새 기기라도 같은 학번·이름이면 같은 학생(하던 점수로 복귀).
		key := c.sid + "|" + c.name
		for _, q := range h.order {
			if q.key() == key {
				p = q
				break
			}
		}
		if p != nil {
			delete(h.players, p.token)
			p.token = c.token
			h.players[c.token] = p
		}
	}
	if p != nil && p.conn != nil && p.conn != c {
		h.sendTo(p.conn, mustJSON(map[string]string{"t": "kicked"}))
		p.conn.closeSend()
		p.conn = nil
	}
	if p == nil {
		p = &Player{id: newID(), token: c.token, sid: c.sid, name: c.name}
		h.players[c.token] = p
		h.order = append(h.order, p)
		log.Printf("학생 입장: %s", p.label())
	} else {
		log.Printf("학생 다시 접속: %s", p.label())
	}
	p.conn = c
	hello := map[string]any{
		"t":    "hello",
		"me":   map[string]string{"name": p.name, "sid": p.sid},
		"sess": h.sessInfo(),
	}
	if p.round == h.sess.Round && h.sess.State != "idle" {
		hello["prog"] = p.prog
	}
	if r := h.resultFor(p); r != nil {
		hello["result"] = r
	}
	h.sendP(p, hello)
	h.broadcastHost()
}

func (h *Hub) onUnregister(c *client) {
	if c.host {
		if h.hosts[c] {
			delete(h.hosts, c)
			c.closeSend()
		}
		return
	}
	p, ok := h.players[c.token]
	if !ok || p.conn != c { // 이미 새 연결로 교체됨(교체할 때 send 를 닫았다)
		if !ok {
			c.closeSend()
		}
		return
	}
	c.closeSend()
	p.conn = nil
	log.Printf("학생 연결 끊김: %s", p.label())
	h.broadcastHost()
}

func (h *Hub) sendTo(c *client, b []byte) {
	if c == nil || c.done {
		return
	}
	select {
	case c.send <- b:
	default: // 느린 연결은 건너뛴다(다음 갱신이 전체를 다시 보냄)
	}
}

func (h *Hub) sendP(p *Player, v any) {
	if p != nil && p.conn != nil {
		h.sendTo(p.conn, mustJSON(v))
	}
}

func (h *Hub) broadcastStudents(v any) {
	b := mustJSON(v)
	for _, p := range h.order {
		if p.conn != nil {
			h.sendTo(p.conn, b)
		}
	}
}

func (h *Hub) broadcastHost() {
	if len(h.hosts) == 0 {
		return
	}
	b := h.hostMsg()
	for c := range h.hosts {
		h.sendTo(c, b)
	}
}

// ── 메시지 ──────────────────────────────────────────────────

type clientMsg struct {
	T     string `json:"t"`
	Round int    `json:"round"`
	Min   int    `json:"min"`
	ID    string `json:"id"`
	Progress
}

func (h *Hub) onMessage(in inMsg) {
	var msg clientMsg
	if json.Unmarshal(in.data, &msg) != nil {
		return
	}
	if in.c.host {
		h.onHostMessage(msg)
		return
	}
	p, ok := h.players[in.c.token]
	if !ok || p.conn != in.c {
		return
	}
	if msg.T == "prog" {
		h.acceptProgress(p, msg.Round, msg.Progress)
	}
}

// acceptProgress 는 이번 판의 점수만 받는다(연습 점수·지난 판 점수는 버린다).
// 시간이 끝난 직후 도착한 마지막 점수는 집계 전(finalGrace)까지 받는다.
func (h *Hub) acceptProgress(p *Player, round int, pr Progress) bool {
	s := &h.sess
	if round != s.Round || s.Round == 0 {
		return false
	}
	switch {
	case s.State == "run" || s.State == "pause":
	case s.State == "end" && s.Final == nil:
	default:
		return false
	}
	pr.clamp()
	p.prog = pr
	p.round = s.Round
	return true
}

func (h *Hub) onHostMessage(msg clientMsg) {
	switch msg.T {
	case "start":
		h.startSession(msg.Min)
	case "pause":
		h.pauseSession()
	case "resume":
		h.resumeSession()
	case "stop":
		h.stopSession()
	case "reset":
		h.resetSession()
	case "remove":
		h.removePlayer(msg.ID)
	}
	h.broadcastHost()
}

func (h *Hub) startSession(min int) {
	s := &h.sess
	if s.State == "run" || s.State == "pause" {
		return
	}
	if s.State == "end" && s.Final == nil {
		return // 집계 중
	}
	if min < minMinutes || min > maxMinutes {
		min = 5
	}
	now := h.now()
	h.sess = Session{
		State:   "run",
		Round:   s.Round + 1,
		Dur:     time.Duration(min) * time.Minute,
		Started: now,
		StartAt: now.Add(countdown),
	}
	h.sess.EndAt = h.sess.StartAt.Add(h.sess.Dur)
	log.Printf("%d판 시작 (%d분)", h.sess.Round, min)
	h.broadcastSess()
}

func (h *Hub) pauseSession() {
	s := &h.sess
	if s.State != "run" {
		return
	}
	now := h.now()
	if now.Before(s.StartAt) {
		s.Left = s.Dur // 카운트다운 중에 멈추면 처음부터
	} else {
		s.Left = s.EndAt.Sub(now)
	}
	s.State = "pause"
	h.broadcastSess()
}

func (h *Hub) resumeSession() {
	s := &h.sess
	if s.State != "pause" {
		return
	}
	now := h.now()
	s.State = "run"
	s.StartAt = now.Add(countdown) // 다시 시작할 때도 3초 카운트다운
	s.EndAt = s.StartAt.Add(s.Left)
	h.broadcastSess()
}

func (h *Hub) stopSession() {
	s := &h.sess
	if s.State != "run" && s.State != "pause" {
		return
	}
	s.State = "end"
	s.Ended = h.now()
	log.Printf("%d판 끝", s.Round)
	h.broadcastSess()
}

func (h *Hub) resetSession() {
	s := &h.sess
	if s.State == "run" || s.State == "pause" || (s.State == "end" && s.Final == nil) {
		return
	}
	s.State = "idle"
	s.Final = nil
	s.SavedAs = ""
	// 연결이 끊긴 학생은 명단에서 정리한다.
	kept := h.order[:0]
	for _, p := range h.order {
		if p.conn == nil {
			delete(h.players, p.token)
			continue
		}
		kept = append(kept, p)
	}
	h.order = kept
	h.broadcastSess()
}

// removePlayer — 잘못 입력한 이름 등, 연결이 끊긴 학생을 교사가 명단에서 지운다.
func (h *Hub) removePlayer(id string) {
	for i, p := range h.order {
		if p.id == id && p.conn == nil {
			delete(h.players, p.token)
			h.order = append(h.order[:i], h.order[i+1:]...)
			return
		}
	}
}

// ── 틱 ──────────────────────────────────────────────────────

func (h *Hub) onTick() {
	h.tick++
	s := &h.sess
	now := h.now()
	if s.State == "run" && !now.Before(s.EndAt) {
		h.stopSession()
	}
	if s.State == "end" && s.Final == nil && now.Sub(s.Ended) >= finalGrace {
		h.finalize()
	}
	if s.State == "run" {
		if h.tick%rankEvery == 0 {
			h.sendRanks()
		}
		if h.tick%syncEvery == 0 {
			h.broadcastSess()
		}
	}
	if h.tick%hostEvery == 0 {
		h.broadcastHost()
	}
}

func (h *Hub) finalize() {
	s := &h.sess
	s.Final = h.ranking()
	res := &Result{
		Round:   s.Round,
		Started: s.Started,
		Ended:   s.Ended,
		Minutes: int(s.Dur / time.Minute),
		Rows:    s.Final,
	}
	if name, err := h.save(res); err != nil {
		log.Printf("결과 저장 실패: %v", err)
	} else {
		s.SavedAs = name
		log.Printf("%d판 결과 저장: %s (%d명)", s.Round, name, len(s.Final))
	}
	for _, p := range h.order {
		if r := h.resultFor(p); r != nil {
			h.sendP(p, r)
		}
	}
	h.broadcastHost()
}

// ranking 은 이번 판에 점수를 낸 학생만 줄 세운다: 점수 ↓, 부서진 횟수 ↑, 명중 ↓.
func (h *Hub) ranking() []Row {
	rows := []Row{} // nil 이 아니어야 한다 — sess.Final != nil 이 "집계 끝" 표시
	for _, p := range h.order {
		if p.round != h.sess.Round || h.sess.Round == 0 {
			continue
		}
		rows = append(rows, Row{ID: p.id, SID: p.sid, Name: p.name, Progress: p.prog})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Breaks != b.Breaks {
			return a.Breaks < b.Breaks
		}
		return a.Hits > b.Hits
	})
	for i := range rows {
		rows[i].Rank = i + 1
		if i > 0 && rows[i].Score == rows[i-1].Score && rows[i].Breaks == rows[i-1].Breaks && rows[i].Hits == rows[i-1].Hits {
			rows[i].Rank = rows[i-1].Rank
		}
	}
	return rows
}

func (h *Hub) sendRanks() {
	rows := h.ranking()
	n := len(rows)
	for _, r := range rows {
		for _, p := range h.order {
			if p.id == r.ID {
				h.sendP(p, map[string]any{"t": "rk", "r": r.Rank, "n": n})
				break
			}
		}
	}
}

type topEntry struct {
	Name  string `json:"name"`
	Score int    `json:"score"`
	Rank  int    `json:"rank"`
}

// resultFor 는 집계가 끝난 판에서 그 학생의 결과(없으면 nil).
func (h *Hub) resultFor(p *Player) map[string]any {
	s := &h.sess
	if s.State != "end" || s.Final == nil {
		return nil
	}
	top := []topEntry{}
	for i, r := range s.Final {
		if i >= 5 {
			break
		}
		top = append(top, topEntry{Name: r.Name, Score: r.Score, Rank: r.Rank})
	}
	for _, r := range s.Final {
		if r.ID == p.id {
			return map[string]any{"t": "result", "round": s.Round, "rank": r.Rank, "n": len(s.Final), "score": r.Score, "top": top}
		}
	}
	return nil
}

// ── 송신 메시지 ─────────────────────────────────────────────

func (h *Hub) sessInfo() map[string]any {
	s := &h.sess
	now := h.now()
	var remain, cd time.Duration
	switch s.State {
	case "run":
		remain = s.EndAt.Sub(now)
		if now.Before(s.StartAt) { // 카운트다운 동안은 게임 시간이 줄지 않는다
			cd = s.StartAt.Sub(now)
			remain = s.EndAt.Sub(s.StartAt)
		}
	case "pause":
		remain = s.Left
	}
	if remain < 0 {
		remain = 0
	}
	return map[string]any{
		"state": s.State, "round": s.Round,
		"remainMs": remain.Milliseconds(), "cdMs": cd.Milliseconds(), "durMs": s.Dur.Milliseconds(),
	}
}

func (h *Hub) broadcastSess() {
	h.broadcastStudents(map[string]any{"t": "sess", "sess": h.sessInfo()})
}

type hostPlayer struct {
	ID     string `json:"id"`
	SID    string `json:"sid"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
	Played bool   `json:"played"`
	Rank   int    `json:"rank"`
	Progress
}

func (h *Hub) hostMsg() []byte {
	rows := h.ranking()
	if h.sess.Final != nil {
		rows = h.sess.Final
	}
	rank := map[string]int{}
	for _, r := range rows {
		rank[r.ID] = r.Rank
	}
	list := make([]hostPlayer, 0, len(h.order))
	for _, p := range h.order {
		hp := hostPlayer{ID: p.id, SID: p.sid, Name: p.name, Online: p.conn != nil}
		if r, ok := rank[p.id]; ok {
			hp.Played, hp.Rank, hp.Progress = true, r, p.prog
		}
		list = append(list, hp)
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Played != b.Played {
			return a.Played
		}
		if a.Played {
			return a.Rank < b.Rank
		}
		return a.SID+a.Name < b.SID+b.Name
	})
	info := h.sessInfo()
	info["saved"] = h.sess.SavedAs
	info["final"] = h.sess.Final != nil
	return mustJSON(map[string]any{"t": "host", "sess": info, "players": list})
}

// ── WebSocket ───────────────────────────────────────────────

// serveWS — ?role=host  또는  ?name=..&sid=..&token=..
func (h *Hub) serveWS(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c := &client{hub: h, send: make(chan []byte, 64)}
	if q.Get("role") == "host" {
		if !isLoopback(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		c.host = true
	} else {
		c.name = cleanText(q.Get("name"), 12)
		c.sid = cleanText(q.Get("sid"), 8)
		c.token = cleanText(q.Get("token"), 40)
		if c.name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		if c.token == "" {
			c.token = newID()
		}
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WS 업그레이드 실패: %v", err)
		return
	}
	c.conn = conn
	h.register <- c
	go c.writePump()
	go c.readPump()
}

func (c *client) readPump() {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(2048)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		c.hub.inbound <- inMsg{c: c, data: msg}
	}
}

func (c *client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ── 유틸 ────────────────────────────────────────────────────

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("JSON 인코딩 실패: %v", err)
		return []byte(`{}`)
	}
	return b
}

// cleanText 는 앞뒤 공백·제어문자를 없애고 글자 수를 제한한다.
func cleanText(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	const hexd = "0123456789abcdef"
	out := make([]byte, 12)
	for i, v := range b {
		out[i*2] = hexd[v>>4]
		out[i*2+1] = hexd[v&0x0f]
	}
	return string(out)
}
