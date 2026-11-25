package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (f *fakeClock) now() time.Time      { return f.t }
func (f *fakeClock) add(d time.Duration) { f.t = f.t.Add(d) }
func testHub() (*Hub, *fakeClock, *[]*Result) {
	clk := &fakeClock{t: time.Date(2026, 11, 25, 10, 0, 0, 0, time.Local)}
	h := newHub()
	h.now = clk.now
	var saved []*Result
	h.save = func(r *Result) (string, error) { saved = append(saved, r); return "2026-11-25/100000-1판.json", nil }
	return h, clk, &saved
}

// join 은 연결 없이 학생을 등록한다(송신 채널만 있는 가짜 연결).
func join(h *Hub, token, sid, name string) (*Player, *client) {
	c := &client{hub: h, send: make(chan []byte, 256), token: token, sid: sid, name: name}
	h.onRegister(c)
	return h.players[token], c
}

func drain(c *client) []map[string]any {
	var out []map[string]any
	for {
		select {
		case b, ok := <-c.send:
			if !ok {
				return out
			}
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			out = append(out, m)
		default:
			return out
		}
	}
}

func tickFor(h *Hub, clk *fakeClock, d time.Duration) {
	for el := time.Duration(0); el < d; el += tickPeriod {
		clk.add(tickPeriod)
		h.onTick()
	}
}

func TestSessionLifecycle(t *testing.T) {
	h, clk, saved := testHub()
	a, _ := join(h, "ta", "10101", "가람")
	b, _ := join(h, "tb", "10102", "나래")

	// 연습 중 점수는 받지 않는다.
	if h.acceptProgress(a, 0, Progress{Score: 500}) {
		t.Fatal("idle 에서 점수를 받으면 안 된다")
	}

	h.startSession(10)
	if h.sess.State != "run" || h.sess.Round != 1 {
		t.Fatalf("시작 상태 %+v", h.sess)
	}
	if info := h.sessInfo(); info["cdMs"].(int64) != 3000 || info["remainMs"].(int64) != 600000 {
		t.Fatalf("카운트다운/남은 시간 %v", info)
	}
	if h.acceptProgress(a, 0, Progress{Score: 1}) {
		t.Fatal("지난 판 번호 점수는 버려야 한다")
	}
	h.acceptProgress(a, 1, Progress{Score: 1200, Shots: 10, Hits: 99})
	h.acceptProgress(b, 1, Progress{Score: 3400})
	if a.prog.Hits != 10 {
		t.Fatalf("명중은 발사 수를 넘을 수 없다: %d", a.prog.Hits)
	}

	// 멈춤: 시간이 흐르지 않는다.
	tickFor(h, clk, 63*time.Second) // 카운트다운 3초 + 60초
	h.pauseSession()
	left := h.sess.Left
	tickFor(h, clk, 5*time.Minute)
	if h.sess.State != "pause" || h.sess.Left != left {
		t.Fatalf("멈춘 동안 시간이 흘렀다 %+v", h.sess)
	}
	h.resumeSession()
	if got := h.sessInfo()["remainMs"].(int64); got != left.Milliseconds() {
		t.Fatalf("다시 시작 남은 시간 %d, 기대 %d", got, left.Milliseconds())
	}

	// 시간이 다 되면 end → 늦게 온 마지막 점수 받기 → 집계·저장.
	tickFor(h, clk, left+countdown)
	if h.sess.State != "end" {
		t.Fatalf("시간이 끝났는데 %s", h.sess.State)
	}
	if !h.acceptProgress(a, 1, Progress{Score: 5000}) {
		t.Fatal("집계 전 마지막 점수는 받아야 한다")
	}
	tickFor(h, clk, finalGrace+tickPeriod)
	if h.sess.Final == nil || len(*saved) != 1 {
		t.Fatal("집계·저장이 안 됐다")
	}
	if h.acceptProgress(b, 1, Progress{Score: 99999}) {
		t.Fatal("집계 뒤 점수는 받지 않는다")
	}
	rows := (*saved)[0].Rows
	if len(rows) != 2 || rows[0].Name != "가람" || rows[0].Rank != 1 || rows[1].Rank != 2 {
		t.Fatalf("순위 %+v", rows)
	}
	if r := h.resultFor(b); r == nil || r["rank"].(int) != 2 || r["n"].(int) != 2 {
		t.Fatalf("학생 결과 %v", r)
	}

	// 새 판: 바로 시작 가능, 지난 점수는 이번 판 순위에 없다.
	h.startSession(15)
	if h.sess.Round != 2 || len(h.ranking()) != 0 {
		t.Fatalf("새 판 %+v %v", h.sess, h.ranking())
	}
}

func TestReconnectRestoresProgress(t *testing.T) {
	h, _, _ := testHub()
	a, c1 := join(h, "t1", "10101", "가람")
	h.startSession(12)
	h.acceptProgress(a, 1, Progress{Score: 777, Lvl: 3})
	h.onUnregister(c1)
	if a.conn != nil {
		t.Fatal("끊김 처리")
	}
	// 브라우저를 닫고 새로 들어와도(새 토큰) 같은 학번·이름이면 같은 학생.
	a2, c2 := join(h, "t2", "10101", "가람")
	if a2 != a || len(h.order) != 1 {
		t.Fatal("같은 학생으로 복귀해야 한다")
	}
	msgs := drain(c2)
	if len(msgs) == 0 || msgs[0]["t"] != "hello" {
		t.Fatalf("hello 없음 %v", msgs)
	}
	prog, _ := msgs[0]["prog"].(map[string]any)
	if prog == nil || prog["score"].(float64) != 777 || prog["lvl"].(float64) != 3 {
		t.Fatalf("진행 복원 %v", msgs[0])
	}
	// 같은 이름으로 다른 기기에서 들어오면 앞 연결은 밀려난다.
	_, c3 := join(h, "t3", "10101", "가람")
	if a.conn != c3 {
		t.Fatal("새 연결로 교체")
	}
	kicked := false
	for _, m := range drain(c2) {
		if m["t"] == "kicked" {
			kicked = true
		}
	}
	if !kicked {
		t.Fatal("앞 연결에 kicked 를 보내야 한다")
	}
}

func TestResetDropsOffline(t *testing.T) {
	h, clk, _ := testHub()
	join(h, "t1", "", "온라인")
	_, c := join(h, "t2", "", "나감")
	h.onUnregister(c)
	h.startSession(5)
	h.stopSession()
	h.resetSession() // 집계 전에는 무시
	if h.sess.State != "end" {
		t.Fatal("집계 전 reset 은 무시")
	}
	tickFor(h, clk, finalGrace+tickPeriod)
	h.resetSession()
	if h.sess.State != "idle" || len(h.order) != 1 || h.order[0].name != "온라인" {
		t.Fatalf("reset %+v %d", h.sess.State, len(h.order))
	}
}

func TestCSV(t *testing.T) {
	r := &Result{Round: 1, Rows: []Row{{Rank: 1, SID: "10101", Name: "가람", Progress: Progress{Score: 5000, Lvl: 2, Loop: 1, Seen: 63, Shots: 40, Hits: 30}}}}
	s := string(resultCSV(r))
	if !strings.HasPrefix(s, "\xef\xbb\xbf순위") || !strings.Contains(s, "1,10101,가람,5000,9,서울·동쪽,6,") || !strings.Contains(s, ",40,30,75") {
		t.Fatalf("CSV:\n%s", s)
	}
}
