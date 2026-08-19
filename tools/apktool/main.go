// apktool 是 APK 源码恢复审计的取证工具。
//
// 子命令：
//
//	info    <bin>                     打印 pclntab 头部与自洽性检查
//	files   <bin>                     列出本项目源文件
//	funcs   <bin> [file]              列出函数与行段（可限定文件）
//	inline  <bin> <funcSubstr>        显示函数的内联树
//	strings <bin> <funcSubstr>        提取函数体内的字符串引用与常量
//	diff    <apk> <local>             符号差集（三级收敛）
//	span    <apk> <local> [file...]   行段规模对照
package main

import (
	"fmt"
	"math"
	"os"
	"sort"

	"apktool/arm64"
	"apktool/binimg"
	"apktool/symview"
)

func main() {
	if len(os.Args) < 3 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	var err error
	switch cmd {
	case "info":
		err = cmdInfo(os.Args[2])
	case "files":
		err = cmdFiles(os.Args[2])
	case "funcs":
		file := ""
		if len(os.Args) > 3 {
			file = os.Args[3]
		}
		err = cmdFuncs(os.Args[2], file)
	case "inline":
		if len(os.Args) < 4 {
			usage()
			os.Exit(2)
		}
		err = cmdInline(os.Args[2], os.Args[3])
	case "strings":
		if len(os.Args) < 4 {
			usage()
			os.Exit(2)
		}
		err = cmdStrings(os.Args[2], os.Args[3])
	case "diff":
		if len(os.Args) < 4 {
			usage()
			os.Exit(2)
		}
		err = cmdDiff(os.Args[2], os.Args[3])
	case "span":
		if len(os.Args) < 4 {
			usage()
			os.Exit(2)
		}
		err = cmdSpan(os.Args[2], os.Args[3], os.Args[4:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `apktool — APK 源码恢复取证工具

  info    <bin>                     pclntab 头部与自洽性检查
  files   <bin>                     列出本项目源文件
  funcs   <bin> [file]              函数与行段
  inline  <bin> <funcSubstr>        内联树
  strings <bin> <funcSubstr>        字符串引用与常量
  diff    <apk> <local>             符号差集（三级收敛）
  span    <apk> <local> [file...]   行段规模对照
`)
}

func open(path string) (*binimg.Image, *symview.View, error) {
	img, err := binimg.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return img, symview.Build(img, true), nil
}

func cmdInfo(path string) error {
	img, err := binimg.Open(path)
	if err != nil {
		return err
	}
	defer img.Close()
	fmt.Printf("file        : %s\n", path)
	fmt.Printf("magic       : 0x%x  (%s)\n", img.Magic, img.Version)
	fmt.Printf("ptrSize     : %d\n", img.PtrSize)
	fmt.Printf("nfunc       : %d\n", img.NFunc)
	fmt.Printf("nfiles      : %d\n", img.NFiles)
	fmt.Printf("pclntab@    : 0x%x\n", img.PclnAddr)
	fmt.Printf("textStart   : 0x%x\n", img.TextStart)
	ok, bad, sample := img.Sanity()
	fmt.Printf("sanity      : %d entries in-range, %d out-of-range\n", ok, bad)
	for _, s := range sample {
		fmt.Printf("   BAD %s\n", s)
	}
	if bad == 0 {
		fmt.Println("            -> 地址映射正确，可做字节级分析")
	} else {
		fmt.Println("            -> 映射仍有问题")
	}
	v := symview.Build(img, true)
	fmt.Printf("module funcs: %d\n", len(v.Funcs))
	fmt.Printf("module files: %d\n", len(v.Files()))
	return nil
}

func cmdFiles(path string) error {
	img, v, err := open(path)
	if err != nil {
		return err
	}
	defer img.Close()
	for _, f := range v.Files() {
		fmt.Println(f)
	}
	return nil
}

func cmdFuncs(path, file string) error {
	img, v, err := open(path)
	if err != nil {
		return err
	}
	defer img.Close()
	var list []*symview.FuncInfo
	if file != "" {
		list = v.ByFile(file)
	} else {
		for i := range v.Funcs {
			list = append(list, &v.Funcs[i])
		}
	}
	for _, fi := range list {
		fmt.Printf("%4d-%-4d size=%-6d 0x%08x  %s\n",
			fi.LineLo, fi.LineHi, fi.Size, fi.Entry, fi.Name)
	}
	return nil
}

func cmdInline(path, sub string) error {
	img, v, err := open(path)
	if err != nil {
		return err
	}
	defer img.Close()
	for _, fi := range v.Find(sub) {
		fmt.Printf("== %s  (%s:%d-%d, size=%d)\n", fi.Name, fi.File, fi.LineLo, fi.LineHi, fi.Size)
		if len(fi.Inlined) == 0 {
			fmt.Println("   (无内联的项目内函数)")
			continue
		}
		for _, s := range fi.Inlined {
			fmt.Println("   inlined:", s)
		}
	}
	return nil
}

func cmdStrings(path, sub string) error {
	img, v, err := open(path)
	if err != nil {
		return err
	}
	defer img.Close()
	for _, fi := range v.Find(sub) {
		fn := v.GoFunc(fi.Name)
		if fn == nil {
			continue
		}
		code := img.Code(fn)
		if code == nil {
			fmt.Printf("== %s  !! 无法读取代码 (0x%x)\n", fi.Name, fi.Entry)
			continue
		}
		fmt.Printf("== %s  (%s:%d-%d, %d bytes)\n", fi.Name, fi.File, fi.LineLo, fi.LineHi, len(code))
		consts, strs := arm64.Analyze(code, fn.Entry, img)
		if len(strs) > 0 {
			fmt.Println("   -- 字符串字面量 --")
			for _, s := range strs {
				fmt.Printf("   +0x%04x len=%-4d %q\n", s.Off, s.Len, s.Val)
			}
		}
		// 只展示有信息量的立即数
		var imms []arm64.Const
		for _, c := range consts {
			if c.Kind == "imm" && c.Imm > 1 && c.Imm < 1<<32 {
				imms = append(imms, c)
			}
			if c.Kind == "fimm" {
				imms = append(imms, c)
			}
		}
		if len(imms) > 0 {
			fmt.Println("   -- 立即数 / 常量池 --")
			seen := map[string]bool{}
			for _, c := range imms {
				key := fmt.Sprintf("%s|%d", c.Kind, c.Imm)
				if seen[key] {
					continue
				}
				seen[key] = true
				if c.Kind == "fimm" {
					fmt.Printf("   +0x%04x %s  raw=0x%x  f64=%v\n", c.Off, c.Note, c.Imm, f64(c.Imm))
				} else {
					fmt.Printf("   +0x%04x %s\n", c.Off, c.Note)
				}
			}
		}
	}
	return nil
}

func f64(bits uint64) float64 { return math.Float64frombits(bits) }

func cmdDiff(apkPath, localPath string) error {
	aImg, aV, err := open(apkPath)
	if err != nil {
		return err
	}
	defer aImg.Close()
	lImg, lV, err := open(localPath)
	if err != nil {
		return err
	}
	defer lImg.Close()

	aTop := set(names(aV.Funcs))
	aAll := set(aV.VisibleSymbols())
	lAll := set(lV.VisibleSymbols())
	lTop := set(names(lV.Funcs))

	fmt.Printf("APK   顶层 %d  可见(含内联) %d\n", len(aTop), len(aAll))
	fmt.Printf("LOCAL 顶层 %d  可见(含内联) %d\n", len(lTop), len(lAll))

	// L1: 本地可见、APK 完全不可见 -> 虚构嫌疑
	var suspect []string
	for s := range lAll {
		if !aAll[s] {
			suspect = append(suspect, s)
		}
	}
	sort.Strings(suspect)
	fmt.Printf("\n== 本地可见但 APK 完全不可见（虚构嫌疑）: %d ==\n", len(suspect))
	for _, s := range suspect {
		fmt.Println("  +", s)
	}

	// L2: APK 可见、本地完全不可见 -> 待恢复
	var missing []string
	for s := range aAll {
		if !lAll[s] {
			missing = append(missing, s)
		}
	}
	sort.Strings(missing)
	fmt.Printf("\n== APK 可见但本地完全不可见（待恢复）: %d ==\n", len(missing))
	for _, s := range missing {
		fmt.Println("  -", s)
	}
	return nil
}

func cmdSpan(apkPath, localPath string, files []string) error {
	aImg, aV, err := open(apkPath)
	if err != nil {
		return err
	}
	defer aImg.Close()
	lImg, lV, err := open(localPath)
	if err != nil {
		return err
	}
	defer lImg.Close()

	if len(files) == 0 {
		seen := map[string]bool{}
		for _, f := range aV.Files() {
			seen[f] = true
		}
		for _, f := range lV.Files() {
			seen[f] = true
		}
		for f := range seen {
			files = append(files, f)
		}
		sort.Strings(files)
	}

	fmt.Printf("%-46s %8s %8s %8s\n", "FILE", "APKmax", "LOCmax", "DELTA")
	for _, f := range files {
		a := maxLine(aV.ByFile(f))
		l := maxLine(lV.ByFile(f))
		if a == 0 && l == 0 {
			continue
		}
		mark := ""
		switch {
		case a == 0:
			mark = "  APK无此文件"
		case l == 0:
			mark = "  本地无此文件"
		case l > a:
			mark = fmt.Sprintf("  本地膨胀 %.1fx", float64(l)/float64(a))
		case a > l:
			mark = "  本地缺失"
		}
		fmt.Printf("%-46s %8d %8d %+8d%s\n", f, a, l, l-a, mark)
	}
	return nil
}

func maxLine(list []*symview.FuncInfo) int {
	m := 0
	for _, fi := range list {
		if fi.LineHi > m {
			m = fi.LineHi
		}
	}
	return m
}

func names(fs []symview.FuncInfo) []string {
	out := make([]string, len(fs))
	for i := range fs {
		out[i] = fs[i].Name
	}
	return out
}

func set(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}
