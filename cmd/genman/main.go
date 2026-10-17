// Команда genman собирает man-страницы из описания команд. Значения по
// умолчанию в них печатаются дословно, поэтому конфиг берётся не из файла
// пользователя: иначе в man/*.1 уезжали бы его путь к модели, провайдер,
// адрес прокси и, если ключ лежит в конфиге, сам ключ.
package main

import (
	"log"
	"os"

	"github.com/dedomorozoff/dmsh/internal/cli"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/spf13/cobra/doc"
)

// docConfig — конфиг для документации: стабильные дефолты и ничего
// пользовательского.
func docConfig() config.Config {
	cfg := config.Default()
	cfg.Threads = 0
	cfg.GPULayers = 0
	cfg.Shell = ""
	return cfg
}

// machineFlags — флаги, значение по умолчанию для которых dmsh подбирает по
// машине (число ядер, видеокарта, оболочка). Печатать их в man нельзя: на
// другой машине страница изменится, а в документации «--gpu-layers=32» или
// «--shell=/bin/zsh» выглядели бы как требование пользователя.
var machineFlags = []string{"threads", "gpu-layers", "shell"}

// generate пишет man-страницы в каталог dir. Отдельная функция, чтобы тест
// мог проверить их содержимое, не трогая рабочее дерево.
func generate(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	cmd := cli.NewRootCmdWithConfig(docConfig())
	for _, name := range machineFlags {
		if f := cmd.PersistentFlags().Lookup(name); f != nil {
			f.DefValue = ""
		}
	}
	header := &doc.GenManHeader{
		Title:   "DMSH",
		Section: "1",
		Source:  "Direct Model Shell",
		Manual:  "dmsh Manual",
	}
	return doc.GenManTree(cmd, header, dir)
}

func main() {
	if err := generate("man"); err != nil {
		log.Fatalf("failed to generate man pages: %v", err)
	}
}
