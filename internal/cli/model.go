package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/model"
	"github.com/spf13/cobra"
)

// listLocalModels печатает рекомендуемые и скачанные модели: общий список
// для `dmsh model list` и для /models вне TUI.
func listLocalModels(out io.Writer) {
	d := model.New("")

	fmt.Fprintln(out, "=== Recommended ===")
	for i, m := range model.RecommendedModels {
		status := "[ ]"
		if d.Exists(m.Name) {
			status = "[*]"
		}
		fmt.Fprintf(out, "%d. %s %s (%d MB)\n    %s\n    %s\n",
			i+1, status, m.Name, m.SizeMB, m.Description, m.URL)
	}

	all, err := d.ListAllModels()
	if err != nil {
		fmt.Fprintf(out, "scan error: %v\n", err)
	}
	if len(all) > 0 {
		fmt.Fprintln(out, "\n=== Downloaded ===")
		for _, m := range all {
			size := ""
			if fi, err := os.Stat(d.ModelPath(m.Name)); err == nil {
				size = fmt.Sprintf(" (%d MB)", fi.Size()/1024/1024)
			}
			fmt.Fprintf(out, "  %s%s\n", m.Name, size)
		}
	}
}

func newModelCmd(rf *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Model management",
		Long:  "Download or select a model for dmsh",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return cmd.Help()
			}
			return runModelWizard(cmd)
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available models",
		RunE: func(cmd *cobra.Command, _ []string) error {
			listLocalModels(cmd.OutOrStdout())
			return nil
		},
	})

	downloadCmd := &cobra.Command{
		Use:   "download [number, name, or URL]",
		Short: "Download a model (URL or from list)",
		Long: `Downloads a GGUF model. You can specify:
  a number from the list (dmsh model list)
  a model name from the list
  a direct URL to a .gguf file`,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) > 0 {
				target = args[0]
			}

			d := model.New("")

			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
				if strings.HasSuffix(strings.ToLower(target), ".gguf") {
					if d.Exists(target) {
						fmt.Fprintf(cmd.OutOrStdout(), "Already downloaded: %s\n", d.ModelPath(target))
						return nil
					}
					return downloadURL(cmd, d, target)
				}
				return fmt.Errorf("URL must point to a .gguf file")
			}

			if target == "" {
				m := model.RecommendModel()
				target = m.Name
			}

			models := model.RecommendedModels
			var info model.ModelInfo
			found := false

			if num, err := strconv.Atoi(target); err == nil && num > 0 && num <= len(models) {
				info = models[num-1]
				found = true
			} else {
				for _, m := range models {
					if m.Name == target {
						info = m
						found = true
						break
					}
				}
			}

			if !found {
				errMsg := fmt.Sprintf("model %q not found in the list.\n", target)
				errMsg += "  Use: dmsh model list\n"
				errMsg += "  Or provide a direct URL to a .gguf file"
				return errors.New(errMsg)
			}

			if d.Exists(info.Name) {
				fmt.Fprintf(cmd.OutOrStdout(), "Model %s already downloaded: %s\n", info.Name, d.ModelPath(info.Name))
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Downloading %s (%d MB)...\n", info.Name, info.SizeMB)
			path, err := d.Download(info, progressFn(cmd.OutOrStdout()))
			if err != nil {
				return fmt.Errorf("download failed: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\n\nDone: %s\n", path)

			if cmd.Flags().Changed("set-default") {
				setDefault(cmd, info.Name)
			}
			return nil
		},
	}
	downloadCmd.Flags().Bool("set-default", false, "Set as default model")

	cmd.AddCommand(downloadCmd)

	pathCmd := &cobra.Command{
		Use:   "path [name]",
		Short: "Show path to a downloaded model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := model.New("")
			if !d.Exists(args[0]) {
				return fmt.Errorf("model %q not found", args[0])
			}
			fmt.Fprintln(cmd.OutOrStdout(), d.ModelPath(args[0]))
			return nil
		},
		ValidArgsFunction: completeDownloadedModels,
	}
	cmd.AddCommand(pathCmd)

	useCmd := &cobra.Command{
		Use:   "use [name]",
		Short: "Set a downloaded model as default",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := model.New("")
			if !d.Exists(args[0]) {
				return fmt.Errorf("model %q not found in %s", args[0], d.ModelPath(""))
			}
			setDefault(cmd, args[0])
			return nil
		},
		ValidArgsFunction: completeDownloadedModels,
	}
	cmd.AddCommand(useCmd)

	verifyCmd := &cobra.Command{
		Use:   "verify [name...]",
		Short: "Verify model integrity against .sha256 sidecar files",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			d := model.New("")
			paths, err := resolveModelVerifyTargets(d, args)
			if err != nil {
				return err
			}
			if len(paths) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no models to verify")
				return nil
			}
			var failed []string
			for _, p := range paths {
				name := filepath.Base(p)
				checked, verr := model.VerifySHA256Sidecar(p)
				switch {
				case verr != nil:
					msg := verr.Error()
					if checked {
						fmt.Fprintf(cmd.OutOrStdout(), "%s: FAIL (%s)\n", name, msg)
					} else {
						fmt.Fprintf(cmd.OutOrStdout(), "%s: FAIL (%s)\n", name, msg)
					}
					failed = append(failed, name)
				case !checked:
					fmt.Fprintf(cmd.OutOrStdout(), "%s: NO CHECKSUM (missing .sha256 sidecar)\n", name)
					failed = append(failed, name)
				default:
					fmt.Fprintf(cmd.OutOrStdout(), "%s: OK (sha256 verified)\n", name)
				}
			}
			if len(failed) > 0 {
				return fmt.Errorf("verification failed for %d model(s)", len(failed))
			}
			return nil
		},
		ValidArgsFunction: completeDownloadedModels,
	}
	cmd.AddCommand(verifyCmd)

	removeCmd := &cobra.Command{
		Use:   "remove <name> [name...]",
		Short: "Remove downloaded model files and their checksum sidecars",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := model.New("")
			for _, name := range args {
				if err := removeLocalModelFile(d, name); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", name)
			}
			return nil
		},
		ValidArgsFunction: completeDownloadedModels,
	}
	cmd.AddCommand(removeCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "prune",
		Short: "Remove incomplete .gguf.tmp downloads from the models directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := model.New("")
			removed, err := pruneIncompleteModelFiles(d.ModelPath(""))
			if err != nil {
				return err
			}
			if len(removed) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "nothing to prune")
				return nil
			}
			for _, name := range removed {
				fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", name)
			}
			return nil
		},
	})

	return cmd
}

// wizardIO читает ответы из одного сканера, чтобы несколько вопросов подряд
// корректно работали и с pipe, и с терминалом.
type wizardIO struct {
	sc  *bufio.Scanner
	out io.Writer
}

func newWizardIO(in io.Reader, out io.Writer) *wizardIO {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	return &wizardIO{sc: sc, out: out}
}

func (w *wizardIO) ask(prompt string) (string, error) {
	_, _ = fmt.Fprint(w.out, prompt)
	flushOutput(w.out)
	if !w.sc.Scan() {
		if err := w.sc.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return strings.TrimSpace(w.sc.Text()), nil
}

// runModelWizard interactively suggests choosing a model to download
// and set it as default.
func runModelWizard(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	in := newWizardIO(cmd.InOrStdin(), out)
	d := model.New("")
	hw := config.DetectHardware()

	fmt.Fprintln(out, "=== Recommended Models ===")
	for i, m := range model.RecommendedModels {
		status := "[ ]"
		if d.Exists(m.Name) {
			status = "[*]"
		}
		fmt.Fprintf(out, "  %2d. %s %s (%d MB)\n      %s\n", i+1, status, m.Name, m.SizeMB, m.Description)
		if hw.RAMGB > 0 && m.MinRAM > hw.RAMGB {
			fmt.Fprintf(out, "      (requires %d GB RAM, you have %d)\n", m.MinRAM, hw.RAMGB)
		}
	}
	fmt.Fprintln(out)

	pick, err := in.ask("Which model to download? (number or name, Enter to cancel): ")
	if err != nil {
		return nil
	}
	pick = strings.TrimSpace(pick)
	if pick == "" {
		fmt.Fprintln(out, "(cancelled)")
		return nil
	}

	var info model.ModelInfo
	found := false
	if num, err := strconv.Atoi(pick); err == nil && num > 0 && num <= len(model.RecommendedModels) {
		info = model.RecommendedModels[num-1]
		found = true
	} else {
		for _, m := range model.RecommendedModels {
			if m.Name == pick {
				info = m
				found = true
				break
			}
		}
	}
	if !found {
		return fmt.Errorf("model %q not found in the list", pick)
	}

	if d.Exists(info.Name) {
		fmt.Fprintf(out, "Model %s already downloaded\n", info.Name)
	} else {
		fmt.Fprintf(out, "Downloading %s (%d MB)...\n", info.Name, info.SizeMB)
		path, err := d.Download(info, progressFn(out))
		if err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
		fmt.Fprintf(out, "\n\nDone: %s\n", path)
	}

	answer, err := in.ask("Set it as default model? [y/N]: ")
	if err != nil {
		return nil
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "y" || answer == "yes" {
		setDefault(cmd, info.Name)
	} else {
		fmt.Fprintln(out, "(current default model unchanged)")
	}
	return nil
}

func downloadURL(cmd *cobra.Command, d *model.Downloader, url string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "Downloading %s ...\n", url)
	path, err := d.DownloadURL(url, progressFn(cmd.OutOrStdout()))
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\n\nDone: %s\n", path)

	if cmd.Flags().Changed("set-default") {
		name := path
		if idx := strings.LastIndexAny(name, "/\\"); idx >= 0 {
			name = name[idx+1:]
		}
		setDefault(cmd, name)
	}
	return nil
}

func setDefault(cmd *cobra.Command, name string) {
	cfg, _ := config.Load()
	cfg.DefaultModel = name
	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Failed to save config: %v\n", err)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Model set as default\n")
	}
}

func progressFn(out io.Writer) func(dl, total int) {
	return func(dl, total int) {
		if total > 0 {
			pct := dl * 100 / total
			fmt.Fprintf(out, "\r  %d%% (%d / %d MB)", pct, dl/1024/1024, total/1024/1024)
		}
	}
}

func resolveModelVerifyTargets(d *model.Downloader, args []string) ([]string, error) {
	if len(args) == 0 {
		models, err := d.ListAllModels()
		if err != nil {
			return nil, err
		}
		paths := make([]string, 0, len(models))
		for _, m := range models {
			paths = append(paths, d.ModelPath(m.Name))
		}
		return paths, nil
	}
	paths := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "" {
			return nil, fmt.Errorf("model name cannot be empty")
		}
		if fi, err := os.Stat(arg); err == nil && !fi.IsDir() {
			paths = append(paths, arg)
			continue
		}
		name := arg
		if err := validateModelName(name); err == nil {
			path := d.ModelPath(name)
			if _, err := os.Stat(path); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil, fmt.Errorf("model %q not found", name)
				}
				return nil, err
			}
			paths = append(paths, path)
			continue
		}
		if _, err := os.Stat(arg); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("model %q not found", arg)
			}
			return nil, err
		}
		paths = append(paths, arg)
	}
	return paths, nil
}

func validateModelName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid model name %q", name)
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("invalid model name %q", name)
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("invalid model name %q", name)
	}
	return nil
}

// removeLocalModelFile удаляет скачанный .gguf и его .sha256 рядом.
// Имя проверяется: произвольный путь удалить нельзя.
func removeLocalModelFile(d *model.Downloader, name string) error {
	if err := validateModelName(name); err != nil {
		return err
	}
	path := d.ModelPath(name)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("model %q not found", name)
		}
		return fmt.Errorf("model %q: %w", name, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove %q: %w", name, err)
	}
	_ = os.Remove(model.SHA256SidecarPath(path))
	return nil
}

// pruneIncompleteModelFiles удаляет недокачанные *.gguf.tmp из каталога
// моделей. Пустой результат — не ошибка: чистить может быть нечего.
func pruneIncompleteModelFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var removed []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".gguf.tmp") {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return removed, fmt.Errorf("remove %s: %w", name, err)
			}
			removed = append(removed, name)
		}
	}
	return removed, nil
}

// verifyModelStatus сверяет файл с .sha256 рядом и возвращает короткий статус
// для интерфейса. ok=false означает и ошибку чтения, и несовпадение хеша, и
// отсутствие sidecar — всё это для целостности одинаково плохо.
func verifyModelStatus(path string) (ok bool, msg string) {
	checked, verr := model.VerifySHA256Sidecar(path)
	switch {
	case verr != nil:
		if checked {
			return false, "checksum mismatch"
		}
		return false, verr.Error()
	case !checked:
		return false, "no checksum"
	default:
		return true, "sha256 ok"
	}
}

func addModelCommand(root *cobra.Command, rf *rootFlags) {
	modelCmd := newModelCmd(rf)
	modelCmd.Aliases = []string{"models"}
	root.AddCommand(modelCmd)

	root.AddCommand(&cobra.Command{
		Use:   "pull",
		Short: "Download a model (shortcut for model download)",
		RunE:  modelCmd.Commands()[1].RunE,
	})

	root.AddCommand(newInfoCmd())
}
