package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/dedomorozoff/dmsh/internal/netproxy"
)

type Downloader struct {
	modelDir string
	client   *http.Client
}

func New(modelDir string) *Downloader {
	if modelDir == "" {
		modelDir = defaultModelDir()
	}
	return &Downloader{
		modelDir: modelDir,
		client:   newHTTPClient(),
	}
}

var (
	downloadMu   sync.RWMutex
	downloadHTTP *http.Client
)

// noRedirect: скачивание GGUF разбирает редирект на CDN вручную (с него
// берётся финальный URL), поэтому автоследование редиректов выключено.
func noRedirect(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}

// SetProxy задаёт прокси для скачивания моделей. Вызывается один раз при
// старте CLI: файлы весят гигабайты, и качать их мимо настроенного прокси
// нельзя. Ошибка возвращается сразу, а не через несколько минут загрузки.
func SetProxy(p netproxy.Settings) error {
	client, err := p.Client(0)
	if err != nil {
		return err
	}
	client.CheckRedirect = noRedirect
	downloadMu.Lock()
	defer downloadMu.Unlock()
	downloadHTTP = client
	return nil
}

// newHTTPClient возвращает общий клиент скачивания. Таймаут запроса
// остаётся нулевым намеренно: многогигабайтная загрузка с паузами на
// стороне CDN иначе обрывалась бы на середине.
func newHTTPClient() *http.Client {
	downloadMu.RLock()
	defer downloadMu.RUnlock()
	if downloadHTTP != nil {
		return downloadHTTP
	}
	return &http.Client{CheckRedirect: noRedirect}
}

func defaultModelDir() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "dmsh", "models")
}

func (d *Downloader) ModelPath(name string) string {
	return filepath.Join(d.modelDir, name)
}

func (d *Downloader) Exists(name string) bool {
	_, err := os.Stat(d.ModelPath(name))
	return err == nil
}

type ModelInfo struct {
	Name        string
	URL         string
	SizeMB      int
	Description string
	MinRAM      int
	// SHA256 — ожидаемая контрольная сумма файла (hex). Если пуста,
	// целостность не проверяется, но скачивание всё равно может быть
	// возобновлено.
	SHA256 string
}

var RecommendedModels = []ModelInfo{
	{
		Name:        "Qwopus3.5-9B-coder-Exp-Q3_K_M.gguf",
		URL:         "https://huggingface.co/Jackrong/Qwopus3.5-9B-Coder-GGUF/resolve/main/Qwopus3.5-9B-coder-Exp-Q3_K_M.gguf",
		SizeMB:      4409,
		Description: "Qwopus3.5 9B Coder — лучшее качество, 3-bit квантование (рекомендуемая)",
		MinRAM:      6,
	},
	{
		Name:        "Qwopus3.5-9B-coder-Exp-Q4_K_M.gguf",
		URL:         "https://huggingface.co/Jackrong/Qwopus3.5-9B-Coder-GGUF/resolve/main/Qwopus3.5-9B-coder-Exp-Q4_K_M.gguf",
		SizeMB:      5630,
		Description: "Qwopus3.5 9B Coder — 4-bit квантование (нужно 8+ ГБ RAM)",
		MinRAM:      8,
	},
	{
		Name:        "Qwopus3.5-9B-coder-Exp-Q5_K_M.gguf",
		URL:         "https://huggingface.co/Jackrong/Qwopus3.5-9B-Coder-GGUF/resolve/main/Qwopus3.5-9B-coder-Exp-Q5_K_M.gguf",
		SizeMB:      6168,
		Description: "Qwopus3.5 9B Coder — 5-bit квантование (высокое качество, нужно 10+ ГБ RAM)",
		MinRAM:      10,
	},
	{
		Name:        "Qwen3-4B-Q4_K_M.gguf",
		URL:         "https://huggingface.co/Qwen/Qwen3-4B-GGUF/resolve/main/Qwen3-4B-Q4_K_M.gguf",
		SizeMB:      2382,
		Description: "Qwen3 4B — компактная, хорошее соотношение размер/качество",
		MinRAM:      4,
	},
	{
		Name:        "Qwen3-1.7B-Q8_0.gguf",
		URL:         "https://huggingface.co/Qwen/Qwen3-1.7B-GGUF/resolve/main/Qwen3-1.7B-Q8_0.gguf",
		SizeMB:      1749,
		Description: "Qwen3 1.7B — для слабых машин",
		MinRAM:      2,
	},
	{
		Name:        "Qwen3-8B-Q4_K_M.gguf",
		URL:         "https://huggingface.co/Qwen/Qwen3-8B-GGUF/resolve/main/Qwen3-8B-Q4_K_M.gguf",
		SizeMB:      4795,
		Description: "Qwen3 8B — максимальное качество (нужно 8+ ГБ RAM)",
		MinRAM:      8,
	},
	{
		Name:        "qwen2.5-1.5b-instruct-q4_k_m.gguf",
		URL:         "https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct-GGUF/resolve/main/qwen2.5-1.5b-instruct-q4_k_m.gguf",
		SizeMB:      1066,
		Description: "Qwen2.5 1.5B — предыдущее поколение",
		MinRAM:      2,
	},
	{
		Name:        "llama3.2-1b-instruct-q4_k_m.gguf",
		URL:         "https://huggingface.co/unsloth/Llama-3.2-1B-Instruct-GGUF/resolve/main/Llama-3.2-1B-Instruct-Q4_K_M.gguf",
		SizeMB:      770,
		Description: "Llama 3.2 1B — от Meta",
		MinRAM:      2,
	},
	{
		Name:        "qwen2.5-0.5b-instruct-q4_k_m.gguf",
		URL:         "https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF/resolve/main/qwen2.5-0.5b-instruct-q4_k_m.gguf",
		SizeMB:      469,
		Description: "Qwen2.5 0.5B — самая маленькая, для тестов (по умолчанию)",
		MinRAM:      1,
	},
}

func (d *Downloader) ListModels() []ModelInfo {
	var available []ModelInfo
	for _, m := range RecommendedModels {
		if d.Exists(m.Name) {
			available = append(available, m)
		}
	}
	return available
}

func (d *Downloader) ListAllModels() ([]ModelInfo, error) {
	var models []ModelInfo
	entries, err := os.ReadDir(d.modelDir)
	if err != nil {
		if os.IsNotExist(err) {
			return models, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".gguf") {
			models = append(models, ModelInfo{Name: e.Name()})
		}
	}
	return models, nil
}

func (d *Downloader) EnsureDir() error {
	return os.MkdirAll(d.modelDir, 0755)
}

func (d *Downloader) DownloadURL(url string, progress func(dl int, total int)) (string, error) {
	return d.DownloadURLCtx(context.Background(), url, progress)
}

func (d *Downloader) DownloadURLCtx(ctx context.Context, url string, progress func(dl int, total int)) (string, error) {
	if err := d.EnsureDir(); err != nil {
		return "", fmt.Errorf("create model dir: %w", err)
	}

	name := filepath.Base(url)
	if !strings.HasSuffix(strings.ToLower(name), ".gguf") {
		name += ".gguf"
	}

	destPath := d.ModelPath(name)
	if d.Exists(name) {
		return destPath, nil
	}

	tmpPath := destPath + ".tmp"
	if err := d.downloadFile(ctx, url, tmpPath, "", progress); err != nil {
		if isCorruptDownload(err) {
			_ = os.Remove(tmpPath)
		}
		return "", fmt.Errorf("download %s: %w", name, err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return "", fmt.Errorf("rename tmp file: %w", err)
	}

	return destPath, nil
}

func (d *Downloader) Download(info ModelInfo, progress func(dl int, total int)) (string, error) {
	return d.DownloadCtx(context.Background(), info, progress)
}

// DownloadCtx скачивает файл, прерываясь при отмене ctx. Временный файл
// удаляется при ошибке или отмене.
func (d *Downloader) DownloadCtx(ctx context.Context, info ModelInfo, progress func(dl int, total int)) (string, error) {
	if err := d.EnsureDir(); err != nil {
		return "", fmt.Errorf("create model dir: %w", err)
	}

	destPath := d.ModelPath(info.Name)
	if d.Exists(info.Name) {
		return destPath, nil
	}

	tmpPath := destPath + ".tmp"
	if err := d.downloadFile(ctx, info.URL, tmpPath, info.SHA256, progress); err != nil {
		if isCorruptDownload(err) {
			_ = os.Remove(tmpPath)
		}
		return "", fmt.Errorf("download %s: %w", info.Name, err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return "", fmt.Errorf("rename tmp file: %w", err)
	}

	return destPath, nil
}

// isCorruptDownload сообщает, что временный файл нельзя докачивать.
func isCorruptDownload(err error) bool {
	var c *checksumError
	return errors.As(err, &c)
}

type checksumError struct {
	want string
	got  string
}

func (e *checksumError) Error() string {
	return fmt.Sprintf("sha256 mismatch: want %s, got %s", e.want, e.got)
}

func (d *Downloader) downloadFile(ctx context.Context, rawURL, destPath, wantSHA string, progress func(dl, total int)) error {
	wantSHA = strings.ToLower(strings.TrimSpace(wantSHA))

	// Резюмируем с того места, где остановились: GGUF большой, и обрыв
	// сети не должен означать скачивание с нуля.
	offset := existingSize(destPath)
	if offset > 0 && wantSHA != "" {
		// Нельзя честно проверить целостность куска без знания суммы
		// префикса, поэтому при требовании SHA256 начинаем заново.
		_ = os.Remove(destPath)
		offset = 0
	}

	resp, err := d.openDownload(ctx, rawURL, offset)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	appendMode := offset > 0 && resp.StatusCode == http.StatusPartialContent
	if offset > 0 && !appendMode {
		// Сервер не поддержал Range или вернул неожиданное состояние —
		// начинаем заново, чтобы не склеить мусор.
		_ = resp.Body.Close()
		_ = os.Remove(destPath)
		offset = 0
		resp, err = d.openDownload(ctx, rawURL, 0)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusPartialContent:
	default:
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(destPath, flags, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}

	total := downloadTotal(offset, resp, appendMode)
	ok := false
	defer func() {
		_ = f.Close()
		if !ok && wantSHA != "" {
			// Повреждённый файл нельзя докачивать: удаляем, чтобы следующая
			// попытка началась с чистого состояния.
			_ = os.Remove(destPath)
		}
	}()

	h := sha256.New()
	downloaded := int(offset)
	if !appendMode {
		downloaded = 0
	}
	buf := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if _, werr := f.Write(chunk); werr != nil {
				return fmt.Errorf("write: %w", werr)
			}
			if wantSHA != "" {
				_, _ = h.Write(chunk)
			}
			downloaded += n
			if progress != nil {
				progress(downloaded, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("read: %w", rerr)
		}
	}
	if wantSHA != "" {
		got := hex.EncodeToString(h.Sum(nil))
		if got != wantSHA {
			return &checksumError{want: wantSHA, got: got}
		}
	}
	ok = true
	return nil
}

// openDownload открывает поток скачивания, сохраняя старую логику обхода
// редиректов HuggingFace: первый ответ берём без авто-редиректа, затем
// при необходимости идём по Location и финальному URL вручную.
func (d *Downloader) openDownload(ctx context.Context, rawURL string, offset int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	setRange(req, offset)

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", rawURL, err)
	}

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusTemporaryRedirect {
		loc := resp.Header.Get("Location")
		_ = resp.Body.Close()
		if loc == "" {
			return nil, fmt.Errorf("HTTP %d without Location", resp.StatusCode)
		}
		resp, err = d.getWithRange(ctx, loc, offset)
		if err != nil {
			return nil, fmt.Errorf("GET redirect: %w", err)
		}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return resp, nil
	}

	finalURL := resp.Request.URL.String()
	if finalURL != "" && finalURL != rawURL && strings.HasPrefix(finalURL, "https://") {
		_ = resp.Body.Close()
		resp, err = d.getWithRange(ctx, finalURL, offset)
		if err != nil {
			return nil, fmt.Errorf("GET final: %w", err)
		}
	}
	return resp, nil
}

func (d *Downloader) getWithRange(ctx context.Context, rawURL string, offset int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	setRange(req, offset)
	return d.client.Do(req)
}

func setRange(req *http.Request, offset int64) {
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
}

func existingSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0
	}
	if info.Size() < 0 {
		return 0
	}
	return info.Size()
}

func downloadTotal(offset int64, resp *http.Response, appendMode bool) int {
	if appendMode {
		if resp.ContentLength > 0 {
			return int(offset + resp.ContentLength)
		}
		return int(offset)
	}
	if resp.ContentLength > 0 {
		return int(resp.ContentLength)
	}
	return 100 * 1024 * 1024
}

func DetectRAMGB() int {
	if runtime.GOOS == "windows" {
		return detectWindowsRAM()
	}
	return detectLinuxRAM()
}

func detectWindowsRAM() int {
	return 8
}

func detectLinuxRAM() int {
	return 8
}

var DefaultModelName = "qwen2.5-0.5b-instruct-q4_k_m.gguf"

func RecommendModel() ModelInfo {
	for _, m := range RecommendedModels {
		if m.Name == DefaultModelName {
			return m
		}
	}
	return RecommendedModels[0]
}
