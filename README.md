<p align="center"><sub>배포용 exe 는 <b>teaveloper 포털에서 빌드</b>합니다(앱 이름 통일성). 이 레포는 소스입니다.</sub></p>

# celestial-cannon (행성 포탑 방어전)

중학교 1학년 과학 **일주운동** 단원 수업용 교실 게임.
같은 교내망의 학생들이 **QR 로 접속**해 함께 겨룬다. 교사 PC에서 **exe 한 개를 더블클릭**하면 끝이고, 학생은 앱 설치 없이 브라우저로 들어온다.

📖 **선생님용 사용법은 [사용 설명서(MANUAL.md)](MANUAL.md)** 를 보세요. 이 README 는 만드는 사람을 위한 설명입니다.

구조는 형제 프로젝트 [vector-soccer](https://github.com/TeaveloperHQ/vector-soccer) · teavel-submarine 과 같다(LAN 바인딩 · QR · 액터 모델 허브 · 결과 파일 저장).

## 교육 목표

"자전하는 구면 위의 관찰자에게, 무한히 먼 곳에 정지한 대상이 어떻게 **겉보기 운동**으로 보이는가."
특히 중위도(서울 37.5°)의 겉보기 운동 — 북: 북극성 중심 원, 동: 비스듬히 떠오름, 남: 낮은 호, 서: 비스듬히 짐 — 을
네 장의 사진을 외우는 대신 **하나의 기울어진 회전**으로 이해시킨다.

- 악당·빌런·배경 별·대표 별자리(북두칠성·카시오페이아·오리온·시리우스, 실제 적경·적위)는 모두 **무한히 먼 고정 방향**이다. 화면의 움직임은 전부 행성 회전 때문.
- 오른쪽 위 **우주에서 본 지구** 지도: 우주에 고정한 카메라 앞에서 지구가 서→동으로 실제로 돌고, 내 위치와 **지평면**이 함께 기운다.
  고정된 악당·별자리는 지평면 위/아래에 따라 밝게/흐리게 바뀐다 → "지구가 돌아 지평면이 기울면 별이 진다".

## 구성

| 경로 | 내용 |
|---|---|
| `games/planet-turret/index.html` | **게임**(단일 HTML, Canvas 2D, 외부 라이브러리 없음). 파일로 열면 혼자 하기, 서버가 내보내면 접속 모드 |
| `main.go` | LAN IP 탐지 · `0.0.0.0` 바인딩 · 라우팅 · QR · 브라우저 자동 열기 · 로그 · `PT_RESUME`/`PT_LOCAL` |
| `hub.go` | 연결 · 판 진행(idle → run ⇄ pause → end) · 남은 시간 · 순위 · 재접속 |
| `results.go` | 결과 저장(`celestial-cannon-results/<날짜>/`) · 목록 · CSV · 교사 전용 API |
| `netmedia*.go` | 학생 접속 주소가 무선/유선인지 판별(vector-soccer 와 동일) |
| `assets/` | 교사 화면(`host.html`) · 아이콘 · 엠블럼 |
| `prototypes/my-stellarium/` | 나만의 스텔라리움(보류된 시안) |
| `docs/` | 활동·연구 조사, 수업 설계 |
| `CLAUDE.md` | 설계 결정 · 좌표계 · 절대 깨면 안 되는 물리 규칙 |

## 빌드 / 실행

```bash
./build.sh                                # dist/celestial-cannon.exe (콘솔 창 = 실행 중 표시, 닫으면 종료)
./build.sh dist/celestial-cannon.exe gui  # 콘솔 없이
go test ./...                             # 판 진행·재접속·CSV 테스트
PT_NO_BROWSER=1 go run .                  # 개발 실행(브라우저 자동 열기 끔)
PT_LOCAL=1 go run .                       # 이 PC 에서만 접속(혼자 시험)
```

리눅스에서 윈도우 exe로 그대로 크로스컴파일된다(`CGO_ENABLED=0`). 기본 포트 **8091**
(classroom-quiz 8080 · vector-soccer 8090 과 동시에 켜도 겹치지 않게), 사용 중이면 다음 포트로 넘어간다.

게임만 혼자 해 보려면 `games/planet-turret/index.html` 을 브라우저로 바로 열면 된다(빌드 불필요).
외부에서 불러오는 것은 Google Fonts 하나뿐이고, 인터넷이 없으면 시스템 글꼴로 대체된다.

## 설계 메모

- **게임 물리는 학생 기기에서** 계산한다(1인 게임). 서버는 판 상태·남은 시간·점수·순위만 맡는다. 학생은 1초마다 진행 상황(`prog`)을 보낸다.
- 서버가 `index.html` 을 내보낼 때 `<meta name="pt-server" content="0">` 을 `1` 로 바꿔 접속 모드를 켠다.
- 모든 상태 변경은 `Hub.run()` 단일 고루틴 안에서만 일어난다(락 없음).
- 학생 식별: `sessionStorage` 토큰 + 학번·이름. 새 탭·새 기기라도 같은 학번·이름이면 같은 학생으로 복귀(하던 점수·장소).
- `/host`, `/info`, 결과 API, 교사용 WebSocket 은 `localOnly`(loopback)로 막는다.
- 결과 폴더·로그 이름에 게임 이름을 붙여, 여러 teaveloper 게임 exe 가 같은 폴더에 있어도 기록이 섞이지 않는다. 저장은 임시 파일 → 이름 바꾸기.

### WebSocket 프로토콜 (`/ws?name=..&sid=..&token=..`)

| 방향 | 메시지 |
|---|---|
| 학생 → 서버 | `prog{round,score,lvl,loop,kills,bosses,breaks,shots,hits,seen}` |
| 서버 → 학생 | `hello{me,sess,prog?,result?}` `sess{state,round,remainMs,cdMs,durMs}` `rk{r,n}` `result{rank,n,score,top}` `kicked` |
| 서버 → 교사 | `host{sess,players}` (2Hz) |
| 교사 → 서버 | `start{min}` `pause` `resume` `stop` `reset` `remove{id}` |

## 라이선스

MIT — [LICENSE](LICENSE) 참고.
