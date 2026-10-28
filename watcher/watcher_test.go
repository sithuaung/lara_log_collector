package watcher

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func TestReadLineSkipsPastOverlongLine(t *testing.T) {
	r := bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", 100)+"\nnext"), 16)
	line, err := readLine(r, 10)
	if err != nil || line != strings.Repeat("x", 10) {
		t.Fatalf("line=%q err=%v", line, err)
	}
	if line, err = readLine(r, 10); err != nil || line != "next" {
		t.Fatalf("line=%q err=%v", line, err)
	}
	if _, err = readLine(r, 10); err != io.EOF {
		t.Fatalf("err=%v want EOF", err)
	}
}
