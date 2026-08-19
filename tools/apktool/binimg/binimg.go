// Package binimg 加载 Go 编译的 ELF 二进制，提供正确的 pclntab 解析与
// 虚拟地址读取能力。
//
// 关键点：gosym.NewLineTable 的第二个参数必须是 text 段起始地址，
// 而不是 pclntab 节自身的地址。传错会导致所有函数地址整体偏移，
// 表现为函数 Entry 落在 .text 范围之外、objdump 取不到代码。
// 真实 textStart 记录在 pclntab 头部，本包直接读取它。
package binimg

import (
	"debug/elf"
	"debug/gosym"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

// Image 是一个已解析的 Go ELF 镜像。
type Image struct {
	ELF       *elf.File
	Table     *gosym.Table
	TextStart uint64
	PclnAddr  uint64
	NFunc     uint64
	NFiles    uint64
	PtrSize   int
	Magic     uint32
	Version   string

	sections []*elf.Section // 按 Addr 排序，供地址查找
}

// Open 打开并解析二进制。
func Open(path string) (*Image, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, err
	}
	sec := findPclntab(f)
	if sec == nil {
		f.Close()
		return nil, fmt.Errorf("%s: no pclntab section", path)
	}
	data, err := sec.Data()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("read pclntab: %w", err)
	}
	if len(data) < 32 {
		f.Close()
		return nil, fmt.Errorf("pclntab too small: %d bytes", len(data))
	}

	img := &Image{
		ELF:      f,
		PclnAddr: sec.Addr,
		Magic:    binary.LittleEndian.Uint32(data[0:4]),
		PtrSize:  int(data[7]),
	}
	img.Version = versionName(img.Magic)

	ps := img.PtrSize
	if ps != 4 && ps != 8 {
		f.Close()
		return nil, fmt.Errorf("unexpected ptrSize %d", ps)
	}
	rd := func(off int) uint64 {
		if ps == 8 {
			return binary.LittleEndian.Uint64(data[off:])
		}
		return uint64(binary.LittleEndian.Uint32(data[off:]))
	}

	switch img.Magic {
	case 0xfffffff0, 0xfffffff1:
		// go1.20+：nfunc, nfiles, textStart, ...
		img.NFunc = rd(8)
		img.NFiles = rd(8 + ps)
		img.TextStart = rd(8 + 2*ps)
	case 0xfffffffa:
		// go1.18/1.19：nfunc, nfiles, textStart, ...
		img.NFunc = rd(8)
		img.NFiles = rd(8 + ps)
		img.TextStart = rd(8 + 2*ps)
	default:
		// 更老的格式没有 textStart 字段，退回 .text 节地址。
		if t := f.Section(".text"); t != nil {
			img.TextStart = t.Addr
		}
	}
	// 兜底与合理性校验：textStart 必须落在某个可执行节内。
	if !img.addrInSections(img.TextStart) {
		if t := f.Section(".text"); t != nil {
			img.TextStart = t.Addr
		}
	}

	tbl, err := gosym.NewTable(nil, gosym.NewLineTable(data, img.TextStart))
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("gosym: %w", err)
	}
	img.Table = tbl

	for _, s := range f.Sections {
		if s.Addr != 0 && s.Size != 0 && s.Type != elf.SHT_NOBITS {
			img.sections = append(img.sections, s)
		}
	}
	sort.Slice(img.sections, func(i, j int) bool {
		return img.sections[i].Addr < img.sections[j].Addr
	})
	return img, nil
}

func (img *Image) Close() error { return img.ELF.Close() }

func findPclntab(f *elf.File) *elf.Section {
	if s := f.Section(".gopclntab"); s != nil {
		return s
	}
	for _, s := range f.Sections {
		if strings.Contains(s.Name, "gopclntab") {
			return s
		}
	}
	for _, s := range f.Sections {
		if strings.Contains(s.Name, "pclntab") {
			return s
		}
	}
	return nil
}

func versionName(magic uint32) string {
	switch magic {
	case 0xfffffffb:
		return "go1.16/1.17 (ver1)"
	case 0xfffffffa:
		return "go1.18/1.19 (ver2)"
	case 0xfffffff0:
		return "go1.20+ (ver2)"
	case 0xfffffff1:
		return "go1.22+ (ver3)"
	}
	return fmt.Sprintf("unknown(0x%x)", magic)
}

func (img *Image) addrInSections(addr uint64) bool {
	for _, s := range img.ELF.Sections {
		if s.Addr == 0 || s.Size == 0 {
			continue
		}
		if addr >= s.Addr && addr < s.Addr+s.Size {
			return true
		}
	}
	return false
}

// ReadVA 按虚拟地址读取 n 字节。地址不可读时返回 nil。
func (img *Image) ReadVA(addr, n uint64) []byte {
	if n == 0 {
		return nil
	}
	for _, s := range img.sections {
		if addr >= s.Addr && addr+n <= s.Addr+s.Size {
			b, err := s.Data()
			if err != nil {
				return nil
			}
			off := addr - s.Addr
			return b[off : off+n]
		}
	}
	return nil
}

// CString 读取以 NUL 结尾的字符串，上限 max 字节。
func (img *Image) CString(addr uint64, max uint64) string {
	b := img.ReadVA(addr, max)
	if b == nil {
		// 尾部可能越界，逐步缩小。
		for try := max; try > 0; try /= 2 {
			if b = img.ReadVA(addr, try); b != nil {
				break
			}
		}
		if b == nil {
			return ""
		}
	}
	if i := indexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// Code 返回函数的机器码。
func (img *Image) Code(fn *gosym.Func) []byte {
	if fn.End <= fn.Entry {
		return nil
	}
	return img.ReadVA(fn.Entry, fn.End-fn.Entry)
}

// Sanity 报告解析结果是否自洽：函数入口应落在节内。
func (img *Image) Sanity() (okCount, badCount int, sample []string) {
	for i := range img.Table.Funcs {
		fn := &img.Table.Funcs[i]
		if img.addrInSections(fn.Entry) {
			okCount++
		} else {
			badCount++
			if len(sample) < 5 {
				sample = append(sample, fmt.Sprintf("%s @0x%x", fn.Name, fn.Entry))
			}
		}
	}
	return
}
