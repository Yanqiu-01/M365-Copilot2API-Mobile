// Package symview 在 binimg 之上提供符号、行段与内联信息的查询视图。
package symview

import (
	"debug/gosym"
	"sort"
	"strings"

	"apktool/binimg"
)

const ModPrefix = "m365-copilot2api/"

// FuncInfo 描述一个顶层函数符号。
type FuncInfo struct {
	Name     string // 已去掉模块前缀
	FullName string
	File     string // 已去掉模块前缀
	Entry    uint64
	End      uint64
	Size     uint64
	LineLo   int // 归属自身（非内联）的最小行
	LineHi   int // 归属自身的最大行
	Inlined  []string
}

// View 是一次查询会话。
type View struct {
	Img   *binimg.Image
	Funcs []FuncInfo

	byName map[string]*FuncInfo
}

// Build 遍历 pclntab 建立视图。onlyModule 为真时只保留本项目符号。
func Build(img *binimg.Image, onlyModule bool) *View {
	v := &View{Img: img, byName: map[string]*FuncInfo{}}
	for i := range img.Table.Funcs {
		fn := &img.Table.Funcs[i]
		if onlyModule && !strings.HasPrefix(fn.Name, ModPrefix) {
			continue
		}
		file, _, _ := img.Table.PCToLine(fn.Entry)
		fi := FuncInfo{
			FullName: fn.Name,
			Name:     strings.TrimPrefix(fn.Name, ModPrefix),
			File:     strings.TrimPrefix(file, ModPrefix),
			Entry:    fn.Entry,
			End:      fn.End,
			Size:     fn.End - fn.Entry,
			LineLo:   1 << 30,
		}
		inl := map[string]bool{}
		for pc := fn.Entry; pc < fn.End; pc += 4 {
			f2, l2, inner := img.Table.PCToLine(pc)
			if inner != nil && inner.Name != fn.Name {
				inl[strings.TrimPrefix(inner.Name, ModPrefix)] = true
				continue
			}
			// 只统计归属自身、且与函数同文件的行
			if strings.TrimPrefix(f2, ModPrefix) != fi.File {
				continue
			}
			if l2 > 0 {
				if l2 < fi.LineLo {
					fi.LineLo = l2
				}
				if l2 > fi.LineHi {
					fi.LineHi = l2
				}
			}
		}
		if fi.LineLo == 1<<30 {
			fi.LineLo = 0
		}
		for k := range inl {
			fi.Inlined = append(fi.Inlined, k)
		}
		sort.Strings(fi.Inlined)
		v.Funcs = append(v.Funcs, fi)
	}
	sort.Slice(v.Funcs, func(i, j int) bool { return v.Funcs[i].Entry < v.Funcs[j].Entry })
	for i := range v.Funcs {
		v.byName[v.Funcs[i].Name] = &v.Funcs[i]
	}
	return v
}

// Lookup 按去前缀名查找。
func (v *View) Lookup(name string) *FuncInfo { return v.byName[name] }

// Find 返回名字包含 substr 的函数。
func (v *View) Find(substr string) []*FuncInfo {
	var out []*FuncInfo
	for i := range v.Funcs {
		if strings.Contains(v.Funcs[i].Name, substr) {
			out = append(out, &v.Funcs[i])
		}
	}
	return out
}

// ByFile 返回归属某文件的函数，按起始行排序。
func (v *View) ByFile(file string) []*FuncInfo {
	var out []*FuncInfo
	for i := range v.Funcs {
		if v.Funcs[i].File == file {
			out = append(out, &v.Funcs[i])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LineLo < out[j].LineLo })
	return out
}

// Files 返回全部涉及的源文件。
func (v *View) Files() []string {
	seen := map[string]bool{}
	for i := range v.Funcs {
		seen[v.Funcs[i].File] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// VisibleSymbols 返回所有可见符号：顶层 + 被内联者。
func (v *View) VisibleSymbols() []string {
	seen := map[string]bool{}
	for i := range v.Funcs {
		seen[v.Funcs[i].Name] = true
		for _, s := range v.Funcs[i].Inlined {
			seen[s] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// GoFunc 取回底层 gosym.Func，用于读机器码。
func (v *View) GoFunc(name string) *gosym.Func {
	fi := v.byName[name]
	if fi == nil {
		return nil
	}
	for i := range v.Img.Table.Funcs {
		if v.Img.Table.Funcs[i].Name == fi.FullName {
			return &v.Img.Table.Funcs[i]
		}
	}
	return nil
}
