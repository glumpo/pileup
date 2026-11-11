#!/bin/bash
set -uo pipefail
du -sk app/modules/core/ios/Mobile.xcframework/*/Mobile.framework/Mobile
app=app/ios/build/Build/Products/Release-iphonesimulator/app.app
du -sk "$app" "$app/app"
find app/ios/build -name 'app-LinkMap-*.txt' -exec python3 -c '
import re, sys
core, total, part = set(), 0, 0
section = ""
for line in open(sys.argv[1], errors="replace"):
    if line.startswith("#"):
        if line.rstrip().endswith(":"):
            section = line
        continue
    m = re.match(r"\[\s*(\d+)\] (.*)", line)
    if "Object files" in section and m and "Mobile.framework/Mobile" in m.group(2):
        core.add(m.group(1))
    s = re.match(r"0x[0-9A-Fa-f]+\s+0x([0-9A-Fa-f]+)\s+\[\s*(\d+)\]", line)
    if "Symbols" in section and "Dead" not in section and s:
        n = int(s.group(1), 16)
        total += n
        if s.group(2) in core:
            part += n
print(f"{sys.argv[1]}: linked {total} bytes, core {part} bytes")
' {} \;
true
