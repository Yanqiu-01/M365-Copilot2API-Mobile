// Package arm64 提供针对 Go 编译产物的轻量 arm64 分析：
// 恢复 ADRP+ADD/LDR 组成的地址计算，提取字符串常量与立即数。
//
// 这不是通用反汇编器，只覆盖 Go 代码生成中稳定出现的少数模式，
// 目的是在符号表被 strip 后仍能取到字节级证据。
package arm64

import (
	"encoding/binary"
	"fmt"
	"sort"
)

// Reader 提供按虚拟地址读内存的能力。
type Reader interface {
	ReadVA(addr, n uint64) []byte
	CString(addr uint64, max uint64) string
}

// Const 是从函数体中恢复出的一个常量。
type Const struct {
	Off  int    // 相对函数入口的字节偏移
	Kind string // "imm" | "str" | "addr" | "fimm"
	Imm  uint64
	Str  string
	Note string
}

// StrRef 是一个字符串字面量引用：Go 用 ADRP+ADD 得到指针，
// 紧邻的 MOVZ/MOVK 或 ADD 给出长度。
type StrRef struct {
	Off  int
	Addr uint64
	Len  int
	Val  string
}

type regState struct {
	val   uint64
	known bool
	// 该寄存器是否由 ADRP 产生（页基址）
	fromADRP bool
}

// Analyze 扫描一段函数机器码，恢复常量与字符串引用。
// entry 是该段代码的虚拟地址。
func Analyze(code []byte, entry uint64, r Reader) (consts []Const, strs []StrRef) {
	var regs [32]regState
	// 记录最近一次算出的地址寄存器，便于把 ADD 得到的指针与随后的长度配对
	type pending struct {
		off  int
		addr uint64
	}
	var lastPtr *pending

	for i := 0; i+4 <= len(code); i += 4 {
		w := binary.LittleEndian.Uint32(code[i:])
		pc := entry + uint64(i)

		switch {
		case isADRP(w):
			rd := int(w & 0x1f)
			imm := adrpImm(w)
			base := (pc & ^uint64(0xfff)) + uint64(int64(imm))
			regs[rd] = regState{val: base, known: true, fromADRP: true}

		case isADDimm(w):
			rd := int(w & 0x1f)
			rn := int((w >> 5) & 0x1f)
			imm12 := uint64((w >> 10) & 0xfff)
			if (w>>22)&1 == 1 {
				imm12 <<= 12
			}
			if regs[rn].known {
				v := regs[rn].val + imm12
				regs[rd] = regState{val: v, known: true, fromADRP: regs[rn].fromADRP}
				if regs[rn].fromADRP {
					// 可能是字符串/符号地址
					lastPtr = &pending{off: i, addr: v}
					consts = append(consts, Const{
						Off: i, Kind: "addr", Imm: v,
						Note: fmt.Sprintf("ADRP+ADD -> 0x%x", v),
					})
				}
			} else {
				regs[rd] = regState{}
			}

		case isMOVZ(w):
			rd := int(w & 0x1f)
			imm := uint64((w >> 5) & 0xffff)
			sh := ((w >> 21) & 0x3) * 16
			v := imm << sh
			regs[rd] = regState{val: v, known: true}
			if imm != 0 {
				consts = append(consts, Const{Off: i, Kind: "imm", Imm: v,
					Note: fmt.Sprintf("MOVZ #%d<<%d", imm, sh)})
			}
			// 字符串长度常紧跟指针
			if lastPtr != nil && i-lastPtr.off <= 24 && v > 0 && v < 1<<16 {
				if s := r.CString(lastPtr.addr, v+1); len(s) >= int(v) && v > 0 {
					strs = append(strs, StrRef{Off: lastPtr.off, Addr: lastPtr.addr,
						Len: int(v), Val: s[:v]})
					lastPtr = nil
				}
			}

		case isMOVK(w):
			rd := int(w & 0x1f)
			imm := uint64((w >> 5) & 0xffff)
			sh := ((w >> 21) & 0x3) * 16
			if regs[rd].known {
				regs[rd].val = (regs[rd].val & ^(uint64(0xffff) << sh)) | (imm << sh)
				consts = append(consts, Const{Off: i, Kind: "imm", Imm: regs[rd].val,
					Note: fmt.Sprintf("MOVK -> %d", regs[rd].val)})
			}

		case isMOVN(w):
			rd := int(w & 0x1f)
			imm := uint64((w >> 5) & 0xffff)
			sh := ((w >> 21) & 0x3) * 16
			v := ^(imm << sh)
			if (w>>31)&1 == 0 {
				v &= 0xffffffff
			}
			regs[rd] = regState{val: v, known: true}
			consts = append(consts, Const{Off: i, Kind: "imm", Imm: v,
				Note: fmt.Sprintf("MOVN -> %d", int64(v))})

		case isCMPimm(w):
			imm12 := uint64((w >> 10) & 0xfff)
			if (w>>22)&1 == 1 {
				imm12 <<= 12
			}
			consts = append(consts, Const{Off: i, Kind: "imm", Imm: imm12,
				Note: fmt.Sprintf("CMP #%d", imm12)})

		case isLDRlit(w):
			// LDR (literal): 目标是 PC 相对的常量池，常用于浮点字面量
			imm19 := int64(int32((w>>5)&0x7ffff) << 13 >> 13)
			addr := pc + uint64(imm19*4)
			sz := uint64(4)
			if (w>>30)&1 == 1 {
				sz = 8
			}
			if b := r.ReadVA(addr, sz); b != nil {
				var v uint64
				if sz == 8 {
					v = binary.LittleEndian.Uint64(b)
				} else {
					v = uint64(binary.LittleEndian.Uint32(b))
				}
				consts = append(consts, Const{Off: i, Kind: "fimm", Imm: v,
					Note: fmt.Sprintf("LDR literal @0x%x", addr)})
			}
		}
	}
	sort.Slice(strs, func(a, b int) bool { return strs[a].Off < strs[b].Off })
	return
}

func isADRP(w uint32) bool   { return (w>>24)&0x9f == 0x90 }
func isADDimm(w uint32) bool { return (w>>24)&0x7f == 0x11 || (w>>24)&0x7f == 0x51 }
func isMOVZ(w uint32) bool   { return (w>>23)&0x1ff == 0x1a5 || (w>>23)&0x1ff == 0x0a5 }
func isMOVK(w uint32) bool   { return (w>>23)&0x1ff == 0x1e5 || (w>>23)&0x1ff == 0x0e5 }
func isMOVN(w uint32) bool   { return (w>>23)&0x1ff == 0x125 || (w>>23)&0x1ff == 0x025 }
func isCMPimm(w uint32) bool { return (w>>24)&0x7f == 0x71 || (w>>24)&0x7f == 0x31 }
func isLDRlit(w uint32) bool { return (w>>24)&0x3f == 0x18 }

func adrpImm(w uint32) int64 {
	immlo := int64((w >> 29) & 0x3)
	immhi := int64((w >> 5) & 0x7ffff)
	imm := (immhi << 2) | immlo
	// 21 位符号扩展，再左移 12
	imm = imm << 43 >> 43
	return imm << 12
}
