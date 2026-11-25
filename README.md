# celestial-cannon

중학교 1학년 과학 **일주운동·연주운동** 단원 수업 자료 프로젝트.
핵심 목표: 학생이 "구면(자전하는 행성) 위의 관찰자가 무한히 먼 곳에 정지한 대상을 어떻게 **겉보기 운동**으로 인식하는가"를 게임하면서 자연스럽게 체득하게 한다. 특히 **중위도(서울 37.5°)** 일주운동의 이해가 목표다.

## 구성

| 경로 | 내용 | 상태 |
|---|---|---|
| `games/planet-turret/index.html` | **행성 포탑 방어전**: 자전하는 행성 위 포탑에서 악당·빌런을 대공포로 쏘는 아케이드 게임. 레벨마다 위도와 보는 방향이 바뀜 | 메인, 개발 중 |
| `games/planet-turret/*.go` | **교실 서버**: QR 접속 · 3·5·8분 판 · 실시간 순위 · 결과 엑셀 (vector-soccer 와 같은 구조). 선생님용 [MANUAL.md](games/planet-turret/MANUAL.md) | 메인 |
| `prototypes/my-stellarium/index.html` | **나만의 스텔라리움**: 빈칸 3개(회전축·방향·각도)를 골라 하늘을 움직이는 수업 도구 | 보류된 시안 |
| `docs/research.md` | 활동·연구 조사 결과와 출처 | 참고 |
| `docs/lesson-design.md` | 중위도 일주운동 지도 방법, 수업안, 검토했던 아이디어들 | 참고 |
| `CLAUDE.md` | 이어서 개발할 때 필요한 설계 결정·좌표계·규칙 | 개발용 |

## 실행

**교실 전체가 함께 할 때**: `games/planet-turret/build.sh` 로 `dist/planet-turret.exe` 를 만들어 교사 PC에서 실행합니다(Go 1.26+). 개발 중에는 `PT_NO_BROWSER=1 go run .`, 테스트는 `go test ./...`. 기본 포트 8091.

**혼자 해 볼 때**: 빌드 과정이 없습니다. 각 `index.html` 파일을 브라우저로 열면 바로 실행됩니다(이때는 서버 없이 예전처럼 생명이 다 닳으면 끝나는 모드). 외부에서 불러오는 것은 Google Fonts 하나뿐이고, 인터넷이 없으면 시스템 글꼴로 대체됩니다.

VS Code에서는 Live Server 같은 확장으로 열면 편합니다. 태블릿에서 시험하려면 같은 와이파이에서 PC의 로컬 서버로 접속하면 됩니다.

## 온라인 사본 (claude.ai Artifact, 비공개)

- 행성 포탑 방어전: https://claude.ai/artifact/DhsKSWtxvbZoowJ1Y5gKoZ
- 나만의 스텔라리움: https://claude.ai/artifact/E9BBkRzot8AXJteKjXuNa9

이 폴더의 파일이 원본입니다. 위 사본은 2026-11-25 시점 버전이고, 자동으로 동기화되지 않습니다.
