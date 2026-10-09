import io
p = 'stagecommon.sh'
s = io.open(p, encoding='utf-8').read()
a = 'find "$E" -type f | wc -l\n'
assert s.count(a) == 1
b = '''# the verification on the final working tree (logs written beside this script; the two large `go test -json` logs
# are not kept, their failing sets and counts are)
for f in go-final-id.txt go-final-gofmt.txt go-final-vet.txt go-final-build-amd64.txt go-final-build-arm64.txt \
         go-baseline-id.txt go-baseline-failset.txt go-final-agent-failset.txt go-final-other-failset.txt go-final-summary.txt \
         product-files-after-the-build.txt worktree-files.txt; do
  [ -f "$J/$f" ] && sed 's/\x0d$//' "$J/$f" > "$E/verification/$f"
done
for f in web-build-final.log web-test-final.log; do [ -f "$J/$f" ] && sed 's/\x0d$//' "$J/$f" > "$E/verification/$f.txt"; done
'''
io.open(p, 'w', encoding='utf-8', newline=chr(10)).write(s.replace(a, b + a))
print('ok')
