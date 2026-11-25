// mksyso — app.ico 를 exe 아이콘 리소스(rsrc_windows_amd64.syso)로 만든다. 외부 도구(rsrc) 없이 표준 라이브러리만 쓴다.
//
//	go run ./branding/mksyso branding/app.ico rsrc_windows_amd64.syso
//
// .syso 는 .rsrc 섹션 하나를 가진 COFF 오브젝트다. go build 가 같은 폴더의 *.syso 를 자동으로 링크해
// exe 에 아이콘이 붙는다(GOOS=windows 일 때만).
//
// 리소스 트리: 루트(종류) → RT_ICON(3) / RT_GROUP_ICON(14) → ID → 언어(0) → 데이터 항목.
// 데이터 항목의 주소(RVA)는 링커가 채우도록 재배치(IMAGE_REL_AMD64_ADDR32NB)를 단다.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

const (
	rtIcon      = 3
	rtGroupIcon = 14
	relAddr32NB = 0x0003 // IMAGE_REL_AMD64_ADDR32NB
)

type iconImage struct {
	w, h, colors, planes, bpp int
	data                      []byte
}

func readICO(path string) ([]iconImage, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 6 || binary.LittleEndian.Uint16(b[2:]) != 1 {
		return nil, fmt.Errorf("ico 파일이 아닙니다")
	}
	n := int(binary.LittleEndian.Uint16(b[4:]))
	var out []iconImage
	for i := 0; i < n; i++ {
		e := b[6+16*i:]
		size := int(binary.LittleEndian.Uint32(e[8:]))
		off := int(binary.LittleEndian.Uint32(e[12:]))
		if off+size > len(b) {
			return nil, fmt.Errorf("ico 항목 %d 이 잘렸습니다", i)
		}
		out = append(out, iconImage{
			w: int(e[0]), h: int(e[1]), colors: int(e[2]),
			planes: int(binary.LittleEndian.Uint16(e[4:])), bpp: int(binary.LittleEndian.Uint16(e[6:])),
			data: b[off : off+size],
		})
	}
	return out, nil
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "사용법: go run ./branding/mksyso <app.ico> <rsrc_windows_amd64.syso>")
		os.Exit(2)
	}
	icons, err := readICO(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// 그룹 아이콘(GRPICONDIR): 어떤 크기의 RT_ICON 이 몇 번 ID 인지.
	var grp bytes.Buffer
	le := func(w *bytes.Buffer, v ...any) {
		for _, x := range v {
			_ = binary.Write(w, binary.LittleEndian, x)
		}
	}
	le(&grp, uint16(0), uint16(1), uint16(len(icons)))
	for i, ic := range icons {
		le(&grp, uint8(ic.w), uint8(ic.h), uint8(ic.colors), uint8(0), uint16(ic.planes), uint16(ic.bpp), uint32(len(ic.data)), uint16(i+1))
	}

	// 리소스 목록: (종류, ID, 데이터)
	type res struct {
		typ, id int
		data    []byte
	}
	var all []res
	for i, ic := range icons {
		all = append(all, res{rtIcon, i + 1, ic.data})
	}
	all = append(all, res{rtGroupIcon, 1, grp.Bytes()})

	// 크기 계산: 디렉터리 = 16 + 8*항목 수.
	dirSize := func(n int) int { return 16 + 8*n }
	rootOff := 0
	iconDirOff := rootOff + dirSize(2)
	groupDirOff := iconDirOff + dirSize(len(icons))
	langOff := groupDirOff + dirSize(1) // 리소스마다 언어 디렉터리 하나
	entryOff := langOff + dirSize(1)*len(all)
	dataOff := entryOff + 16*len(all)

	align := func(n int) int { return (n + 7) &^ 7 }
	dataPos := make([]int, len(all))
	pos := align(dataOff)
	for i, r := range all {
		dataPos[i] = pos
		pos = align(pos + len(r.data))
	}
	raw := make([]byte, pos)
	put16 := func(at, v int) { binary.LittleEndian.PutUint16(raw[at:], uint16(v)) }
	put32 := func(at int, v uint32) { binary.LittleEndian.PutUint32(raw[at:], v) }
	dirHeader := func(at, nID int) { put16(at+14, nID) } // 특성·시각·버전 0, 이름 항목 0
	const sub = 0x80000000

	// 루트: 종류 두 개(ID 오름차순)
	dirHeader(rootOff, 2)
	put32(rootOff+16, rtIcon)
	put32(rootOff+20, sub|uint32(iconDirOff))
	put32(rootOff+24, rtGroupIcon)
	put32(rootOff+28, sub|uint32(groupDirOff))

	// 종류별 ID 디렉터리 → 언어 디렉터리
	dirHeader(iconDirOff, len(icons))
	for i := range icons {
		put32(iconDirOff+16+8*i, uint32(i+1))
		put32(iconDirOff+20+8*i, sub|uint32(langOff+dirSize(1)*i))
	}
	dirHeader(groupDirOff, 1)
	put32(groupDirOff+16, 1)
	put32(groupDirOff+20, sub|uint32(langOff+dirSize(1)*len(icons)))

	// 언어 디렉터리(언어 0 = 중립) → 데이터 항목
	var relocs []int // 재배치가 필요한 위치(데이터 항목의 OffsetToData)
	for i, r := range all {
		lo := langOff + dirSize(1)*i
		dirHeader(lo, 1)
		put32(lo+16, 0)
		put32(lo+20, uint32(entryOff+16*i))
		eo := entryOff + 16*i
		put32(eo, uint32(dataPos[i])) // 섹션 안 위치(링커가 섹션 RVA 를 더한다)
		put32(eo+4, uint32(len(r.data)))
		relocs = append(relocs, eo)
		copy(raw[dataPos[i]:], r.data)
	}

	// COFF 파일: 헤더(20) + 섹션 헤더(40) + 원본 데이터 + 재배치(10*n) + 심볼(18) + 문자열 표(4)
	var out bytes.Buffer
	rawPtr := 20 + 40
	relPtr := rawPtr + len(raw)
	symPtr := relPtr + 10*len(relocs)
	le(&out, uint16(0x8664), uint16(1), uint32(0), uint32(symPtr), uint32(1), uint16(0), uint16(0x0004))
	out.WriteString(".rsrc\x00\x00\x00")
	le(&out, uint32(0), uint32(0), uint32(len(raw)), uint32(rawPtr), uint32(relPtr), uint32(0), uint16(len(relocs)), uint16(0), uint32(0x40000040))
	out.Write(raw)
	for _, at := range relocs {
		le(&out, uint32(at), uint32(0), uint16(relAddr32NB))
	}
	out.WriteString(".rsrc\x00\x00\x00")
	le(&out, uint32(0), int16(1), uint16(0), uint8(3), uint8(0)) // 값 0 · 섹션 1 · 정적(3)
	le(&out, uint32(4))                                          // 빈 문자열 표

	if err := os.WriteFile(os.Args[2], out.Bytes(), 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes, 아이콘 %d개)\n", os.Args[2], out.Len(), len(icons))
}
