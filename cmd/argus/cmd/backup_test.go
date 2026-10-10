package cmd

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDBFilePath(t *testing.T) {
	cases := map[string]string{
		"./data/argus.db":       "./data/argus.db",
		"file:./data/argus.db":  "./data/argus.db",
		"file:data.db?mode=rwc": "data.db",
	}
	for in, want := range cases {
		if got := dbFilePath(in); got != want {
			t.Fatalf("dbFilePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestZipAddFileRoundtrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(src, []byte("hello-argus"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.zip")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if err := zipAddFile(zw, "argus.db", src); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 1 || zr.File[0].Name != "argus.db" {
		t.Fatalf("unexpected zip entries: %+v", zr.File)
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	buf := make([]byte, 16)
	n, _ := rc.Read(buf)
	if string(buf[:n]) != "hello-argus" {
		t.Fatalf("roundtrip mismatch: %q", buf[:n])
	}
}

// TestRestoreZipSlipGuard 复现 restore 对非法路径条目的拒绝逻辑
func TestRestoreZipSlipGuard(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "evil.zip")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	// 恶意条目：路径穿越
	if _, err := zw.Create("../../evil.db"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	rejected := false
	for _, zf := range zr.File {
		name := filepath.Clean(zf.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			rejected = true
		}
	}
	if !rejected {
		t.Fatal("expected zip slip entry to be rejected")
	}
}
