package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/model"
)

func newLocalModelMenu(t *testing.T) tuiModel {
	t.Helper()
	isolateModelDir(t)
	rf := &rootFlags{cfg: config.Config{Provider: config.ProviderLocal, Mode: config.ModeAI}}
	s := &session{cfg: config.Config{Provider: config.ProviderLocal, Mode: config.ModeAI}, provider: config.ProviderLocal, engine: &captureEngine{}}
	m := NewTuiModel(rf, s).(tuiModel)
	m.width, m.height = 120, 40
	mm, _ := m.openModelMenu()
	return mm
}

func writeMenuModel(t *testing.T, name string, data []byte) string {
	t.Helper()
	d := model.New("")
	if err := os.MkdirAll(d.ModelPath(""), 0o755); err != nil {
		t.Fatal(err)
	}
	path := d.ModelPath(name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeMenuSidecar(t *testing.T, modelPath string, data []byte) {
	t.Helper()
	sum := sha256.Sum256(data)
	if err := os.WriteFile(model.SHA256SidecarPath(modelPath), []byte(hex.EncodeToString(sum[:])+"  "+filepath.Base(modelPath)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func selectMenuItem(m tuiModel, name string) tuiModel {
	items := m.visibleModelItems()
	for i, it := range items {
		if it.name == name {
			m.modelIdx = i
			return m
		}
	}
	return m
}

func TestModelMenuNoteShowsSizeAndHash(t *testing.T) {
	m := newLocalModelMenu(t)
	data := []byte("hello-model")
	path := writeMenuModel(t, "note.gguf", data)
	writeMenuSidecar(t, path, data)

	m.modelItems = m.buildModelMenu()
	m = selectMenuItem(m, "note.gguf")
	note := modelItemNote(m.visibleModelItems()[m.modelIdx])
	if !strings.Contains(note, "sha256:") {
		t.Fatalf("note = %q, want checksum hash", note)
	}
	if strings.Contains(note, "no checksum") {
		t.Fatalf("note = %q, should not report missing checksum", note)
	}
	if !strings.Contains(note, "MB") && !strings.Contains(note, "GB") && !strings.Contains(note, "B") {
		t.Fatalf("note = %q, want size", note)
	}
}

func TestModelMenuNoteShowsMissingChecksum(t *testing.T) {
	m := newLocalModelMenu(t)
	writeMenuModel(t, "plain.gguf", []byte("x"))
	m.modelItems = m.buildModelMenu()
	m = selectMenuItem(m, "plain.gguf")
	note := modelItemNote(m.visibleModelItems()[m.modelIdx])
	if !strings.Contains(note, "no checksum") {
		t.Fatalf("note = %q, want missing checksum marker", note)
	}
}

func TestModelMenuPruneRemovesTmp(t *testing.T) {
	m := newLocalModelMenu(t)
	d := model.New("")
	if err := os.MkdirAll(d.ModelPath(""), 0o755); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(d.ModelPath(""), "part.gguf.tmp")
	if err := os.WriteFile(tmp, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMenuModel(t, "keep.gguf", []byte("ok"))

	mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyF6})
	got := mm.(tuiModel)
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("tmp should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(d.ModelPath("keep.gguf")); err != nil {
		t.Fatalf("regular model should remain: %v", err)
	}
	if !strings.Contains(got.content, "part.gguf.tmp") && !strings.Contains(got.content, "Removed") {
		t.Fatalf("prune result not reported: %q", got.content)
	}
}

func TestModelMenuRemoveDeletesSelected(t *testing.T) {
	m := newLocalModelMenu(t)
	data := []byte("model")
	path := writeMenuModel(t, "gone.gguf", data)
	writeMenuSidecar(t, path, data)
	m.modelItems = m.buildModelMenu()
	m = selectMenuItem(m, "gone.gguf")

	mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyDelete})
	got := mm.(tuiModel)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("model should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(model.SHA256SidecarPath(path)); !os.IsNotExist(err) {
		t.Fatalf("sidecar should be removed, stat err=%v", err)
	}
	if !strings.Contains(got.content, "Removed") || !strings.Contains(got.content, "gone.gguf") {
		t.Fatalf("removal not reported: %q", got.content)
	}
}

func TestModelMenuVerifySelected(t *testing.T) {
	m := newLocalModelMenu(t)
	data := []byte("verify-me")
	path := writeMenuModel(t, "check.gguf", data)
	writeMenuSidecar(t, path, data)
	m.modelItems = m.buildModelMenu()
	m = selectMenuItem(m, "check.gguf")

	mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyF5})
	busy := mm.(tuiModel)
	if !busy.modelBusy {
		t.Fatal("verify should run in background")
	}
	select {
	case msg := <-busy.modelDoneCh:
		done, _ := busy.Update(msg)
		got := done.(tuiModel)
		if !strings.Contains(got.content, "check.gguf") || !strings.Contains(got.content, "OK") {
			t.Fatalf("verify result not reported: %q", got.content)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("verify did not finish")
	}
}

func TestModelMenuVerifyMissingSidecar(t *testing.T) {
	m := newLocalModelMenu(t)
	writeMenuModel(t, "nosum.gguf", []byte("x"))
	m.modelItems = m.buildModelMenu()
	m = selectMenuItem(m, "nosum.gguf")

	mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyF5})
	busy := mm.(tuiModel)
	select {
	case msg := <-busy.modelDoneCh:
		done, _ := busy.Update(msg)
		got := done.(tuiModel)
		if !strings.Contains(got.content, "NO CHECKSUM") {
			t.Fatalf("missing sidecar not reported: %q", got.content)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("verify did not finish")
	}
}
