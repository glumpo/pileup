#!/bin/bash
set -uo pipefail
du -sk app/modules/core/ios/Mobile.xcframework/*/Mobile.framework/Mobile
app=app/ios/build/Build/Products/Release-iphonesimulator/app.app
du -sk "$app" "$app/app"
find app/ios/build -name 'app-LinkMap-*.txt' -exec python3 -c '
import re, sys
core, zerofill, symbols = set(), [], []
section = ""
for line in open(sys.argv[1], errors="replace"):
    if line.startswith("#"):
        if line.rstrip().endswith(":"):
            section = line
        continue
    m = re.match(r"\[\s*(\d+)\] (.*)", line)
    if "Object files" in section and m and "Mobile.framework/Mobile" in m.group(2):
        core.add(m.group(1))
    z = re.match(r"0x([0-9A-Fa-f]+)\s+0x([0-9A-Fa-f]+)\s+\S+\s+(\S+)", line)
    if "Sections" in section and z and ("bss" in z.group(3) or "common" in z.group(3)):
        start = int(z.group(1), 16)
        zerofill.append((start, start + int(z.group(2), 16)))
    s = re.match(r"0x([0-9A-Fa-f]+)\s+0x([0-9A-Fa-f]+)\s+\[\s*(\d+)\]", line)
    if "Symbols" in section and "Dead" not in section and s:
        symbols.append((int(s.group(1), 16), int(s.group(2), 16), s.group(3)))
symbols.sort()
total = part = 0
for i, (address, size, file) in enumerate(symbols):
    if any(a <= address < b for a, b in zerofill):
        continue
    if i + 1 < len(symbols):
        size = min(size, symbols[i + 1][0] - address)
    total += size
    if file in core:
        part += size
print(f"{sys.argv[1]}: linked without zerofill {total} bytes, core {part} bytes")
' {} \;
true
