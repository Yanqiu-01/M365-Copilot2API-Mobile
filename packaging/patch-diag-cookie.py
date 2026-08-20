#!/usr/bin/env python3
"""让原生诊断页记住登录状态。

DiagActivity 把会话 cookie 存在普通实例字段里（smali 中
`.field private cookie:Ljava/lang/String;`），Activity 一销毁就丢，因此每次
进「网关诊断」都要重新输密码，采集到的帧也随之看不到。dex 里没有任何
SharedPreferences 调用，这个行为无法从 Go 侧修正。

本脚本做两处最小注入：

1. 构造函数里 `cookie = ""` 改成从 SharedPreferences 读回；
2. 登录成功写入 cookie 之后，同步写进 SharedPreferences。

注入使用 Activity 自身作为 Context（`p0`），只依赖 android.content.Context /
SharedPreferences / SharedPreferences$Editor 三个平台接口，不引入新类，也不
改变方法签名与寄存器约定 —— 每处都在原有 `iput-object` 前后插入，寄存器取自
该方法已声明的 `.locals` 范围内的临时变量。
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

PREFS = "m365_diag"
KEY = "cookie"


def find_locals(lines: list[str], method_start: int) -> tuple[int, int]:
    """返回 (locals 行号, 声明的寄存器数)。"""
    for i in range(method_start, min(method_start + 6, len(lines))):
        m = re.match(r"\s*\.locals\s+(\d+)", lines[i])
        if m:
            return i, int(m.group(1))
    raise SystemExit(f"no .locals after line {method_start}")


def method_bounds(lines: list[str], needle: str) -> tuple[int, int]:
    start = None
    for i, line in enumerate(lines):
        if line.startswith(".method") and needle in line:
            start = i
            break
    if start is None:
        raise SystemExit(f"method not found: {needle}")
    for j in range(start, len(lines)):
        if lines[j].strip() == ".end method":
            return start, j
    raise SystemExit("unterminated method")


def patch_constructor(lines: list[str]) -> list[str]:
    """cookie = "" -> cookie = prefs.getString("cookie", "")"""
    start, end = method_bounds(lines, "constructor <init>()V")
    locals_line, locals_n = find_locals(lines, start)

    target = None
    for i in range(start, end):
        if "DiagActivity;->cookie:Ljava/lang/String;" in lines[i] and "iput-object" in lines[i]:
            target = i
            break
    if target is None:
        raise SystemExit("constructor cookie assignment not found")

    reg = lines[target].split()[1].rstrip(",")  # e.g. v0
    # 需要三个临时寄存器；构造函数原本只用到 v0，扩到 4 个足够。
    if locals_n < 4:
        lines[locals_line] = re.sub(r"\.locals\s+\d+", ".locals 4", lines[locals_line])

    inject = f"""    # --- injected: 从 SharedPreferences 读回诊断页会话 cookie ---
    const-string v1, "{PREFS}"

    const/4 v2, 0x0

    invoke-virtual {{p0, v1, v2}}, Landroid/content/Context;->getSharedPreferences(Ljava/lang/String;I)Landroid/content/SharedPreferences;

    move-result-object v1

    const-string v2, "{KEY}"

    const-string v3, ""

    invoke-interface {{v1, v2, v3}}, Landroid/content/SharedPreferences;->getString(Ljava/lang/String;Ljava/lang/String;)Ljava/lang/String;

    move-result-object {reg}

    # --- end injected ---
"""
    # 替换掉紧邻的 const-string reg, "" （若存在），再插入读取逻辑。
    lead = target
    while lead > start and lines[lead - 1].strip() == "":
        lead -= 1
    if lead - 1 > start and re.match(rf'\s*const-string {reg}, ""\s*$', lines[lead - 1]):
        lines[lead - 1] = ""
    lines.insert(target, inject)
    return lines


def patch_login(lines: list[str]) -> list[str]:
    """登录成功后把 cookie 写入 SharedPreferences。"""
    # 目标：方法内对 cookie 赋值且来自 Set-Cookie 解析（含 StringBuilder->toString）
    target = None
    for i, line in enumerate(lines):
        if "DiagActivity;->cookie:Ljava/lang/String;" not in line or "iput-object" not in line:
            continue
        window = "".join(lines[max(0, i - 40):i])
        if "Set-Cookie" in window or "StringBuilder;->toString" in window:
            target = i
    if target is None:
        raise SystemExit("login cookie assignment not found")

    # 定位所属方法，扩大 .locals 以容纳临时寄存器
    m_start = None
    for j in range(target, -1, -1):
        if lines[j].startswith(".method"):
            m_start = j
            break
    if m_start is None:
        raise SystemExit("enclosing method not found")
    locals_line, locals_n = find_locals(lines, m_start)
    need = 9
    if locals_n < need:
        lines[locals_line] = re.sub(r"\.locals\s+\d+", f".locals {need}", lines[locals_line])

    src = lines[target].split()[1].rstrip(",")  # 被写入 cookie 的寄存器
    inject = f"""
    # --- injected: 持久化诊断页会话 cookie ---
    const-string v6, "{PREFS}"

    const/4 v7, 0x0

    invoke-virtual {{p0, v6, v7}}, Landroid/content/Context;->getSharedPreferences(Ljava/lang/String;I)Landroid/content/SharedPreferences;

    move-result-object v6

    invoke-interface {{v6}}, Landroid/content/SharedPreferences;->edit()Landroid/content/SharedPreferences$Editor;

    move-result-object v6

    const-string v7, "{KEY}"

    invoke-interface {{v6, v7, {src}}}, Landroid/content/SharedPreferences$Editor;->putString(Ljava/lang/String;Ljava/lang/String;)Landroid/content/SharedPreferences$Editor;

    move-result-object v6

    invoke-interface {{v6}}, Landroid/content/SharedPreferences$Editor;->apply()V

    # --- end injected ---
"""
    lines.insert(target + 1, inject)
    return lines


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit("usage: patch-diag-cookie.py <smali-root>")
    path = Path(sys.argv[1]) / "com/m365/gateway/DiagActivity.smali"
    if not path.exists():
        raise SystemExit(f"not found: {path}")
    text = path.read_text(encoding="utf-8")
    if "injected: 持久化诊断页会话 cookie" in text:
        print("DiagActivity 已打过补丁，跳过")
        return
    lines = text.split("\n")
    lines = patch_login(lines)
    lines = patch_constructor(lines)
    path.write_text("\n".join(lines), encoding="utf-8")
    print(f"已注入诊断页 cookie 持久化: {path}")


if __name__ == "__main__":
    main()
